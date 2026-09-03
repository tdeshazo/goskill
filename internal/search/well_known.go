package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/tdeshazo/goskill/internal/remoteskill"
	"github.com/tdeshazo/goskill/internal/skills"
)

const wellKnownProviderTimeout = 10 * time.Second

// WellKnownProviderOptions configure one explicitly allowed Agent Skills
// discovery endpoint. Endpoint is never inferred from a user search term, so
// normal search cannot crawl arbitrary domains.
type WellKnownProviderOptions struct {
	Name          string
	Endpoint      string
	Token         string
	CredentialEnv string
	Private       bool
	Client        *http.Client
	Timeout       time.Duration
}

// WellKnownProvider reads a configured Agent Skills well-known index and
// filters its entries locally. The index is intentionally a bounded endpoint,
// not a general web crawler or arbitrary-domain search provider.
type WellKnownProvider struct {
	name          string
	endpoint      string
	token         string
	credentialEnv string
	private       bool
	client        *http.Client
	timeout       time.Duration
}

// NewWellKnownProvider constructs a provider. Search reports malformed
// configured endpoints as an isolated provider failure.
func NewWellKnownProvider(options WellKnownProviderOptions) *WellKnownProvider {
	if options.Client == nil {
		options.Client = &http.Client{Timeout: wellKnownProviderTimeout}
	}
	if options.Timeout <= 0 {
		options.Timeout = wellKnownProviderTimeout
	}
	return &WellKnownProvider{
		name:          strings.TrimSpace(options.Name),
		endpoint:      strings.TrimSpace(options.Endpoint),
		token:         options.Token,
		credentialEnv: strings.TrimSpace(options.CredentialEnv),
		private:       options.Private,
		client:        options.Client,
		timeout:       options.Timeout,
	}
}

// FetchSkill materializes a configured private artifact. Authorization is sent
// only to the endpoint's origin and is never embedded in a source URL.
func (p *WellKnownProvider) FetchSkill(result SearchResult) (skills.Skill, error) {
	if files, indexURL, ok := v1Files(result.ProviderMetadata); ok {
		return p.fetchV1Skill(result.Name, indexURL, files)
	}
	return remoteskill.FetchAuthorized(result.InstallURL, p.token, p.endpoint)
}

func (p *WellKnownProvider) Name() string {
	return p.name
}

// Search implements SearchProvider. The standard supports either
// /.well-known/agent-skills/index.json or the older /.well-known/skills/
// index. A configured index.json URL is accepted directly for reverse proxies.
func (p *WellKnownProvider) Search(ctx context.Context, query SearchQuery) ([]SearchResult, error) {
	query = query.Normalized()
	indexURLs, err := wellKnownIndexURLs(p.endpoint)
	if err != nil {
		return nil, errors.New("invalid well-known provider endpoint")
	}
	var lastErr error
	for _, indexURL := range indexURLs {
		entries, err := p.fetchIndex(ctx, indexURL)
		if err != nil {
			lastErr = err
			continue
		}
		return filterWellKnownEntries(entries, query, p.name, indexURL, p.credentialEnv, p.private), nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if lastErr == nil {
		lastErr = errors.New("well-known index unavailable")
	}
	return nil, lastErr
}

func wellKnownIndexURLs(raw string) ([]string, error) {
	endpoint, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || endpoint.Scheme == "" || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return nil, errors.New("invalid endpoint")
	}
	if endpoint.Scheme != "https" && endpoint.Scheme != "http" {
		return nil, errors.New("invalid endpoint")
	}
	if strings.HasSuffix(strings.ToLower(endpoint.Path), "/index.json") {
		return []string{endpoint.String()}, nil
	}
	base := strings.TrimSuffix(endpoint.String(), "/")
	return []string{
		base + "/.well-known/agent-skills/index.json",
		base + "/.well-known/skills/index.json",
	}, nil
}

type wellKnownEntry struct {
	Name        string
	Description string
	InstallURL  string
	Files       []string
	Metadata    map[string]any
}

