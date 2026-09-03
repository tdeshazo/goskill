package search

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestDeduplicateIdentityIsConservative(t *testing.T) {
	tests := []struct {
		name       string
		results    []SearchResult
		wantCount  int
		providers  []string
		wantSource string
		wantPath   string
	}{
		{
			name: "equivalent GitHub shorthand tree and blob forms",
			results: []SearchResult{
				{Provider: "skills.sh", Name: "web", CanonicalSource: "github:Owner/Repo.git", SkillPath: "skills/web/SKILL.md", ProviderMetadata: map[string]any{"default_branch": "main"}},
				{Provider: "skillmd", Name: "web", CanonicalSource: "https://github.com/owner/repo/tree/main/skills/web"},
				{Provider: "github", Name: "web", CanonicalSource: "https://github.com/owner/repo/blob/main/skills/web/SKILL.md"},
			},
			wantCount:  1,
			providers:  []string{"skills.sh", "skillmd", "github"},
			wantSource: "owner/repo",
			wantPath:   "skills/web/SKILL.md",
		},
		{
			name: "GitHub skills on different refs stay distinct",
			results: []SearchResult{
				{Provider: "github", Name: "demo", CanonicalSource: "https://github.com/owner/repo/tree/main/skills/demo"},
				{Provider: "github", Name: "demo", CanonicalSource: "https://github.com/owner/repo/blob/release/skills/demo/SKILL.md"},
			},
			wantCount: 2,
		},
		{
			name: "matching GitHub tree and blob refs merge",
			results: []SearchResult{
				{Provider: "skillmd", Name: "demo", CanonicalSource: "https://github.com/owner/repo/tree/release/skills/demo"},
				{Provider: "github", Name: "demo", CanonicalSource: "https://github.com/owner/repo/blob/release/skills/demo/SKILL.md"},
			},
			wantCount: 1,
			providers: []string{"skillmd", "github"},
		},
		{
			name: "ref-less sources without an established default stay distinct",
			results: []SearchResult{
				{Provider: "skills.sh", Name: "demo", CanonicalSource: "owner/repo", SkillPath: "skills/demo/SKILL.md"},
				{Provider: "github", Name: "demo", CanonicalSource: "https://github.com/owner/repo/tree/main/skills/demo"},
			},
			wantCount: 2,
		},
		{
			name: "source and install URL from different repositories stay distinct",
			results: []SearchResult{
				{
					Provider:        "skills.sh",
					Name:            "demo",
					CanonicalSource: "owner/one",
					InstallURL:      "https://github.com/owner/two/blob/main/skills/demo/SKILL.md",
				},
				{
					Provider:        "github",
					Name:            "demo",
					CanonicalSource: "owner/one",
					SkillPath:       "skills/demo/SKILL.md",
					ProviderMetadata: map[string]any{
						"default_branch": "main",
					},
				},
			},
			wantCount: 2,
		},
		{
			name: "same name in different subdirectories stays distinct",
			results: []SearchResult{
				{Provider: "skills.sh", Name: "duplicate", CanonicalSource: "owner/repo", SkillPath: "skills/one/SKILL.md"},
				{Provider: "github", Name: "duplicate", CanonicalSource: "https://github.com/owner/repo/tree/main/skills/two"},
			},
			wantCount: 2,
		},
		{
			name: "same name in different repositories stays distinct",
			results: []SearchResult{
				{Provider: "skills.sh", Name: "duplicate", CanonicalSource: "owner/one", SkillPath: "SKILL.md"},
				{Provider: "github", Name: "duplicate", CanonicalSource: "owner/two", SkillPath: "SKILL.md"},
			},
			wantCount: 2,
		},
		{
			name: "missing source never falls back to name",
			results: []SearchResult{
				{Provider: "skills.sh", Name: "duplicate"},
				{Provider: "skillmd", Name: "duplicate"},
			},
			wantCount: 2,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Deduplicate(test.results)
			if len(got) != test.wantCount {
				t.Fatalf("count = %d, results = %#v", len(got), got)
			}
			if test.providers != nil && !reflect.DeepEqual(got[0].Providers, test.providers) {
				t.Fatalf("providers = %#v, want %#v", got[0].Providers, test.providers)
			}
			if test.wantSource != "" && got[0].CanonicalSource != test.wantSource {
				t.Fatalf("source = %q, want %q", got[0].CanonicalSource, test.wantSource)
			}
			if test.wantPath != "" && got[0].SkillPath != test.wantPath {
				t.Fatalf("path = %q, want %q", got[0].SkillPath, test.wantPath)
			}
		})
	}
}

