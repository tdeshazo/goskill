package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	trueFoundryDefaultURL = "https://raw.githubusercontent.com/truefoundry/awesome-skills-registry/main/dist/ai-skills.json"
	trueFoundryTimeout    = 10 * time.Second
	maxCatalogSize        = 20 * 1024 * 1024
)

// TrueFoundryProvider adapts the awesome-skills-registry JSON export. The
// documented export is a JSON array with id, display_name, description,
// authors, tags, category, source.repo, source.path, metadata.stars, and
// added_at. Unknown fields remain available through Entry.Metadata.
type TrueFoundryProvider struct {
	URL     string
	Client  *http.Client
	Timeout time.Duration
}

// NewTrueFoundryProvider creates the first built-in static catalog adapter.
func NewTrueFoundryProvider(url string, client *http.Client) *TrueFoundryProvider {
	if url == "" {
		url = trueFoundryDefaultURL
	}
	if client == nil {
		client = &http.Client{Timeout: trueFoundryTimeout}
	}
	return &TrueFoundryProvider{URL: url, Client: client, Timeout: trueFoundryTimeout}
}

func (p *TrueFoundryProvider) Name() string {
	return "truefoundry"
}

func (p *TrueFoundryProvider) Fetch(ctx context.Context) ([]byte, error) {
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = trueFoundryTimeout
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, p.URL, nil)
	if err != nil {
		return nil, errors.New("create TrueFoundry catalog request")
	}
	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	res, err := client.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, errors.New("fetch TrueFoundry catalog")
	}
	defer res.Body.Close()
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("fetch TrueFoundry catalog: %s", res.Status)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, maxCatalogSize+1))
	if err != nil || len(body) > maxCatalogSize {
		return nil, errors.New("read TrueFoundry catalog")
	}
	return body, nil
}

func (p *TrueFoundryProvider) Parse(data []byte) ([]Entry, error) {
	var records []json.RawMessage
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, errors.New("decode TrueFoundry catalog")
	}
	entries := make([]Entry, 0, len(records))
	for _, record := range records {
		entry, ok := parseTrueFoundryEntry(record)
		if ok {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

func parseTrueFoundryEntry(raw json.RawMessage) (Entry, bool) {
	metadata := map[string]any{}
	if json.Unmarshal(raw, &metadata) != nil {
		return Entry{}, false
	}
	source, ok := metadata["source"].(map[string]any)
	if !ok {
		return Entry{}, false
	}
	id := catalogString(metadata, "id")
	repo := catalogString(source, "repo")
	sourcePath := catalogString(source, "path")
	if id == "" || repo == "" || sourcePath == "" {
		return Entry{}, false
	}
	displayName := catalogString(metadata, "display_name", "displayName")
	if displayName == "" {
		displayName = id
	}
	addedAt, _ := time.Parse(time.RFC3339, catalogString(metadata, "added_at", "addedAt"))
	entry := Entry{
		ID:          id,
		DisplayName: displayName,
		Description: catalogString(metadata, "description"),
		Tags:        catalogStrings(metadata["tags"]),
		Category:    catalogString(metadata, "category"),
		SourceRepo:  repo,
		SourcePath:  sourcePath,
		Authors:     catalogStrings(metadata["authors"]),
		Stars:       catalogInt(metadataValue(metadata, "metadata.stars")),
		AddedAt:     addedAt,
		Metadata:    metadata,
	}
	return entry, true
}

func catalogString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := values[key].(string)
		if ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func catalogStrings(value any) []string {
	values, ok := value.([]any)
	if !ok {
		return []string{}
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
			result = append(result, strings.TrimSpace(text))
		}
	}
	return result
}

func catalogInt(value any) int {
	switch value := value.(type) {
	case float64:
		return int(value)
	case string:
		var parsed int
		if _, err := fmt.Sscanf(value, "%d", &parsed); err == nil {
			return parsed
		}
	}
	return 0
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
