package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const skillsSHRequestTimeout = 10 * time.Second

// SkillsSHProviderOptions configures the skills.sh search endpoints. The v1
// endpoint is currently documented as a richer, optionally OIDC-authenticated
// API; the legacy endpoint remains the compatibility fallback for local CLIs.
type SkillsSHProviderOptions struct {
	BaseURL         string
	RichSearchURL   string
	LegacySearchURL string
	AuthToken       string
	Client          *http.Client
	Timeout         time.Duration
}

// SkillsSHProvider searches the skills.sh registry.
type SkillsSHProvider struct {
	richSearchURL   string
	legacySearchURL string
	authToken       string
	client          *http.Client
	timeout         time.Duration
}

// NewSkillsSHProvider creates a provider using the default skills.sh endpoint
// paths. It preserves the existing base URL configuration used by goskill.
func NewSkillsSHProvider(baseURL string, client *http.Client) *SkillsSHProvider {
	return NewSkillsSHProviderWithOptions(SkillsSHProviderOptions{
		BaseURL: baseURL,
		Client:  client,
	})
}

// NewSkillsSHProviderWithOptions creates a provider with independently
// configurable rich and legacy endpoints. This is useful for deployments that
// proxy either endpoint and for tests; URLs are never included in returned
// errors so query-string credentials cannot be exposed accidentally.
func NewSkillsSHProviderWithOptions(options SkillsSHProviderOptions) *SkillsSHProvider {
	baseURL := strings.TrimRight(options.BaseURL, "/")
	if options.RichSearchURL == "" {
		options.RichSearchURL = baseURL + "/api/v1/skills/search"
	}
	if options.LegacySearchURL == "" {
		options.LegacySearchURL = baseURL + "/api/search"
	}
	if options.Client == nil {
		options.Client = &http.Client{Timeout: skillsSHRequestTimeout}
	}
	if options.Timeout <= 0 {
		options.Timeout = skillsSHRequestTimeout
	}
	return &SkillsSHProvider{
		richSearchURL:   options.RichSearchURL,
		legacySearchURL: options.LegacySearchURL,
		authToken:       options.AuthToken,
		client:          options.Client,
		timeout:         options.Timeout,
	}
}

func (p *SkillsSHProvider) Name() string {
	return "skills.sh"
}

// Search implements SearchProvider. The v1 endpoint is preferred because it
// may expose richer source and quality metadata. Any v1 failure, including a
// missing optional credential, falls back to the established /api/search path.
func (p *SkillsSHProvider) Search(ctx context.Context, query SearchQuery) ([]SearchResult, error) {
	query = query.Normalized()
	richResults, richErr := p.searchEndpoint(ctx, p.richSearchURL, query, p.authToken)
	if richErr == nil {
		return richResults, nil
	}
	legacyResults, legacyErr := p.searchEndpoint(ctx, p.legacySearchURL, query, "")
	if legacyErr == nil {
		return legacyResults, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, errors.New("skills.sh rich and compatibility searches failed")
}

func (p *SkillsSHProvider) searchEndpoint(ctx context.Context, rawURL string, query SearchQuery, authToken string) ([]SearchResult, error) {
	requestCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	endpoint, err := url.Parse(rawURL)
	if err != nil {
		return nil, errors.New("invalid skills.sh search endpoint")
	}
	params := endpoint.Query()
	params.Set("q", query.Text)
	if query.Limit > 0 {
		params.Set("limit", fmt.Sprintf("%d", query.Limit))
	}
	endpoint.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, errors.New("create skills.sh search request")
	}
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}
	res, err := p.client.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, errors.New("skills.sh request failed")
	}
	defer res.Body.Close()
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("skills.sh request failed: %s", res.Status)
	}

	var payload map[string]json.RawMessage
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return nil, errors.New("decode skills.sh search response")
	}
	rawSkills := payload["data"]
	if len(rawSkills) == 0 {
		rawSkills = payload["skills"]
	}
	if len(rawSkills) == 0 {
		return []SearchResult{}, nil
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(rawSkills, &entries); err != nil {
		return nil, errors.New("decode skills.sh search results")
	}
	results := make([]SearchResult, 0, len(entries))
	for _, entry := range entries {
		result, err := parseSkillsSHResult(entry)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

func parseSkillsSHResult(raw json.RawMessage) (SearchResult, error) {
	metadata := map[string]any{}
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return SearchResult{}, errors.New("decode skills.sh result")
	}
	source := stringValue(metadata, "source")
	author, repository := sourceParts(source)
	if value := stringValue(metadata, "author", "owner"); value != "" {
		author = value
	}
	if value := stringValue(metadata, "repository", "repo"); value != "" {
		repository = value
	}
	return SearchResult{
		Name:               stringValue(metadata, "name", "slug", "skillId", "id"),
		Description:        stringValue(metadata, "description", "summary"),
		Provider:           "skills.sh",
		CanonicalSource:    source,
		InstallURL:         stringValue(metadata, "installUrl", "install_url"),
		Author:             author,
		Repository:         repository,
		Stars:              intValue(metadata, "stars", "githubStars", "popularity.stars"),
		Installs:           intValue(metadata, "installs", "installCount", "popularity.installs"),
		Rating:             floatValue(metadata, "rating", "score", "popularity.rating"),
		VerificationStatus: verificationStatus(metadata),
		AuditStatus:        auditStatus(metadata),
		UpdatedAt:          resultTime(metadata),
		ProviderMetadata:   metadata,
	}, nil
}

func sourceParts(source string) (string, string) {
	parts := strings.SplitN(source, "/", 2)
	if len(parts) != 2 {
		return "", source
	}
	return parts[0], parts[1]
}

func stringValue(metadata map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := metadataValue(metadata, key).(string)
		if ok && value != "" {
			return value
		}
	}
	return ""
}

