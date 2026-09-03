// Package catalog provides cached, offline-searchable skill catalogs.
package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const defaultTTL = 24 * time.Hour

// Entry is a catalog-normalized skill record. Provider adapters convert their
// remote schema to this form before generic search and rendering integrations
// consume it.
type Entry struct {
	ID          string
	DisplayName string
	Description string
	Tags        []string
	Category    string
	SourceRepo  string
	SourcePath  string
	Authors     []string
	Stars       int
	AddedAt     time.Time
	Metadata    map[string]any
}

// Provider fetches and parses one static catalog.
type Provider interface {
	Name() string
	Fetch(context.Context) ([]byte, error)
	Parse([]byte) ([]Entry, error)
}

// LoadStatus describes cache use for the most recent load.
type LoadStatus struct {
	Cached     bool
	Refreshed  bool
	Stale      bool
	RefreshErr error
}

// Store manages atomically written provider cache snapshots.
type Store struct {
	Dir string
	TTL time.Duration
	Now func() time.Time
}

type cacheFile struct {
	FetchedAt time.Time       `json:"fetched_at"`
	Data      json.RawMessage `json:"data"`
}

// NewStore creates a cache store. An empty directory selects the
// platform-appropriate user cache directory.
func NewStore(dir string, ttl time.Duration) Store {
	if dir == "" {
		dir = DefaultDir()
	}
	if ttl < 0 {
		ttl = defaultTTL
	}
	return Store{Dir: dir, TTL: ttl, Now: time.Now}
}

// DefaultDir resolves the platform-appropriate user cache location. The
// fallback keeps the cache usable on systems where UserCacheDir is unavailable.
func DefaultDir() string {
	if dir, err := os.UserCacheDir(); err == nil && dir != "" {
		return filepath.Join(dir, "goskill", "catalogs")
	}
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		return filepath.Join(home, ".cache", "goskill", "catalogs")
	}
	return filepath.Join(os.TempDir(), "goskill-catalogs")
}

// Load returns cached entries when fresh. Stale or explicitly refreshed
// catalogs are fetched again; a valid stale cache remains usable when refresh
// fails, so transient provider outages do not remove offline search results.
func (s Store) Load(ctx context.Context, provider Provider, refresh bool) ([]Entry, LoadStatus, error) {
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	cached, fetchedAt, cachedEntries, cacheOK := s.read(provider)
	status := LoadStatus{Cached: cacheOK}
	if cacheOK && !refresh && !expired(now(), fetchedAt, s.ttl()) {
		return cachedEntries, status, nil
	}

	data, err := provider.Fetch(ctx)
	if err == nil {
		var entries []Entry
		entries, err = provider.Parse(data)
		if err == nil {
			err = s.write(provider, now(), data)
			if err == nil {
				return entries, LoadStatus{Cached: cached, Refreshed: true}, nil
			}
		}
	}
	if cacheOK {
		status.Stale = true
		status.RefreshErr = err
		return cachedEntries, status, nil
	}
	if err == nil {
		err = errors.New("refresh catalog")
	}
	return nil, status, fmt.Errorf("refresh %s catalog: %w", provider.Name(), err)
}

func (s Store) read(provider Provider) (bool, time.Time, []Entry, bool) {
	data, err := os.ReadFile(s.path(provider))
	if err != nil {
		return false, time.Time{}, nil, false
	}
	var cached cacheFile
	if json.Unmarshal(data, &cached) != nil || cached.FetchedAt.IsZero() || len(cached.Data) == 0 {
		return false, time.Time{}, nil, false
	}
	entries, err := provider.Parse(cached.Data)
	if err != nil {
		return false, time.Time{}, nil, false
	}
	return true, cached.FetchedAt, entries, true
}

func (s Store) write(provider Provider, fetchedAt time.Time, data []byte) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	encoded, err := json.Marshal(cacheFile{FetchedAt: fetchedAt.UTC(), Data: data})
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	temporary, err := os.CreateTemp(s.Dir, ".catalog-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if _, err := temporary.Write(encoded); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, s.path(provider))
}

func (s Store) path(provider Provider) string {
	name := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, provider.Name())
	return filepath.Join(s.Dir, name+".json")
}

func (s Store) ttl() time.Duration {
	if s.TTL >= 0 {
		return s.TTL
	}
	return defaultTTL
}

func expired(now, fetchedAt time.Time, ttl time.Duration) bool {
	return ttl == 0 || now.Sub(fetchedAt) >= ttl
}

// Search filters catalog entries locally. Every query term must appear in the
// normalized textual fields, preserving catalog order for future ranking.
func Search(entries []Entry, query string, limit int) []Entry {
	terms := strings.Fields(strings.ToLower(query))
	results := make([]Entry, 0)
	for _, entry := range entries {
		fields := []string{
			entry.ID,
			entry.DisplayName,
			entry.Description,
			entry.Category,
			entry.SourceRepo,
			entry.SourcePath,
		}
		fields = append(fields, entry.Tags...)
		fields = append(fields, entry.Authors...)
		haystack := strings.ToLower(strings.Join(fields, " "))
		matched := true
		for _, term := range terms {
			if !strings.Contains(haystack, term) {
				matched = false
				break
			}
		}
		if !matched {
			continue
		}
		results = append(results, entry)
		if limit > 0 && len(results) >= limit {
			break
		}
	}
	return results
}
