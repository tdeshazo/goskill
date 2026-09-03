package catalog

import (
	"context"
	"strings"
	"sync"

	"github.com/tdeshazo/goskill/internal/search"
)

// SearchProvider exposes a cached catalog through goskill's federated search
// interface while keeping catalog-specific schemas outside the search package.
type SearchProvider struct {
	provider Provider
	store    Store
	refresh  bool

	mu     sync.Mutex
	status LoadStatus
}

// NewSearchProvider creates a federated-search adapter for a static catalog.
func NewSearchProvider(provider Provider, store Store, refresh bool) *SearchProvider {
	return &SearchProvider{provider: provider, store: store, refresh: refresh}
}

func (p *SearchProvider) Name() string {
	return p.provider.Name()
}

func (p *SearchProvider) Search(ctx context.Context, query search.SearchQuery) ([]search.SearchResult, error) {
	entries, status, err := p.store.Load(ctx, p.provider, p.refresh)
	p.mu.Lock()
	p.status = status
	if err != nil {
		p.status.RefreshErr = err
	}
	p.mu.Unlock()
	if err != nil {
		return nil, err
	}
	entries = Search(entries, query.Text, query.Limit)
	results := make([]search.SearchResult, 0, len(entries))
	for _, entry := range entries {
		author := ""
		if len(entry.Authors) > 0 {
			author = entry.Authors[0]
		}
		repository := entry.SourceRepo
		if separator := strings.LastIndex(repository, "/"); separator >= 0 {
			repository = repository[separator+1:]
		}
		metadata := copyMetadata(entry.Metadata)
		metadata["catalog_id"] = entry.ID
		metadata["tags"] = append([]string(nil), entry.Tags...)
		metadata["catalog_cached"] = status.Cached
		metadata["catalog_stale"] = status.Stale
		if !entry.AddedAt.IsZero() {
			metadata["catalog_added_at"] = entry.AddedAt
		}
		results = append(results, search.SearchResult{
			ID:               entry.ID,
			Name:             entry.DisplayName,
			Description:      entry.Description,
			Provider:         p.Name(),
			CanonicalSource:  entry.SourceRepo,
			Author:           author,
			Authors:          append([]string(nil), entry.Authors...),
			Repository:       repository,
			SkillPath:        entry.SourcePath,
			Tags:             append([]string(nil), entry.Tags...),
			Category:         entry.Category,
			Stars:            entry.Stars,
			ProviderMetadata: metadata,
		})
	}
	return results, nil
}

// Status returns the result of the most recent catalog load.
func (p *SearchProvider) Status() LoadStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.status
}

func copyMetadata(metadata map[string]any) map[string]any {
	copy := make(map[string]any, len(metadata)+3)
	for key, value := range metadata {
		copy[key] = value
	}
	return copy
}