func intValue(metadata map[string]any, keys ...string) int {
	for _, key := range keys {
		value, ok := metadataValue(metadata, key).(float64)
		if ok {
			return int(value)
		}
	}
	return 0
}

func floatValue(metadata map[string]any, keys ...string) float64 {
	for _, key := range keys {
		value, ok := metadataValue(metadata, key).(float64)
		if ok {
			return value
		}
	}
	return 0
}

func verificationStatus(metadata map[string]any) string {
	if status := stringValue(metadata, "verificationStatus", "verification"); status != "" {
		return status
	}
	for _, key := range []string{"verification", "verified", "isVerified"} {
		verified, ok := metadataValue(metadata, key).(bool)
		if ok && verified {
			return "verified"
		}
		if ok {
			return "unverified"
		}
	}
	if verification, ok := metadata["verification"].(map[string]any); ok {
		return stringValue(verification, "status")
	}
	return ""
}

func auditStatus(metadata map[string]any) string {
	if status := stringValue(metadata, "auditStatus"); status != "" {
		return status
	}
	audit, ok := metadata["audit"].(map[string]any)
	if !ok {
		return firstAuditStatus(metadata)
	}
	if status := stringValue(audit, "status"); status != "" {
		return status
	}
	return firstAuditStatus(metadata)
}

func firstAuditStatus(metadata map[string]any) string {
	audits, ok := metadata["audits"].([]any)
	if !ok {
		return ""
	}
	for _, entry := range audits {
		audit, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if status := stringValue(audit, "status"); status != "" {
			return status
		}
	}
	return ""
}

func resultTime(metadata map[string]any) time.Time {
	for _, key := range []string{"updatedAt", "updated_at", "lastUpdated", "publishedAt"} {
		raw, ok := metadata[key]
		if !ok {
			continue
		}
		encoded, err := json.Marshal(raw)
		if err != nil {
			continue
		}
		if parsed := parseResultTime(encoded); !parsed.IsZero() {
			return parsed
		}
	}
	return time.Time{}
}

func metadataValue(metadata map[string]any, path string) any {
	var value any = metadata
	for _, key := range strings.Split(path, ".") {
		values, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		value = values[key]
	}
	return value
}

func parseResultTime(raw json.RawMessage) time.Time {
	var value string
	if json.Unmarshal(raw, &value) == nil {
		parsed, err := time.Parse(time.RFC3339, value)
		if err == nil {
			return parsed
		}
	}

	var epoch float64
	if json.Unmarshal(raw, &epoch) != nil {
		return time.Time{}
	}
	if epoch > 100_000_000_000 {
		return time.UnixMilli(int64(epoch))
	}
	seconds := int64(epoch)
	nanoseconds := int64((epoch - float64(seconds)) * float64(time.Second))
	return time.Unix(seconds, nanoseconds)
}
