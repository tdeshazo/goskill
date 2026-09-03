// Package search defines provider-neutral skill search primitives.
package search

import (
	"context"
	"strings"
	"sync"
	"time"
)

// SearchProvider searches one skill registry.
type SearchProvider interface {
	Name() string
	Search(context.Context, SearchQuery) ([]SearchResult, error)
}

// SearchQuery is the normalized input accepted by every search provider.
type SearchQuery struct {
	Text  string
	Limit int
}

// SortMode selects the primary ordering applied after providers have returned
// their normalized results. Providers deliberately do not receive this value:
// they remain responsible only for retrieval and normalization.
type SortMode string

const (
	SortRelevance SortMode = "relevance"
	SortPopular   SortMode = "popular"
	SortNewest    SortMode = "newest"
)

// Valid reports whether the sort mode is supported by the provider-neutral
// ranking layer.
func (m SortMode) Valid() bool {
	switch m {
	case SortRelevance, SortPopular, SortNewest:
		return true
	default:
		return false
	}
}

// Normalized returns a query with normalized whitespace and a non-negative limit.
func (q SearchQuery) Normalized() SearchQuery {
	q.Text = strings.Join(strings.Fields(q.Text), " ")
	if q.Limit < 0 {
		q.Limit = 0
	}
	return q
}

// SearchResult is a provider-neutral skill search result.
type SearchResult struct {
	ID                 string               `json:"id,omitempty"`
	Name               string               `json:"name"`
	Description        string               `json:"description,omitempty"`
	Provider           string               `json:"provider,omitempty"`
	CanonicalSource    string               `json:"canonical_source,omitempty"`
	InstallURL         string               `json:"install_url,omitempty"`
	Author             string               `json:"author,omitempty"`
	Authors            []string             `json:"authors,omitempty"`
	Repository         string               `json:"repository,omitempty"`
	SkillPath          string               `json:"skill_path,omitempty"`
	Tags               []string             `json:"tags,omitempty"`
	Category           string               `json:"category,omitempty"`
	Version            string               `json:"version,omitempty"`
	Stars              int                  `json:"stars,omitempty"`
	Installs           int                  `json:"installs,omitempty"`
	Rating             float64              `json:"rating,omitempty"`
	VerificationStatus string               `json:"verification_status,omitempty"`
	AuditStatus        string               `json:"audit_status,omitempty"`
	UpdatedAt          time.Time            `json:"updated_at,omitzero"`
	ProviderMetadata   map[string]any       `json:"provider_metadata,omitempty"`
	Providers          []string             `json:"providers,omitempty"`
	Provenance         []ProviderProvenance `json:"provenance,omitempty"`
}

// ProviderProvenance retains a provider's original normalized record after
// equivalent cross-provider search results have been merged.
type ProviderProvenance struct {
	Provider           string         `json:"provider"`
	ID                 string         `json:"id,omitempty"`
	Name               string         `json:"name"`
	Description        string         `json:"description,omitempty"`
	CanonicalSource    string         `json:"canonical_source,omitempty"`
	InstallURL         string         `json:"install_url,omitempty"`
	Author             string         `json:"author,omitempty"`
	Authors            []string       `json:"authors,omitempty"`
	Repository         string         `json:"repository,omitempty"`
	SkillPath          string         `json:"skill_path,omitempty"`
	Tags               []string       `json:"tags,omitempty"`
	Category           string         `json:"category,omitempty"`
	Version            string         `json:"version,omitempty"`
	Stars              int            `json:"stars,omitempty"`
	Installs           int            `json:"installs,omitempty"`
	Rating             float64        `json:"rating,omitempty"`
	VerificationStatus string         `json:"verification_status,omitempty"`
	AuditStatus        string         `json:"audit_status,omitempty"`
	UpdatedAt          time.Time      `json:"updated_at,omitzero"`
	ProviderMetadata   map[string]any `json:"provider_metadata,omitempty"`
}

// ProviderStatus records the outcome for one provider search.
type ProviderStatus struct {
	Provider string
	Err      error
}

// SearchResponse contains every successful provider result and every provider's
// status. A provider error does not prevent other providers from contributing
// results.
type SearchResponse struct {
	Results   []SearchResult
	Providers []ProviderStatus
}

// Aggregator searches a set of providers concurrently.
type Aggregator struct {
	providers []SearchProvider
}

// NewAggregator creates an aggregator for providers. It copies the provider
// slice so callers can reuse or modify their slice safely.
func NewAggregator(providers ...SearchProvider) Aggregator {
	return Aggregator{providers: append([]SearchProvider(nil), providers...)}
}

// Search queries all providers concurrently. Individual provider failures are
// reported in SearchResponse; caller cancellation is returned after all started
// provider searches have observed it and exited.
func (a Aggregator) Search(ctx context.Context, query SearchQuery) (SearchResponse, error) {
	query = query.Normalized()
	response := SearchResponse{
		Results:   []SearchResult{},
		Providers: make([]ProviderStatus, len(a.providers)),
	}
	if len(a.providers) == 0 {
		return response, nil
	}

	type providerResponse struct {
		index   int
		results []SearchResult
		err     error
	}
	responses := make(chan providerResponse, len(a.providers))
	var group sync.WaitGroup
	for index, provider := range a.providers {
		response.Providers[index].Provider = provider.Name()
		group.Add(1)
		go func(index int, provider SearchProvider) {
			defer group.Done()
			results, err := provider.Search(ctx, query)
			responses <- providerResponse{index: index, results: results, err: err}
		}(index, provider)
	}
	group.Wait()
	close(responses)

	resultsByProvider := make([][]SearchResult, len(a.providers))
	for result := range responses {
		resultsByProvider[result.index] = result.results
		response.Providers[result.index].Err = result.err
	}
	for _, results := range resultsByProvider {
		response.Results = append(response.Results, results...)
	}
	response.Results = Deduplicate(response.Results)
	if err := ctx.Err(); err != nil {
		return response, err
	}
	return response, nil
}

// FirstError returns the first provider error, if any.
func (r SearchResponse) FirstError() error {
	for _, status := range r.Providers {
		if status.Err != nil {
			return status.Err
		}
	}
	return nil
}

// HasSuccessfulProvider reports whether at least one provider completed without
// an error, including providers with zero matches.
func (r SearchResponse) HasSuccessfulProvider() bool {
	for _, status := range r.Providers {
		if status.Err == nil {
			return true
		}
	}
	return false
}
