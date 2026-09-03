package search

import (
	"cmp"
	"sort"
	"strings"
	"time"
)

// ResultFilter narrows already-normalized, deduplicated results. Provider
// matching considers all provenance so a result discovered by several
// registries remains visible when any requested provider contributed it.
type ResultFilter struct {
	Verified bool
	Provider string
}

// FilterAndRank applies user-facing filtering and ordering without coupling
// registry clients to ranking policy.
func FilterAndRank(results []SearchResult, query SearchQuery, filter ResultFilter, mode SortMode) []SearchResult {
	query = query.Normalized()
	if !mode.Valid() {
		mode = SortRelevance
	}
	filtered := make([]SearchResult, 0, len(results))
	for _, result := range results {
		if filter.Verified && !IsVerified(result) {
			continue
		}
		if filter.Provider != "" && !HasProvider(result, filter.Provider) {
			continue
		}
		filtered = append(filtered, result)
	}

	terms := queryTerms(query.Text)
	sort.SliceStable(filtered, func(i, j int) bool {
		left := rankingValues(filtered[i], terms)
		right := rankingValues(filtered[j], terms)
		return compareRanking(left, right, mode, filtered[i], filtered[j]) < 0
	})
	if query.Limit > 0 && len(filtered) > query.Limit {
		return filtered[:query.Limit]
	}
	return filtered
}

// IsVerified reports whether a result has a positive verification or audit
// signal. It intentionally does not infer verification from provider name.
func IsVerified(result SearchResult) bool {
	return verificationRank(result.VerificationStatus) >= 3 || auditRank(result.AuditStatus) >= 3
}

// HasProvider reports whether a provider contributed a result. Matching is
// case-insensitive because provider names are display-independent identifiers.
func HasProvider(result SearchResult, provider string) bool {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return true
	}
	if strings.EqualFold(result.Provider, provider) {
		return true
	}
	for _, value := range result.Providers {
		if strings.EqualFold(value, provider) {
			return true
		}
	}
	for _, provenance := range result.Provenance {
		if strings.EqualFold(provenance.Provider, provider) {
			return true
		}
	}
	return false
}

type rankValues struct {
	relevance  int
	trust      int
	popularity int
	freshness  time.Time
	source     int
}

func rankingValues(result SearchResult, terms []string) rankValues {
	return rankValues{
		relevance:  relevanceScore(result, terms),
		trust:      verificationRank(result.VerificationStatus)*2 + auditRank(result.AuditStatus),
		popularity: popularityScore(result),
		freshness:  result.UpdatedAt,
		source:     sourceQuality(result),
	}
}

func compareRanking(left, right rankValues, mode SortMode, leftResult, rightResult SearchResult) int {
	primary := func(left, right int) int { return cmp.Compare(right, left) }
	compareFreshness := func() int { return right.freshness.Compare(left.freshness) }
	var comparisons []int
	switch mode {
	case SortPopular:
		comparisons = []int{primary(left.popularity, right.popularity), primary(left.relevance, right.relevance)}
	case SortNewest:
		comparisons = []int{compareFreshness(), primary(left.relevance, right.relevance)}
	default:
		comparisons = []int{primary(left.relevance, right.relevance)}
	}
	comparisons = append(comparisons,
		primary(left.trust, right.trust),
		primary(left.popularity, right.popularity),
		compareFreshness(),
		primary(left.source, right.source),
	)
	for _, comparison := range comparisons {
		if comparison != 0 {
			return comparison
		}
	}
	return strings.Compare(resultTieBreak(leftResult), resultTieBreak(rightResult))
}

func queryTerms(query string) []string {
	words := strings.Fields(strings.ToLower(query))
	terms := make([]string, 0, len(words))
	for _, word := range words {
		word = strings.Trim(word, "-_./")
		if word != "" {
			terms = append(terms, word)
		}
	}
	return terms
}

func relevanceScore(result SearchResult, terms []string) int {
	if len(terms) == 0 {
		return 0
	}
	name := strings.ToLower(result.Name)
	description := strings.ToLower(result.Description)
	category := strings.ToLower(result.Category)
	tags := strings.ToLower(strings.Join(result.Tags, " "))
	source := strings.ToLower(strings.Join([]string{result.CanonicalSource, result.Repository, result.SkillPath}, " "))

	var score int
	for _, term := range terms {
		switch {
		case name == term:
			score += 200
		case containsWord(name, term):
			score += 100
		case strings.Contains(name, term):
			score += 70
		}
		if containsWord(tags, term) {
			score += 45
		} else if strings.Contains(tags, term) {
			score += 25
		}
		if category == term {
			score += 30
		} else if strings.Contains(category, term) {
			score += 15
		}
		if containsWord(description, term) {
			score += 25
		} else if strings.Contains(description, term) {
			score += 10
		}
		if strings.Contains(source, term) {
			score += 5
		}
	}
	return score
}

func containsWord(value, term string) bool {
	for _, word := range strings.FieldsFunc(value, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	}) {
		if word == term {
			return true
		}
	}
	return false
}

func popularityScore(result SearchResult) int {
	// Installs are usually the clearest adoption signal. Stars and rating add
	// resolution without allowing an unbounded rating to dominate installs.
	return result.Installs*1000 + result.Stars*10 + int(result.Rating*10)
}

func sourceQuality(result SearchResult) int {
	score := 0
	if strings.TrimSpace(result.CanonicalSource) != "" {
		score += 2
	}
	if strings.TrimSpace(result.InstallURL) != "" {
		score++
	}
	if strings.TrimSpace(result.Repository) != "" && strings.TrimSpace(result.SkillPath) != "" {
		score++
	}
	return score
}

func resultTieBreak(result SearchResult) string {
	return stableResultKey(result)
}
