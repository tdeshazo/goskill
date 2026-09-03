package search

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestFilterAndRankSortModesAndRelevance(t *testing.T) {
	updated := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	results := []SearchResult{
		{
			Name:            "React patterns",
			Description:     "Useful React application conventions",
			CanonicalSource: "owner/react",
			Installs:        5,
			UpdatedAt:       updated.Add(-24 * time.Hour),
		},
		{
			Name:            "JavaScript patterns",
			Description:     "General frontend guidance",
			CanonicalSource: "owner/javascript",
			Installs:        500,
			UpdatedAt:       updated,
		},
		{
			Name:               "React official",
			CanonicalSource:    "owner/react-official",
			Installs:           1,
			VerificationStatus: "verified",
			UpdatedAt:          updated.Add(-48 * time.Hour),
		},
	}

	tests := []struct {
		name string
		mode SortMode
		want []string
	}{
		{
			name: "relevance keeps matching name ahead of popularity",
			mode: SortRelevance,
			want: []string{"React patterns", "React official", "JavaScript patterns"},
		},
		{
			name: "popular uses installs as the primary adoption signal",
			mode: SortPopular,
			want: []string{"JavaScript patterns", "React patterns", "React official"},
		},
		{
			name: "newest uses freshness as the primary signal",
			mode: SortNewest,
			want: []string{"JavaScript patterns", "React patterns", "React official"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := FilterAndRank(results, SearchQuery{Text: "react"}, ResultFilter{}, test.mode)
			if names := resultNames(got); !reflect.DeepEqual(names, test.want) {
				t.Fatalf("names = %#v, want %#v", names, test.want)
			}
		})
	}
}

func TestFilterAndRankFiltersMergedProvenanceAndVerified(t *testing.T) {
	results := []SearchResult{
		{
			Name:               "merged",
			Provider:           "skills.sh",
			Providers:          []string{"skills.sh", "skillmd"},
			VerificationStatus: "verified",
		},
		{
			Name:     "unverified",
			Provider: "skillmd",
		},
		{
			Name:        "audited",
			Provider:    "github",
			AuditStatus: "pass",
		},
	}

	got := FilterAndRank(results, SearchQuery{Text: "skill"}, ResultFilter{
		Verified: true,
		Provider: "SkillMD",
	}, SortRelevance)
	if names := resultNames(got); !reflect.DeepEqual(names, []string{"merged"}) {
		t.Fatalf("filtered names = %#v", names)
	}
	if !IsVerified(results[2]) {
		t.Fatal("audit pass should be treated as a positive verification signal")
	}
}

func TestFilterAndRankIsDeterministic(t *testing.T) {
	results := []SearchResult{
		{Name: "same", Provider: "github", CanonicalSource: "owner/b", Installs: 10},
		{Name: "same", Provider: "skillmd", CanonicalSource: "owner/a", Installs: 10},
	}
	forward := FilterAndRank(results, SearchQuery{Text: "same"}, ResultFilter{}, SortRelevance)
	reversed := FilterAndRank([]SearchResult{results[1], results[0]}, SearchQuery{Text: "same"}, ResultFilter{}, SortRelevance)
	if !reflect.DeepEqual(forward, reversed) {
		t.Fatalf("ranking depends on input order:\nforward=%#v\nreversed=%#v", forward, reversed)
	}
}

func TestFilterAndRankUsesTrustThenSourceQualityAsSecondarySignals(t *testing.T) {
	results := []SearchResult{
		{Name: "web", CanonicalSource: "owner/unverified", Installs: 100},
		{Name: "web", CanonicalSource: "owner/verified", VerificationStatus: "verified"},
		{Name: "web", CanonicalSource: "owner/source"},
		{Name: "web"},
	}
	got := FilterAndRank(results, SearchQuery{Text: "web"}, ResultFilter{}, SortRelevance)
	if sources := resultSources(got); !reflect.DeepEqual(sources, []string{"owner/verified", "owner/unverified", "owner/source", ""}) {
		t.Fatalf("sources = %#v", sources)
	}
}

func TestFilterAndRankAppliesGlobalLimitAfterMultiProviderResultsAreRanked(t *testing.T) {
	query := SearchQuery{Text: "react", Limit: 2}
	response, err := NewAggregator(
		testProvider{name: "one", search: func(context.Context, SearchQuery) ([]SearchResult, error) {
			return []SearchResult{
				{Name: "react one", Provider: "one", CanonicalSource: "one/one", Installs: 1},
				{Name: "react two", Provider: "one", CanonicalSource: "one/two", Installs: 20},
			}, nil
		}},
		testProvider{name: "two", search: func(context.Context, SearchQuery) ([]SearchResult, error) {
			return []SearchResult{
				{Name: "react three", Provider: "two", CanonicalSource: "two/three", Installs: 40},
				{Name: "react four", Provider: "two", CanonicalSource: "two/four", Installs: 30},
			}, nil
		}},
	).Search(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}

	ranked := FilterAndRank(response.Results, query, ResultFilter{}, SortPopular)
	if names := resultNames(ranked); !reflect.DeepEqual(names, []string{"react three", "react four"}) {
		t.Fatalf("globally limited results = %#v", names)
	}
	if unlimited := FilterAndRank(response.Results, SearchQuery{Text: "react"}, ResultFilter{}, SortPopular); len(unlimited) != 4 {
		t.Fatalf("limit zero should be unlimited, got %#v", unlimited)
	}
}

func TestSearchResultJSONOmitsUnsetTimestamps(t *testing.T) {
	encoded, err := json.Marshal(SearchResult{
		Name: "demo",
		Provenance: []ProviderProvenance{{
			Provider: "skills.sh",
			Name:     "demo",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "updated_at") || strings.Contains(string(encoded), "0001-01-01") {
		t.Fatalf("unset timestamps must be omitted: %s", encoded)
	}
}

func resultNames(results []SearchResult) []string {
	names := make([]string, 0, len(results))
	for _, result := range results {
		names = append(names, result.Name)
	}
	return names
}

func resultSources(results []SearchResult) []string {
	sources := make([]string, 0, len(results))
	for _, result := range results {
		sources = append(sources, result.CanonicalSource)
	}
	return sources
}
