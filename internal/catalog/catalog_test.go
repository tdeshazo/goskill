package catalog

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/tdeshazo/goskill/internal/search"
)

type testCatalogProvider struct {
	name       string
	fetch      func(context.Context) ([]byte, error)
	parse      func([]byte) ([]Entry, error)
	fetchCalls int
}

func (p *testCatalogProvider) Name() string {
	return p.name
}

func (p *testCatalogProvider) Fetch(ctx context.Context) ([]byte, error) {
	p.fetchCalls++
	return p.fetch(ctx)
}

func (p *testCatalogProvider) Parse(data []byte) ([]Entry, error) {
	return p.parse(data)
}

func TestStoreUsesFreshCacheForOfflineSearch(t *testing.T) {
	now := time.Date(2026, time.May, 1, 12, 0, 0, 0, time.UTC)
	provider := &testCatalogProvider{
		name:  "test",
		fetch: func(context.Context) ([]byte, error) { return []byte(`"catalog"`), nil },
		parse: func(data []byte) ([]Entry, error) {
			if string(data) != `"catalog"` {
				return nil, errors.New("invalid catalog")
			}
			return []Entry{{ID: "demo", DisplayName: "Demo", Description: "Offline search", Tags: []string{"go"}, SourceRepo: "owner/repo", SourcePath: "skills/demo/SKILL.md"}}, nil
		},
	}
	store := NewStore(t.TempDir(), time.Hour)
	store.Now = func() time.Time { return now }
	entries, status, err := store.Load(context.Background(), provider, false)
	if err != nil || !status.Refreshed || provider.fetchCalls != 1 {
		t.Fatalf("initial load = entries %#v, status %#v, err %v, calls %d", entries, status, err, provider.fetchCalls)
	}
	provider.fetch = func(context.Context) ([]byte, error) { return nil, errors.New("offline") }
	entries, status, err = store.Load(context.Background(), provider, false)
	if err != nil || !status.Cached || status.Refreshed || provider.fetchCalls != 1 {
		t.Fatalf("fresh cache = entries %#v, status %#v, err %v, calls %d", entries, status, err, provider.fetchCalls)
	}
	matched := Search(entries, "offline go", 10)
	if len(matched) != 1 || matched[0].ID != "demo" {
		t.Fatalf("offline search = %#v", matched)
	}
}

func TestStoreKeepsStaleCacheAfterRefreshFailure(t *testing.T) {
	now := time.Date(2026, time.May, 1, 12, 0, 0, 0, time.UTC)
	provider := &testCatalogProvider{
		name:  "test",
		fetch: func(context.Context) ([]byte, error) { return []byte(`"old"`), nil },
		parse: func(data []byte) ([]Entry, error) {
			if string(data) != `"old"` {
				return nil, errors.New("invalid catalog")
			}
			return []Entry{{ID: "old", DisplayName: "Old", SourceRepo: "owner/repo", SourcePath: "SKILL.md"}}, nil
		},
	}
	store := NewStore(t.TempDir(), time.Hour)
	store.Now = func() time.Time { return now }
	if _, _, err := store.Load(context.Background(), provider, false); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(store.path(provider))
	if err != nil {
		t.Fatal(err)
	}
	provider.fetch = func(context.Context) ([]byte, error) { return nil, errors.New("unavailable") }
	store.Now = func() time.Time { return now.Add(2 * time.Hour) }
	entries, status, err := store.Load(context.Background(), provider, false)
	if err != nil || len(entries) != 1 || entries[0].ID != "old" || !status.Stale || status.RefreshErr == nil {
		t.Fatalf("stale fallback = entries %#v, status %#v, err %v", entries, status, err)
	}
	after, err := os.ReadFile(store.path(provider))
	if err != nil || string(after) != string(before) {
		t.Fatalf("cache should remain unchanged: err=%v", err)
	}
}

