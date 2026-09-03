package search

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestGitHubProviderReturnsRawValidationTimeoutWithoutCaching(t *testing.T) {
	var searchCalls, rawCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/search/code":
			searchCalls.Add(1)
			_, _ = w.Write([]byte(`{"items":[{"path":"skills/demo/SKILL.md","repository":{"full_name":"acme/repo","name":"repo","default_branch":"main","owner":{"login":"acme"}}}]}`))
		case "/raw/acme/repo/main/skills/demo/SKILL.md":
			if rawCalls.Add(1) == 1 {
				<-r.Context().Done()
				return
			}
			_, _ = w.Write([]byte("---\nname: demo\ndescription: Validated after retry\n---\n"))
		default:
			t.Fatalf("unexpected request: %s", r.URL.String())
		}
	}))
	defer server.Close()

	provider := NewGitHubProviderWithOptions(GitHubProviderOptions{
		APIBaseURL: server.URL + "/api",
		RawBaseURL: server.URL + "/raw",
		Client:     server.Client(),
		Timeout:    20 * time.Millisecond,
	})
	if _, err := provider.Search(context.Background(), SearchQuery{Text: "demo"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("first search error = %v, want context deadline exceeded", err)
	}

	results, err := provider.Search(context.Background(), SearchQuery{Text: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Name != "demo" {
		t.Fatalf("results = %#v", results)
	}
	if searchCalls.Load() != 2 || rawCalls.Load() != 2 {
		t.Fatalf("timeout response must not be cached: search=%d raw=%d", searchCalls.Load(), rawCalls.Load())
	}
}

func TestGitHubProviderValidatesCandidatesNormalizesMetadataAndCaches(t *testing.T) {
	var searchCalls, rawCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/search/code":
			searchCalls.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				t.Fatalf("authorization = %q", got)
			}
			if got := r.URL.Query().Get("q"); got != "filename:SKILL.md terraform" {
				t.Fatalf("query = %q", got)
			}
			if got := r.URL.Query().Get("per_page"); got != "3" {
				t.Fatalf("per_page = %q", got)
			}
			_, _ = w.Write([]byte(`{"items":[
				{"path":"skills/good/SKILL.md","sha":"good-sha","html_url":"https://github.com/acme/repo/blob/main/skills/good/SKILL.md","repository":{"full_name":"acme/repo","name":"repo","default_branch":"main","stargazers_count":99,"updated_at":"2026-04-05T06:07:08Z","owner":{"login":"acme"}}},
				{"path":"skills/bad/SKILL.md","sha":"bad-sha","repository":{"full_name":"acme/repo","name":"repo","default_branch":"main","owner":{"login":"acme"}}},
				{"path":"SKILL.md","repository":{"full_name":"archived/repo","name":"repo","archived":true,"default_branch":"main"}}
			]}`))
		case "/raw/acme/repo/main/skills/good/SKILL.md":
			rawCalls.Add(1)
			if got := r.Header.Get("Authorization"); got != "" {
				t.Fatalf("raw request should not include authorization, got %q", got)
			}
			_, _ = w.Write([]byte("---\nname: good-skill\ndescription: A validated skill\n---\n"))
		case "/raw/acme/repo/main/skills/bad/SKILL.md":
			rawCalls.Add(1)
			if got := r.Header.Get("Authorization"); got != "" {
				t.Fatalf("raw request should not include authorization, got %q", got)
			}
			_, _ = w.Write([]byte("not a skill"))
		default:
			t.Fatalf("unexpected request: %s", r.URL.String())
		}
	}))
	defer server.Close()

	provider := NewGitHubProviderWithOptions(GitHubProviderOptions{
		APIBaseURL:    server.URL + "/api",
		RawBaseURL:    server.URL + "/raw",
		AuthToken:     "test-token",
		Client:        server.Client(),
		MaxCandidates: 3,
	})
	query := SearchQuery{Text: "terraform", Limit: 10}
	results, err := provider.Search(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %#v", results)
	}
	result := results[0]
	if result.Name != "good-skill" || result.Description != "A validated skill" || result.Provider != "github" {
		t.Fatalf("skill = %#v", result)
	}
	if result.CanonicalSource != "acme/repo" || result.Author != "acme" || result.Repository != "repo" || result.SkillPath != "skills/good/SKILL.md" || result.Stars != 99 || result.UpdatedAt.IsZero() {
		t.Fatalf("normalized metadata = %#v", result)
	}
	if result.ProviderMetadata["default_branch"] != "main" || result.ProviderMetadata["raw_url"] == "" {
		t.Fatalf("provider metadata = %#v", result.ProviderMetadata)
	}
	if _, err := provider.Search(context.Background(), query); err != nil {
		t.Fatal(err)
	}
	if searchCalls.Load() != 1 || rawCalls.Load() != 2 {
		t.Fatalf("cache calls = search %d, raw %d", searchCalls.Load(), rawCalls.Load())
	}
}

func TestGitHubProviderUsesGHTokenFallback(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "fallback-token")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer fallback-token" {
			t.Fatalf("authorization = %q", got)
		}
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer server.Close()

	results, err := NewGitHubProviderWithOptions(GitHubProviderOptions{
		APIBaseURL: server.URL,
		Client:     server.Client(),
	}).Search(context.Background(), SearchQuery{Text: "go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("results = %#v", results)
	}
}

func TestGitHubProviderHandlesHTTPAndRateLimitFailuresWithoutCredentialLeak(t *testing.T) {
	const token = "secret-github-token"
	for _, status := range []int{http.StatusBadGateway, http.StatusTooManyRequests} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
			}))
			defer server.Close()

			_, err := NewGitHubProviderWithOptions(GitHubProviderOptions{
				APIBaseURL: server.URL,
				AuthToken:  token,
				Client:     server.Client(),
			}).Search(context.Background(), SearchQuery{Text: "go"})
			if err == nil || !strings.Contains(err.Error(), "GitHub search request failed") {
				t.Fatalf("error = %v", err)
			}
			if strings.Contains(err.Error(), token) {
				t.Fatalf("credential leaked in error: %v", err)
			}
		})
	}
}

func TestGitHubProviderFailureDoesNotPreventOtherProviderResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	response, err := NewAggregator(
		NewGitHubProviderWithOptions(GitHubProviderOptions{
			APIBaseURL: server.URL,
			Client:     server.Client(),
		}),
		testProvider{name: "working", search: func(context.Context, SearchQuery) ([]SearchResult, error) {
			return []SearchResult{{Name: "registry skill", Provider: "working"}}, nil
		}},
	).Search(context.Background(), SearchQuery{Text: "go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 1 || response.Results[0].Name != "registry skill" || response.Providers[0].Err == nil || !response.HasSuccessfulProvider() {
		t.Fatalf("response = %#v", response)
	}
}
