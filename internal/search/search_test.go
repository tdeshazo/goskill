package search

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type testProvider struct {
	name   string
	search func(context.Context, SearchQuery) ([]SearchResult, error)
}

func (p testProvider) Name() string {
	return p.name
}

func (p testProvider) Search(ctx context.Context, query SearchQuery) ([]SearchResult, error) {
	return p.search(ctx, query)
}

func TestAggregatorSearchesProvidersAndCollectsResults(t *testing.T) {
	var queries []SearchQuery
	var mu sync.Mutex
	provider := func(name, skill string) SearchProvider {
		return testProvider{name: name, search: func(_ context.Context, query SearchQuery) ([]SearchResult, error) {
			mu.Lock()
			queries = append(queries, query)
			mu.Unlock()
			return []SearchResult{{Name: skill, Provider: name}}, nil
		}}
	}

	response, err := NewAggregator(provider("one", "first"), provider("two", "second")).Search(
		context.Background(),
		SearchQuery{Text: "  react   hooks ", Limit: 10},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 2 || len(response.Providers) != 2 {
		t.Fatalf("response = %#v", response)
	}
	if !response.HasSuccessfulProvider() || response.FirstError() != nil {
		t.Fatalf("unexpected statuses: %#v", response.Providers)
	}
	for _, query := range queries {
		if query.Text != "react hooks" || query.Limit != 10 {
			t.Fatalf("query = %#v", query)
		}
	}
}

func TestAggregatorToleratesPartialFailure(t *testing.T) {
	failure := errors.New("registry unavailable")
	response, err := NewAggregator(
		testProvider{name: "working", search: func(context.Context, SearchQuery) ([]SearchResult, error) {
			return []SearchResult{{Name: "usable", Provider: "working"}}, nil
		}},
		testProvider{name: "offline", search: func(context.Context, SearchQuery) ([]SearchResult, error) {
			return nil, failure
		}},
	).Search(context.Background(), SearchQuery{Text: "go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 1 || response.Results[0].Name != "usable" {
		t.Fatalf("results = %#v", response.Results)
	}
	if !errors.Is(response.FirstError(), failure) {
		t.Fatalf("first error = %v, want %v", response.FirstError(), failure)
	}
	if !response.HasSuccessfulProvider() {
		t.Fatal("expected a successful provider")
	}
}

func TestAggregatorPropagatesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	response, err := NewAggregator(testProvider{name: "waiting", search: func(ctx context.Context, _ SearchQuery) ([]SearchResult, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}).Search(ctx, SearchQuery{Text: "go"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context cancellation", err)
	}
	if len(response.Providers) != 1 || !errors.Is(response.Providers[0].Err, context.Canceled) {
		t.Fatalf("statuses = %#v", response.Providers)
	}
}

func TestAggregatorPreservesEmptySuccessfulResults(t *testing.T) {
	response, err := NewAggregator(testProvider{name: "empty", search: func(context.Context, SearchQuery) ([]SearchResult, error) {
		return []SearchResult{}, nil
	}}).Search(context.Background(), SearchQuery{Text: "missing"})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 0 || !response.HasSuccessfulProvider() || response.FirstError() != nil {
		t.Fatalf("response = %#v", response)
	}
}

func TestSkillsSHProviderPrefersRichResponseAndNormalizesMetadata(t *testing.T) {
	var legacyCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") != "react hooks" || r.URL.Query().Get("limit") != "10" {
			t.Fatalf("request = %s", r.URL.String())
		}
		switch r.URL.Path {
		case "/rich":
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				t.Fatalf("authorization = %q", got)
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"owner/repo/react","slug":"react","name":"React","description":"Build React applications","source":"owner/repo","installUrl":"https://github.com/owner/repo","url":"https://skills.sh/owner/repo/react","installs":12,"stars":5,"rating":4.8,"verificationStatus":"verified","audits":[{"status":"pass"}],"updatedAt":"2026-01-02T03:04:05Z","sourceType":"github"}]}`))
		case "/legacy":
			legacyCalls++
			_, _ = w.Write([]byte(`{"skills":[]}`))
		default:
			t.Fatalf("path = %q", r.URL.Path)
		}
	}))
	defer server.Close()

	results, err := NewSkillsSHProviderWithOptions(SkillsSHProviderOptions{
		RichSearchURL:   server.URL + "/rich",
		LegacySearchURL: server.URL + "/legacy",
		AuthToken:       "test-token",
		Client:          server.Client(),
	}).Search(context.Background(), SearchQuery{Text: " react   hooks ", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %#v", results)
	}
	result := results[0]
	if result.Provider != "skills.sh" || result.Name != "React" || result.Description != "Build React applications" || result.CanonicalSource != "owner/repo" || result.InstallURL != "https://github.com/owner/repo" {
		t.Fatalf("result = %#v", result)
	}
	if result.Author != "owner" || result.Repository != "repo" || result.Installs != 12 || result.Stars != 5 || result.Rating != 4.8 {
		t.Fatalf("result popularity/source metadata = %#v", result)
	}
	if result.VerificationStatus != "verified" || result.AuditStatus != "pass" || result.UpdatedAt.IsZero() {
		t.Fatalf("result trust/freshness metadata = %#v", result)
	}
	if result.ProviderMetadata["sourceType"] != "github" || legacyCalls != 0 {
		t.Fatalf("result metadata or fallback calls = %#v, %d", result.ProviderMetadata, legacyCalls)
	}
}

func TestSkillsSHProviderFallsBackWithoutAuthentication(t *testing.T) {
	var richCalls, legacyCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rich":
			richCalls++
			if got := r.Header.Get("Authorization"); got != "" {
				t.Fatalf("unexpected rich authorization = %q", got)
			}
			w.WriteHeader(http.StatusUnauthorized)
		case "/legacy":
			legacyCalls++
			if got := r.Header.Get("Authorization"); got != "" {
				t.Fatalf("legacy authorization must be empty, got %q", got)
			}
			_, _ = w.Write([]byte(`{"skills":[{"name":"react","source":"owner/repo","installs":12,"stars":5,"updatedAt":"2026-01-02T03:04:05Z","custom":"value"},{"name":"legacy-date","source":"owner/repo","updatedAt":"January 2, 2026"},{"name":"epoch-date","source":"owner/repo","updatedAt":1767323045}]}`))
		default:
			t.Fatalf("path = %q", r.URL.Path)
		}
	}))
	defer server.Close()

	results, err := NewSkillsSHProviderWithOptions(SkillsSHProviderOptions{
		RichSearchURL:   server.URL + "/rich",
		LegacySearchURL: server.URL + "/legacy",
		Client:          server.Client(),
	}).Search(context.Background(), SearchQuery{Text: "react", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if richCalls != 1 || legacyCalls != 1 || len(results) != 3 {
		t.Fatalf("rich calls = %d, legacy calls = %d, results = %#v", richCalls, legacyCalls, results)
	}
	if results[0].UpdatedAt.IsZero() {
		t.Fatal("RFC3339 updatedAt should be normalized")
	}
	if !results[1].UpdatedAt.IsZero() {
		t.Fatalf("non-RFC3339 updatedAt should be ignored, got %s", results[1].UpdatedAt)
	}
	if results[2].UpdatedAt.IsZero() {
		t.Fatal("numeric epoch updatedAt should be normalized")
	}
}

func TestSkillsSHProviderFallsBackWhenRichEndpointIsUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rich":
			w.WriteHeader(http.StatusServiceUnavailable)
		case "/legacy":
			_, _ = w.Write([]byte(`{"skills":[{"name":"react","source":"owner/repo","installs":12}]}`))
		default:
			t.Fatalf("path = %q", r.URL.Path)
		}
	}))
	defer server.Close()

	results, err := NewSkillsSHProviderWithOptions(SkillsSHProviderOptions{
		RichSearchURL:   server.URL + "/rich",
		LegacySearchURL: server.URL + "/legacy",
		Client:          server.Client(),
	}).Search(context.Background(), SearchQuery{Text: "react"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Name != "react" {
		t.Fatalf("results = %#v", results)
	}
}

func TestSkillsSHProviderFallsBackAfterRichEndpointTimeout(t *testing.T) {
	const endpointTimeout = 100 * time.Millisecond
	var richCalls, legacyCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rich":
			richCalls++
			<-r.Context().Done()
		case "/legacy":
			legacyCalls++
			_, _ = w.Write([]byte(`{"skills":[{"name":"react","source":"owner/repo"}]}`))
		default:
			t.Fatalf("path = %q", r.URL.Path)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*endpointTimeout)
	defer cancel()
	results, err := NewSkillsSHProviderWithOptions(SkillsSHProviderOptions{
		RichSearchURL:   server.URL + "/rich",
		LegacySearchURL: server.URL + "/legacy",
		Client:          server.Client(),
		Timeout:         endpointTimeout,
	}).Search(ctx, SearchQuery{Text: "react"})
	if err != nil {
		t.Fatal(err)
	}
	if richCalls != 1 || legacyCalls != 1 {
		t.Fatalf("rich calls = %d, legacy calls = %d", richCalls, legacyCalls)
	}
	if len(results) != 1 || results[0].Name != "react" {
		t.Fatalf("results = %#v", results)
	}
}

func TestSkillsSHProviderDoesNotExposeCredentialsInFailures(t *testing.T) {
	const token = "very-secret-token"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rich" && r.Header.Get("Authorization") != "Bearer "+token {
			t.Fatalf("missing rich authorization")
		}
		if r.URL.Path == "/legacy" && r.Header.Get("Authorization") != "" {
			t.Fatalf("legacy request included authorization")
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	_, err := NewSkillsSHProviderWithOptions(SkillsSHProviderOptions{
		RichSearchURL:   server.URL + "/rich?token=" + token,
		LegacySearchURL: server.URL + "/legacy",
		AuthToken:       token,
		Client:          server.Client(),
	}).Search(context.Background(), SearchQuery{Text: "react"})
	if err == nil {
		t.Fatal("expected both endpoints to fail")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("error leaked credential: %v", err)
	}
}

func TestSkillsSHProviderHonorsContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := NewSkillsSHProvider(server.URL, server.Client()).Search(ctx, SearchQuery{Text: "go"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context cancellation", err)
	}
}

func TestSkillsSHProviderUsesBoundedTimeout(t *testing.T) {
	provider := NewSkillsSHProvider("http://example.invalid", &http.Client{})
	if provider.timeout != 10*time.Second {
		t.Fatalf("timeout = %s", provider.timeout)
	}
}
