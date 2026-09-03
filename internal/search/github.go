package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tdeshazo/goskill/internal/github"
)

const (
	githubDefaultAPIURL  = "https://api.github.com"
	githubDefaultRawURL  = "https://raw.githubusercontent.com"
	githubRequestTimeout = 10 * time.Second
	githubMaxCandidates  = 10
)

// GitHubProviderOptions configures the bounded GitHub discovery flow. It uses
// the code-search API for plausible SKILL.md files, then validates a small
// number of raw files before returning them as skill results.
type GitHubProviderOptions struct {
	APIBaseURL    string
	RawBaseURL    string
	AuthToken     string
	Client        *http.Client
	Timeout       time.Duration
	MaxCandidates int
}

// GitHubProvider discovers long-tail skills from public GitHub repositories.
// It is intended for explicit deep searches because GitHub code search has
// tighter rate limits than registry APIs.
type GitHubProvider struct {
	apiBaseURL    string
	rawBaseURL    string
	authToken     string
	client        *http.Client
	timeout       time.Duration
	maxCandidates int

	mu    sync.Mutex
	cache map[string][]SearchResult
}

// NewGitHubProvider creates a provider using the existing GitHub endpoint and
// authentication environment conventions.
func NewGitHubProvider() *GitHubProvider {
	return NewGitHubProviderWithOptions(GitHubProviderOptions{})
}

// NewGitHubProviderWithOptions creates a provider with configurable endpoints
// and HTTP settings. Endpoint overrides are useful for enterprise GitHub and
// tests. Errors intentionally omit URLs and tokens.
func NewGitHubProviderWithOptions(options GitHubProviderOptions) *GitHubProvider {
	if options.APIBaseURL == "" {
		options.APIBaseURL = githubEnvDefault("GITHUB_API_URL", githubDefaultAPIURL)
	}
	if options.RawBaseURL == "" {
		options.RawBaseURL = githubEnvDefault("RAW_GITHUB_URL", githubDefaultRawURL)
	}
	if options.AuthToken == "" {
		options.AuthToken = github.AuthToken()
	}
	if options.Client == nil {
		options.Client = &http.Client{Timeout: githubRequestTimeout}
	}
	if options.Timeout <= 0 {
		options.Timeout = githubRequestTimeout
	}
	if options.MaxCandidates <= 0 || options.MaxCandidates > githubMaxCandidates {
		options.MaxCandidates = githubMaxCandidates
	}
	return &GitHubProvider{
		apiBaseURL:    strings.TrimRight(options.APIBaseURL, "/"),
		rawBaseURL:    strings.TrimRight(options.RawBaseURL, "/"),
		authToken:     options.AuthToken,
		client:        options.Client,
		timeout:       options.Timeout,
		maxCandidates: options.MaxCandidates,
		cache:         map[string][]SearchResult{},
	}
}

func (p *GitHubProvider) Name() string {
	return "github"
}

