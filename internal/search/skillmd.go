package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	skillMDDefaultBaseURL  = "https://api.skillmd.com"
	skillMDRequestTimeout  = 10 * time.Second
	maxSkillMDResponseSize = 10 * 1024 * 1024
)

// SkillMDProviderOptions configures the public SkillMD search API. SkillMD's
// documented /v1/search response exposes raw_url, a directly fetchable
// SKILL.md suitable for installation when a repository source is unavailable.
type SkillMDProviderOptions struct {
	BaseURL   string
	SearchURL string
	Client    *http.Client
	Timeout   time.Duration
}

// SkillMDProvider searches the public SkillMD registry.
type SkillMDProvider struct {
	searchURL string
	client    *http.Client
	timeout   time.Duration
}

// NewSkillMDProvider creates a provider using SkillMD's public API base URL.
func NewSkillMDProvider(baseURL string, client *http.Client) *SkillMDProvider {
	return NewSkillMDProviderWithOptions(SkillMDProviderOptions{
		BaseURL: baseURL,
		Client:  client,
	})
}

// NewSkillMDProviderWithOptions creates a provider with a configurable search
// endpoint. SearchURL is primarily useful for a self-hosted registry or tests.
func NewSkillMDProviderWithOptions(options SkillMDProviderOptions) *SkillMDProvider {
	baseURL := strings.TrimRight(options.BaseURL, "/")
	if baseURL == "" {
		baseURL = skillMDDefaultBaseURL
	}
	if options.SearchURL == "" {
		options.SearchURL = baseURL + "/v1/search"
	}
	if options.Client == nil {
		options.Client = &http.Client{Timeout: skillMDRequestTimeout}
	}
	if options.Timeout <= 0 {
		options.Timeout = skillMDRequestTimeout
	}
	return &SkillMDProvider{
		searchURL: options.SearchURL,
		client:    options.Client,
		timeout:   options.Timeout,
	}
}

func (p *SkillMDProvider) Name() string {
	return "skillmd"
}

// Search implements SearchProvider. The public API is intentionally
// unauthenticated, and non-success responses are reported only for this
// provider so an aggregator can retain results from other registries.
func (p *SkillMDProvider) Search(ctx context.Context, query SearchQuery) ([]SearchResult, error) {
	query = query.Normalized()
	requestCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	endpoint, err := url.Parse(p.searchURL)
	if err != nil {
		return nil, errors.New("invalid SkillMD search endpoint")
	}
	params := endpoint.Query()
	params.Set("q", query.Text)
	if query.Limit > 0 {
		params.Set("limit", fmt.Sprintf("%d", query.Limit))
	}
	endpoint.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, errors.New("create SkillMD search request")
	}
	res, err := p.client.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, errors.New("SkillMD request failed")
	}
	defer res.Body.Close()
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("SkillMD request failed: %s", res.Status)
	}

	body, err := io.ReadAll(io.LimitReader(res.Body, maxSkillMDResponseSize+1))
	if err != nil || len(body) > maxSkillMDResponseSize {
		return nil, errors.New("read SkillMD search response")
	}
	entries, err := skillMDEntries(body)
	if err != nil {
		return nil, err
	}
	results := make([]SearchResult, 0, len(entries))
	for _, entry := range entries {
		result, ok := parseSkillMDResult(entry)
		if ok {
			results = append(results, result)
		}
	}
	return results, nil
}

func skillMDEntries(body []byte) ([]json.RawMessage, error) {
	var payload json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, errors.New("decode SkillMD search response")
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(payload, &entries); err == nil {
		return entries, nil
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return nil, errors.New("decode SkillMD search response")
	}
	for _, key := range []string{"items", "data", "results", "skills"} {
		if rawEntries := envelope[key]; len(rawEntries) > 0 {
			if err := json.Unmarshal(rawEntries, &entries); err != nil {
				return nil, errors.New("decode SkillMD search results")
			}
			return entries, nil
		}
	}
	return []json.RawMessage{}, nil
}

func parseSkillMDResult(raw json.RawMessage) (SearchResult, bool) {
	metadata := map[string]any{}
	if json.Unmarshal(raw, &metadata) != nil {
		return SearchResult{}, false
	}
	slug := stringValue(metadata, "slug", "id")
	name := stringValue(metadata, "title", "name")
	if name == "" {
		_, name = sourceParts(slug)
	}
	installURL := stringValue(metadata, "raw_url", "rawUrl", "install_url", "installUrl")
	canonicalSource := stringValue(metadata, "source", "source_url", "sourceUrl", "repository_url", "repositoryUrl")
	if canonicalSource == "" {
		canonicalSource = installURL
	}
	if name == "" || canonicalSource == "" {
		return SearchResult{}, false
	}
	author, repository := sourceParts(slug)
	if value := stringValue(metadata, "author", "owner", "publisher"); value != "" {
		author = value
	}
	if value := stringValue(metadata, "repository", "repo"); value != "" {
		repository = value
	}
	return SearchResult{
		Name:               name,
		Description:        stringValue(metadata, "description", "summary"),
		Provider:           "skillmd",
		CanonicalSource:    canonicalSource,
		InstallURL:         installURL,
		Author:             author,
		Repository:         repository,
		Category:           stringValue(metadata, "category"),
		Version:            stringValue(metadata, "version", "latest_version", "latestVersion"),
		Stars:              intValue(metadata, "stars", "github_stars", "githubStars"),
		Installs:           intValue(metadata, "installs", "install_count", "installCount", "popularity.installs"),
		Rating:             floatValue(metadata, "avg_rating", "average_rating", "rating", "score"),
		VerificationStatus: verificationStatus(metadata),
		AuditStatus:        auditStatus(metadata),
		UpdatedAt:          resultTime(metadata),
		ProviderMetadata:   metadata,
	}, true
}