func (p *WellKnownProvider) fetchIndex(ctx context.Context, rawURL string) ([]wellKnownEntry, error) {
	requestCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, errors.New("create well-known provider request")
	}
	if p.token != "" {
		req.Header.Set("Authorization", "Bearer "+p.token)
	}
	response, err := p.client.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, errors.New("well-known provider request failed")
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("well-known provider request failed: %s", response.Status)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxSkillMDResponseSize+1))
	if err != nil || len(body) > maxSkillMDResponseSize {
		return nil, errors.New("read well-known provider index")
	}
	return parseWellKnownIndex(body, rawURL)
}

func parseWellKnownIndex(body []byte, indexURL string) ([]wellKnownEntry, error) {
	var payload struct {
		Schema string            `json:"$schema"`
		Skills []json.RawMessage `json:"skills"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, errors.New("decode well-known provider index")
	}
	entries := make([]wellKnownEntry, 0, len(payload.Skills))
	for _, raw := range payload.Skills {
		var entry struct {
			Name        string   `json:"name"`
			Description string   `json:"description"`
			URL         string   `json:"url"`
			Type        string   `json:"type"`
			Files       []string `json:"files"`
		}
		if err := json.Unmarshal(raw, &entry); err != nil || strings.TrimSpace(entry.Name) == "" {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(entry.Type), "archive") {
			continue
		}
		installURL := ""
		if strings.TrimSpace(entry.URL) != "" {
			resolved, err := resolveWellKnownURL(indexURL, entry.URL)
			if err != nil {
				continue
			}
			installURL = resolved
		}
		metadata := map[string]any{"index_schema": payload.Schema}
		if entry.Type != "" {
			metadata["artifact_type"] = entry.Type
		}
		entries = append(entries, wellKnownEntry{
			Name:        strings.TrimSpace(entry.Name),
			Description: strings.TrimSpace(entry.Description),
			InstallURL:  installURL,
			Files:       append([]string(nil), entry.Files...),
			Metadata:    metadata,
		})
	}
	return entries, nil
}

func resolveWellKnownURL(indexURL, raw string) (string, error) {
	base, err := url.Parse(indexURL)
	if err != nil || base.User != nil || base.RawQuery != "" || base.Fragment != "" || base.Scheme == "" || base.Host == "" {
		return "", errors.New("invalid artifact URL")
	}
	reference, err := url.Parse(raw)
	if err != nil || reference.User != nil || reference.RawQuery != "" || reference.Fragment != "" {
		return "", errors.New("invalid artifact URL")
	}
	resolved := base.ResolveReference(reference)
	if resolved.User != nil || resolved.RawQuery != "" || resolved.Fragment != "" || (resolved.Scheme != "https" && resolved.Scheme != "http") {
		return "", errors.New("invalid artifact URL")
	}
	return resolved.String(), nil
}

func filterWellKnownEntries(entries []wellKnownEntry, query SearchQuery, providerName, indexURL, credentialEnv string, private bool) []SearchResult {
	results := make([]SearchResult, 0, len(entries))
	needle := strings.ToLower(query.Text)
	for _, entry := range entries {
		haystack := strings.ToLower(entry.Name + " " + entry.Description)
		if needle != "" && !strings.Contains(haystack, needle) {
			continue
		}
		installURL := entry.InstallURL
		isIndexSource := installURL == ""
		if installURL == "" {
			installURL = wellKnownBaseURL(indexURL)
		}
		canonicalSource := wellKnownSource(installURL)
		if isIndexSource {
			canonicalSource = installURL
		}
		metadata := copyMetadata(entry.Metadata)
		if len(entry.Files) > 0 {
			metadata["well_known_v1_files"] = append([]string(nil), entry.Files...)
			metadata["well_known_v1_index_url"] = indexURL
		}
		if private && credentialEnv != "" {
			metadata["well_known_config"] = providerName
			metadata["well_known_credential_env"] = credentialEnv
		}
		results = append(results, SearchResult{
			Name:             entry.Name,
			Description:      entry.Description,
			Provider:         providerName,
			CanonicalSource:  canonicalSource,
			InstallURL:       installURL,
			ProviderMetadata: metadata,
		})
		if query.Limit > 0 && len(results) >= query.Limit {
			break
		}
	}
	return results
}

func (p *WellKnownProvider) fetchV1Skill(name, indexURL string, files []string) (skills.Skill, error) {
	var skill skills.Skill
	foundSkill := false
	supporting := make([]skills.SnapshotFile, 0, len(files))
	for _, file := range files {
		artifactURL, ok := v1ArtifactURL(indexURL, name, file)
		if !ok {
			continue
		}
		if strings.EqualFold(path.Base(file), "SKILL.md") {
			fetched, err := remoteskill.FetchAuthorized(artifactURL, p.token, p.endpoint)
			if err != nil {
				return skills.Skill{}, err
			}
			skill = fetched
			foundSkill = true
			continue
		}
		content, err := remoteskill.FetchAuthorizedContents(artifactURL, p.token, p.endpoint)
		if err != nil {
			return skills.Skill{}, err
		}
		supporting = append(supporting, skills.SnapshotFile{Path: file, Contents: string(content)})
	}
	if !foundSkill {
		return skills.Skill{}, errors.New("configured well-known skill does not include SKILL.md")
	}
	skill.Files = append(skill.Files, supporting...)
	return skill, nil
}

func v1Files(metadata map[string]any) ([]string, string, bool) {
	indexURL, _ := metadata["well_known_v1_index_url"].(string)
	rawFiles, ok := metadata["well_known_v1_files"].([]string)
	if !ok || indexURL == "" || len(rawFiles) == 0 {
		return nil, "", false
	}
	files := make([]string, 0, len(rawFiles))
	for _, file := range rawFiles {
		if safeV1File(file) {
			files = append(files, file)
		}
	}
	return files, indexURL, len(files) > 0
}

func v1ArtifactURL(indexURL, skillName, file string) (string, bool) {
	if !safeV1File(file) || strings.TrimSpace(skillName) == "" || strings.ContainsAny(skillName, "/\\\x00") {
		return "", false
	}
	index, err := url.Parse(indexURL)
	if err != nil || index.User != nil || index.RawQuery != "" || index.Fragment != "" || !strings.HasSuffix(index.Path, "/index.json") {
		return "", false
	}
	basePath := strings.TrimSuffix(index.Path, "/index.json")
	segments := []string{strings.TrimSuffix(basePath, "/"), skillName}
	for _, segment := range strings.Split(file, "/") {
		segments = append(segments, segment)
	}
	index.Path = strings.Join(segments, "/")
	index.RawPath = ""
	return index.String(), true
}

func safeV1File(file string) bool {
	normalized := strings.ReplaceAll(file, "\\", "/")
	if normalized == "" || strings.HasPrefix(normalized, "/") || strings.Contains(normalized, "\x00") {
		return false
	}
	for _, part := range strings.Split(normalized, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func wellKnownBaseURL(indexURL string) string {
	parsed, err := url.Parse(indexURL)
	if err != nil || parsed.Host == "" {
		return ""
	}
	for _, suffix := range []string{
		"/.well-known/agent-skills/index.json",
		"/.well-known/skills/index.json",
	} {
		if strings.HasSuffix(parsed.Path, suffix) {
			return parsed.Scheme + "://" + parsed.Host + strings.TrimSuffix(parsed.Path, suffix)
		}
	}
	return parsed.Scheme + "://" + parsed.Host + path.Dir(parsed.Path)
}

func wellKnownSource(installURL string) string {
	parsed, err := url.Parse(installURL)
	if err != nil || parsed.Host == "" {
		return ""
	}
	directory := path.Dir(parsed.Path)
	if directory == "." || directory == "/" {
		return parsed.Scheme + "://" + parsed.Host
	}
	return parsed.Scheme + "://" + parsed.Host + directory
}
