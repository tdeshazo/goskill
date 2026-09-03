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
	Name               string
	Description        string
	Provider           string
	CanonicalSource    string
	InstallURL         string
	Author             string
	Repository         string
	Stars              int
	Installs           int
	Rating             float64
	VerificationStatus string
	AuditStatus        string
	UpdatedAt          time.Time
	ProviderMetadata   map[string]any
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