// Search implements SearchProvider. Code-search results are only candidates:
// each is fetched from the repository's default branch and passed through the
// existing GitHub SKILL.md validator before it can become a result.
func (p *GitHubProvider) Search(ctx context.Context, query SearchQuery) ([]SearchResult, error) {
	query = query.Normalized()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cacheKey := query.Text + "\x00" + strconv.Itoa(query.Limit)
	if results, ok := p.cached(cacheKey); ok {
		return results, nil
	}

	requestCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	candidates, err := p.searchCandidates(requestCtx, query)
	if err != nil {
		return nil, err
	}
	results := make([]SearchResult, 0, len(candidates))
	for _, candidate := range candidates {
		if len(results) >= p.resultLimit(query) {
			break
		}
		result, ok, err := p.validateCandidate(requestCtx, candidate)
		if err != nil {
			// Candidate content is normally best-effort, but a canceled or timed
			// out validation means this provider did not finish its bounded search.
			// Returning the request context's error keeps the failed response out of
			// the cache and lets the federated aggregator report this provider alone.
			if requestErr := requestCtx.Err(); requestErr != nil {
				return nil, requestErr
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			continue
		}
		if ok {
			results = append(results, result)
		}
	}
	p.store(cacheKey, results)
	return append([]SearchResult(nil), results...), nil
}

type githubCandidate struct {
	Path       string
	SHA        string
	HTMLURL    string
	Repository struct {
		FullName      string `json:"full_name"`
		Name          string `json:"name"`
		Archived      bool   `json:"archived"`
		DefaultBranch string `json:"default_branch"`
		HTMLURL       string `json:"html_url"`
		Stars         int    `json:"stargazers_count"`
		UpdatedAt     string `json:"updated_at"`
		Owner         struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repository"`
}

func (p *GitHubProvider) searchCandidates(ctx context.Context, query SearchQuery) ([]githubCandidate, error) {
	endpoint, err := url.Parse(p.apiBaseURL + "/search/code")
	if err != nil {
		return nil, errors.New("invalid GitHub search endpoint")
	}
	params := endpoint.Query()
	// GitHub's filename qualifier keeps the broad code-search result set focused
	// on candidate skill files before their frontmatter is validated locally.
	params.Set("q", "filename:SKILL.md "+query.Text)
	params.Set("per_page", strconv.Itoa(p.maxCandidates))
	endpoint.RawQuery = params.Encode()

	var response struct {
		Items []githubCandidate `json:"items"`
	}
	if err := p.getJSON(ctx, endpoint.String(), &response); err != nil {
		return nil, err
	}
	filtered := make([]githubCandidate, 0, len(response.Items))
	for _, candidate := range response.Items {
		if candidate.Repository.Archived || candidate.Repository.FullName == "" || candidate.Repository.DefaultBranch == "" || candidate.Path == "" {
			continue
		}
		if !strings.EqualFold(path.Base(candidate.Path), "SKILL.md") {
			continue
		}
		filtered = append(filtered, candidate)
	}
	return filtered, nil
}

func (p *GitHubProvider) validateCandidate(ctx context.Context, candidate githubCandidate) (SearchResult, bool, error) {
	rawURL := p.rawSkillURL(candidate.Repository.FullName, candidate.Repository.DefaultBranch, candidate.Path)
	content, err := p.getText(ctx, rawURL)
	if err != nil {
		return SearchResult{}, false, err
	}
	skill, ok := github.ParseRemoteSkill(content, false)
	if !ok {
		return SearchResult{}, false, nil
	}
	updatedAt, _ := time.Parse(time.RFC3339, candidate.Repository.UpdatedAt)
	owner := candidate.Repository.Owner.Login
	if owner == "" {
		owner, _ = sourceParts(candidate.Repository.FullName)
	}
	installURL := candidate.HTMLURL
	if installURL == "" {
		installURL = fmt.Sprintf(
			"https://github.com/%s/blob/%s/%s",
			candidate.Repository.FullName,
			url.PathEscape(candidate.Repository.DefaultBranch),
			candidate.Path,
		)
	}
	return SearchResult{
		Name:            skill.Name,
		Description:     skill.Description,
		Provider:        "github",
		CanonicalSource: candidate.Repository.FullName,
		InstallURL:      installURL,
		Author:          owner,
		Repository:      candidate.Repository.Name,
		SkillPath:       candidate.Path,
		Stars:           candidate.Repository.Stars,
		UpdatedAt:       updatedAt,
		ProviderMetadata: map[string]any{
			"path":           candidate.Path,
			"sha":            candidate.SHA,
			"raw_url":        rawURL,
			"default_branch": candidate.Repository.DefaultBranch,
			"archived":       candidate.Repository.Archived,
			"html_url":       candidate.Repository.HTMLURL,
		},
	}, true, nil
}

func (p *GitHubProvider) getJSON(ctx context.Context, endpoint string, target any) error {
	data, err := p.get(ctx, endpoint)
	if err != nil {
		return err
	}
	if json.Unmarshal(data, target) != nil {
		return errors.New("decode GitHub search response")
	}
	return nil
}

func (p *GitHubProvider) getText(ctx context.Context, endpoint string) (string, error) {
	data, err := github.Fetch(ctx, p.client, endpoint, "")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (p *GitHubProvider) get(ctx context.Context, endpoint string) ([]byte, error) {
	body, err := github.Fetch(ctx, p.client, endpoint, p.authToken)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		if strings.HasPrefix(err.Error(), "GitHub request failed:") {
			return nil, fmt.Errorf("GitHub search request failed: %s", strings.TrimPrefix(err.Error(), "GitHub request failed: "))
		}
		return nil, errors.New("GitHub search request failed")
	}
	return body, nil
}

func (p *GitHubProvider) rawSkillURL(ownerRepo, branch, skillPath string) string {
	segments := make([]string, 0, len(strings.Split(skillPath, "/"))+2)
	segments = append(segments, strings.Split(ownerRepo, "/")...)
	segments = append(segments, branch)
	segments = append(segments, strings.Split(skillPath, "/")...)
	for index, segment := range segments {
		segments[index] = url.PathEscape(segment)
	}
	return p.rawBaseURL + "/" + strings.Join(segments, "/")
}

func (p *GitHubProvider) resultLimit(query SearchQuery) int {
	if query.Limit > 0 && query.Limit < p.maxCandidates {
		return query.Limit
	}
	return p.maxCandidates
}

func (p *GitHubProvider) cached(key string) ([]SearchResult, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	results, ok := p.cache[key]
	return append([]SearchResult(nil), results...), ok
}

func (p *GitHubProvider) store(key string, results []SearchResult) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cache[key] = append([]SearchResult(nil), results...)
}

func githubEnvDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