func TestStoreRefreshesFreshCacheWhenExplicitlyRequested(t *testing.T) {
	provider := &testCatalogProvider{
		name:  "test",
		fetch: func(context.Context) ([]byte, error) { return []byte(`"first"`), nil },
		parse: func(data []byte) ([]Entry, error) {
			id := strings.Trim(string(data), `"`)
			return []Entry{{ID: id, DisplayName: id, SourceRepo: "owner/repo", SourcePath: "SKILL.md"}}, nil
		},
	}
	store := NewStore(t.TempDir(), time.Hour)
	if _, _, err := store.Load(context.Background(), provider, false); err != nil {
		t.Fatal(err)
	}
	provider.fetch = func(context.Context) ([]byte, error) { return []byte(`"second"`), nil }
	entries, status, err := store.Load(context.Background(), provider, true)
	if err != nil || !status.Refreshed || entries[0].ID != "second" || provider.fetchCalls != 2 {
		t.Fatalf("explicit refresh = entries %#v, status %#v, err %v, calls %d", entries, status, err, provider.fetchCalls)
	}
}

func TestTrueFoundryProviderFetchesAndSkipsMalformedEntries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[
			{"id":"owner-demo","display_name":"Demo Skill","description":"Works offline","authors":["Alice","Bob"],"tags":["go","cli"],"category":"coding","source":{"repo":"owner/repo","path":"skills/demo/SKILL.md"},"metadata":{"stars":42},"added_at":"2026-05-01T12:00:00Z"},
			null,
			{"id":"missing-source","source":{"repo":"owner/repo"}}
		]`))
	}))
	defer server.Close()

	provider := NewTrueFoundryProvider(server.URL, server.Client())
	data, err := provider.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	entries, err := provider.Parse(data)
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries = %#v, err = %v", entries, err)
	}
	entry := entries[0]
	if entry.ID != "owner-demo" || entry.DisplayName != "Demo Skill" || entry.SourceRepo != "owner/repo" || entry.SourcePath != "skills/demo/SKILL.md" || entry.Stars != 42 || len(entry.Tags) != 2 || len(entry.Authors) != 2 || entry.AddedAt.IsZero() {
		t.Fatalf("entry = %#v", entry)
	}
}

func TestTrueFoundryProviderRejectsMalformedCatalog(t *testing.T) {
	_, err := NewTrueFoundryProvider("http://example.invalid", nil).Parse([]byte(`{"items":[]}`))
	if err == nil || !strings.Contains(err.Error(), "decode TrueFoundry catalog") {
		t.Fatalf("error = %v", err)
	}
}

func TestCachedSearchProviderSearchesFreshCatalogWithoutNetwork(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`[{"id":"demo","display_name":"Demo","description":"Catalog search","authors":["Alice"],"tags":["offline"],"category":"coding","source":{"repo":"owner/repo","path":"skills/demo/SKILL.md"},"metadata":{"stars":7},"added_at":"2026-05-01T12:00:00Z"}]`))
	}))
	provider := NewTrueFoundryProvider(server.URL, server.Client())
	store := NewStore(t.TempDir(), time.Hour)
	searchProvider := NewSearchProvider(provider, store, false)
	results, err := searchProvider.Search(context.Background(), search.SearchQuery{Text: "catalog offline", Limit: 10})
	if err != nil || len(results) != 1 || results[0].Provider != "truefoundry" || results[0].ID != "demo" || len(results[0].Tags) != 1 {
		t.Fatalf("first results = %#v, err = %v", results, err)
	}
	if !results[0].UpdatedAt.IsZero() {
		t.Fatalf("catalog registration time must not be source freshness: %s", results[0].UpdatedAt)
	}
	if _, ok := results[0].ProviderMetadata["catalog_added_at"].(time.Time); !ok {
		t.Fatalf("catalog added_at missing from metadata: %#v", results[0].ProviderMetadata)
	}
	server.Close()
	results, err = searchProvider.Search(context.Background(), search.SearchQuery{Text: "catalog", Limit: 10})
	if err != nil || len(results) != 1 || calls != 1 {
		t.Fatalf("offline results = %#v, err = %v, calls = %d", results, err, calls)
	}
}
