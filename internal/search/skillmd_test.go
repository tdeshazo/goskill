package search

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSkillMDProviderSearchNormalizesPublicResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/search" || r.URL.Query().Get("q") != "pdf extraction" || r.URL.Query().Get("limit") != "10" {
			t.Fatalf("request = %s", r.URL.String())
		}
		_, _ = w.Write([]byte(`{"items":[{"slug":"anthropic/pdf","type":"single","title":"PDF extraction","description":"Extract text from PDFs","verified":true,"category":"Docs & Writing","avg_rating":4.7,"rating_count":12,"version":"1.2.3","installs":42,"source_url":"https://github.com/anthropic/pdf","raw_url":"https://api.skillmd.test/api/skills/anthropic/pdf/raw","updated_at":"2026-03-04T05:06:07Z"}]}`))
	}))
	defer server.Close()

	results, err := NewSkillMDProviderWithOptions(SkillMDProviderOptions{
		SearchURL: server.URL + "/v1/search",
		Client:    server.Client(),
	}).Search(context.Background(), SearchQuery{Text: " pdf   extraction ", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %#v", results)
	}
	result := results[0]
	if result.Name != "PDF extraction" || result.Provider != "skillmd" || result.Description != "Extract text from PDFs" {
		t.Fatalf("identity = %#v", result)
	}
	if result.CanonicalSource != "https://github.com/anthropic/pdf" || result.InstallURL != "https://api.skillmd.test/api/skills/anthropic/pdf/raw" {
		t.Fatalf("install sources = %#v", result)
	}
	if result.Author != "anthropic" || result.Repository != "pdf" || result.Category != "Docs & Writing" || result.Version != "1.2.3" {
		t.Fatalf("metadata = %#v", result)
	}
	if result.Installs != 42 || result.Rating != 4.7 || result.VerificationStatus != "verified" || result.UpdatedAt.IsZero() {
		t.Fatalf("signals = %#v", result)
	}
	if result.ProviderMetadata["type"] != "single" || result.ProviderMetadata["rating_count"] != float64(12) {
		t.Fatalf("provider metadata = %#v", result.ProviderMetadata)
	}
}

func TestSkillMDProviderUsesRawURLWhenNoRepositorySourceIsExposed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"items":[{"slug":"owner/demo","raw_url":"https://api.skillmd.test/api/skills/owner/demo/raw"}]}`))
	}))
	defer server.Close()

	results, err := NewSkillMDProvider(server.URL, server.Client()).Search(context.Background(), SearchQuery{Text: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].CanonicalSource != "https://api.skillmd.test/api/skills/owner/demo/raw" {
		t.Fatalf("results = %#v", results)
	}
}

func TestSkillMDProviderReturnsEmptyResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer server.Close()

	results, err := NewSkillMDProvider(server.URL, server.Client()).Search(context.Background(), SearchQuery{Text: "missing"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("results = %#v", results)
	}
}

func TestSkillMDProviderSkipsMalformedIndividualResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"items":[null,{"title":"missing source"},{"slug":"owner/good","raw_url":"https://api.skillmd.test/api/skills/owner/good/raw"},"not an object"]}`))
	}))
	defer server.Close()

	results, err := NewSkillMDProvider(server.URL, server.Client()).Search(context.Background(), SearchQuery{Text: "good"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Name != "good" {
		t.Fatalf("results = %#v", results)
	}
}

func TestSkillMDProviderRejectsMalformedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"items":`))
	}))
	defer server.Close()

	_, err := NewSkillMDProvider(server.URL, server.Client()).Search(context.Background(), SearchQuery{Text: "bad"})
	if err == nil || !strings.Contains(err.Error(), "decode SkillMD") {
		t.Fatalf("error = %v", err)
	}
}

func TestSkillMDProviderReturnsHTTPFailuresIncludingRateLimits(t *testing.T) {
	for _, status := range []int{http.StatusBadGateway, http.StatusTooManyRequests} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
			}))
			defer server.Close()

			_, err := NewSkillMDProvider(server.URL, server.Client()).Search(context.Background(), SearchQuery{Text: "go"})
			if err == nil || !strings.Contains(err.Error(), "SkillMD request failed") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestSkillMDProviderFailureDoesNotPreventOtherProviderResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	response, err := NewAggregator(
		NewSkillMDProvider(server.URL, server.Client()),
		testProvider{name: "working", search: func(context.Context, SearchQuery) ([]SearchResult, error) {
			return []SearchResult{{Name: "usable", Provider: "working"}}, nil
		}},
	).Search(context.Background(), SearchQuery{Text: "go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 1 || response.Results[0].Name != "usable" || response.Providers[0].Err == nil {
		t.Fatalf("response = %#v", response)
	}
	if !response.HasSuccessfulProvider() || !errors.Is(response.Providers[0].Err, response.FirstError()) {
		t.Fatalf("provider status = %#v", response.Providers)
	}
}
