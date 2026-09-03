// Package remoteskill retrieves a single, directly-addressable SKILL.md.
package remoteskill

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/tdeshazo/goskill/internal/skills"
	"github.com/tdeshazo/goskill/internal/terminal"
)

const (
	requestTimeout  = 10 * time.Second
	maxResponseSize = 5 * 1024 * 1024
)

// Fetch retrieves and validates the minimum frontmatter required for a
// single-file skill. Direct artifact URLs do not imply that supporting files
// are available, so the returned snapshot contains only SKILL.md.
func Fetch(rawURL string) (skills.Skill, error) {
	return fetch(rawURL, "", "")
}

// FetchAuthorized retrieves a direct skill artifact with a bearer token only
// after confirming that its origin matches the configured provider endpoint.
// The token and URL are intentionally omitted from all errors.
func FetchAuthorized(rawURL, token, allowedOrigin string) (skills.Skill, error) {
	if !sameOrigin(rawURL, allowedOrigin) {
		return skills.Skill{}, errors.New("skill URL is outside the configured provider origin")
	}
	return fetchWithClient(rawURL, token, authorizedClient(allowedOrigin))
}

// FetchAuthorizedContents retrieves a supporting artifact from the configured
// provider origin. It uses the same redirect boundary as FetchAuthorized.
func FetchAuthorizedContents(rawURL, token, allowedOrigin string) ([]byte, error) {
	if !sameOrigin(rawURL, allowedOrigin) {
		return nil, errors.New("skill URL is outside the configured provider origin")
	}
	return fetchContents(rawURL, token, authorizedClient(allowedOrigin))
}

func fetch(rawURL, token, _ string) (skills.Skill, error) {
	return fetchWithClient(rawURL, token, http.DefaultClient)
}

func fetchWithClient(rawURL, token string, client *http.Client) (skills.Skill, error) {
	body, err := fetchContents(rawURL, token, client)
	if err != nil {
		return skills.Skill{}, err
	}
	content := string(body)
	frontmatter := skills.ParseFrontmatter(content)
	name, _ := frontmatter["name"].(string)
	description, _ := frontmatter["description"].(string)
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	if name == "" || description == "" {
		return skills.Skill{}, errors.New("skill URL does not contain a valid SKILL.md")
	}
	return skills.Skill{
		Name:        terminal.Metadata(name),
		Description: terminal.Metadata(description),
		RawContent:  content,
		Files: []skills.SnapshotFile{{
			Path:     "SKILL.md",
			Contents: content,
		}},
	}, nil
}

func fetchContents(rawURL, token string, client *http.Client) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, errors.New("create skill URL request")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, errors.New("fetch skill URL")
	}
	defer res.Body.Close()
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		return nil, errors.New("fetch skill URL")
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, maxResponseSize+1))
	if err != nil || len(body) > maxResponseSize {
		return nil, errors.New("read skill URL")
	}
	return body, nil
}

func authorizedClient(allowedOrigin string) *http.Client {
	return &http.Client{
		Timeout: requestTimeout,
		CheckRedirect: func(req *http.Request, _ []*http.Request) error {
			if !sameOrigin(req.URL.String(), allowedOrigin) {
				return errors.New("redirect outside configured provider origin")
			}
			return nil
		},
	}
}

func sameOrigin(rawURL, allowedOrigin string) bool {
	target, err := url.Parse(rawURL)
	if err != nil || target.Scheme == "" || target.Host == "" || target.User != nil {
		return false
	}
	allowed, err := url.Parse(allowedOrigin)
	if err != nil || allowed.Scheme == "" || allowed.Host == "" || allowed.User != nil {
		return false
	}
	return strings.EqualFold(target.Scheme, allowed.Scheme) && strings.EqualFold(target.Host, allowed.Host)
}