func TestDeduplicateMergesComplementaryMetadataDeterministically(t *testing.T) {
	old := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	newer := old.Add(24 * time.Hour)
	results := []SearchResult{
		{
			Provider:           "github",
			Name:               "Demo",
			Description:        "Short",
			CanonicalSource:    "https://github.com/owner/repo/blob/main/skills/demo/SKILL.md",
			Stars:              8,
			Installs:           2,
			Rating:             3.5,
			VerificationStatus: "unverified",
			AuditStatus:        "warning",
			UpdatedAt:          old,
			Tags:               []string{"github"},
			Authors:            []string{"GitHub author"},
			ProviderMetadata:   map[string]any{"shared": "short", "github_only": true},
		},
		{
			Provider:           "skills.sh",
			Name:               "Demo Skill",
			Description:        "A much more complete description",
			CanonicalSource:    "owner/repo",
			SkillPath:          "skills/demo/SKILL.md",
			Stars:              25,
			Installs:           10,
			Rating:             4.8,
			VerificationStatus: "verified",
			AuditStatus:        "pass",
			UpdatedAt:          newer,
			Tags:               []string{"registry"},
			Authors:            []string{"Registry author"},
			ProviderMetadata:   map[string]any{"shared": "a longer value", "skills_only": true, "default_branch": "main"},
		},
	}
	forward := Deduplicate(results)
	reversed := Deduplicate([]SearchResult{results[1], results[0]})
	if !reflect.DeepEqual(forward, reversed) {
		t.Fatalf("dedupe must be independent of provider order:\nforward=%#v\nreversed=%#v", forward, reversed)
	}
	if len(forward) != 1 {
		t.Fatalf("results = %#v", forward)
	}
	result := forward[0]
	if result.Stars != 25 || result.Installs != 10 || result.Rating != 4.8 || result.VerificationStatus != "verified" || result.AuditStatus != "pass" || !result.UpdatedAt.Equal(newer) {
		t.Fatalf("signals = %#v", result)
	}
	if result.Description != "A much more complete description" || !reflect.DeepEqual(result.Tags, []string{"github", "registry"}) || !reflect.DeepEqual(result.Authors, []string{"GitHub author", "Registry author"}) {
		t.Fatalf("merged fields = %#v", result)
	}
	if result.ProviderMetadata["shared"] != "a longer value" || result.ProviderMetadata["github_only"] != true || result.ProviderMetadata["skills_only"] != true {
		t.Fatalf("metadata = %#v", result.ProviderMetadata)
	}
	if len(result.Provenance) != 2 || result.Provenance[0].Provider != "skills.sh" || result.Provenance[1].Provider != "github" {
		t.Fatalf("provenance = %#v", result.Provenance)
	}
}

func TestDeduplicateOrdersSameProviderProvenanceDeterministically(t *testing.T) {
	results := []SearchResult{
		{
			Provider:        "github",
			ID:              "second",
			Name:            "Demo B",
			CanonicalSource: "https://github.com/owner/repo/blob/main/skills/demo/SKILL.md",
			Tags:            []string{"second"},
		},
		{
			Provider:        "github",
			ID:              "first",
			Name:            "Demo A",
			CanonicalSource: "https://github.com/owner/repo/tree/main/skills/demo",
			Tags:            []string{"first"},
		},
	}
	forward := Deduplicate(results)
	reversed := Deduplicate([]SearchResult{results[1], results[0]})
	if !reflect.DeepEqual(forward, reversed) {
		t.Fatalf("same-provider dedupe must be independent of response order:\nforward=%#v\nreversed=%#v", forward, reversed)
	}
	if len(forward) != 1 || len(forward[0].Provenance) != 2 || forward[0].Provenance[0].ID != "first" || forward[0].Provenance[1].ID != "second" {
		t.Fatalf("provenance = %#v", forward)
	}
}

func TestAggregatorAppliesCrossProviderDeduplication(t *testing.T) {
	response, err := NewAggregator(
		testProvider{name: "skills.sh", search: func(context.Context, SearchQuery) ([]SearchResult, error) {
			return []SearchResult{{
				Provider:        "skills.sh",
				Name:            "demo",
				CanonicalSource: "owner/repo",
				SkillPath:       "skills/demo/SKILL.md",
				ProviderMetadata: map[string]any{
					"default_branch": "main",
				},
			}}, nil
		}},
		testProvider{name: "github", search: func(context.Context, SearchQuery) ([]SearchResult, error) {
			return []SearchResult{{Provider: "github", Name: "demo", CanonicalSource: "https://github.com/owner/repo/tree/main/skills/demo"}}, nil
		}},
	).Search(context.Background(), SearchQuery{Text: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 1 || !reflect.DeepEqual(response.Results[0].Providers, []string{"skills.sh", "github"}) {
		t.Fatalf("results = %#v", response.Results)
	}
}
