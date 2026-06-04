package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/tdeshazo/goskill/internal/lockfile"
)

const (
	defaultUpdateCheckInterval = 24 * time.Hour
)

var defaultUpdateRepo = "tdeshazo/goskill"

type releaseInfo struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

type updateCheckCache struct {
	CheckedAt string `json:"checkedAt"`
	Latest    string `json:"latest"`
	URL       string `json:"url"`
}

func (a App) warnIfNewerRelease(cmd string) {
	if a.Stderr == nil || updateCheckDisabled() || skipUpdateCheckCommand(cmd) {
		return
	}
	latest, releaseURL, ok := latestRelease(a.Version)
	if !ok || !newerVersion(latest, a.Version) {
		return
	}
	if releaseURL == "" {
		releaseURL = "https://github.com/" + updateRepo() + "/releases/latest"
	}
	fmt.Fprint(a.Stderr, renderVersionWarning(normalizeVersion(latest), normalizeVersion(a.Version), releaseURL))
}

func renderVersionWarning(latest, current, releaseURL string) string {
	latestVersion := selectorSuccessStyle.Bold(true).Render(latest)
	currentVersion := selectorWarningStyle.Bold(true).Render(current)
	lines := []string{
		selectorWarningStyle.Render("◆") + "  " + selectorTitleStyle.Render("Update available"),
		fmt.Sprintf("%s  A newer goskill release is available: %s (current: %s)", selectorBar(), latestVersion, currentVersion),
		fmt.Sprintf("%s  %s", selectorBar(), selectorPathStyle.Render(releaseURL)),
		selectorBarStyle.Render("└"),
	}
	return strings.Join(lines, "\n") + "\n"
}

func updateCheckDisabled() bool {
	for _, key := range []string{"GOSKILL_NO_UPDATE_CHECK", "GOSKILL_UPDATE_CHECK_DISABLED"} {
		if envTruthy(os.Getenv(key)) {
			return true
		}
	}
	value := strings.TrimSpace(strings.ToLower(os.Getenv("GOSKILL_UPDATE_CHECK")))
	return value == "0" || value == "false" || value == "no" || value == "off"
}

func envTruthy(value string) bool {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func skipUpdateCheckCommand(cmd string) bool {
	switch cmd {
	case "--help", "-h", "help", "--version", "-v":
		return true
	default:
		return false
	}
}

func latestRelease(current string) (string, string, bool) {
	if _, ok := parseVersion(current); !ok {
		return "", "", false
	}
	if cache, ok := readUpdateCheckCache(); ok {
		return cache.Latest, cache.URL, true
	}
	release, ok := fetchLatestRelease()
	if !ok {
		return "", "", false
	}
	_ = writeUpdateCheckCache(updateCheckCache{
		CheckedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Latest:    release.TagName,
		URL:       release.HTMLURL,
	})
	return release.TagName, release.HTMLURL, true
}

func readUpdateCheckCache() (updateCheckCache, bool) {
	var cache updateCheckCache
	data, err := os.ReadFile(updateCheckCachePath())
	if err != nil || json.Unmarshal(data, &cache) != nil || cache.Latest == "" {
		return updateCheckCache{}, false
	}
	checkedAt, err := time.Parse(time.RFC3339Nano, cache.CheckedAt)
	if err != nil || time.Since(checkedAt) > updateCheckInterval() {
		return updateCheckCache{}, false
	}
	return cache, true
}

func writeUpdateCheckCache(cache updateCheckCache) error {
	path := updateCheckCachePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func updateCheckCachePath() string {
	if path := os.Getenv("GOSKILL_UPDATE_CACHE"); path != "" {
		return path
	}
	return filepath.Join(filepath.Dir(lockfile.GlobalPath()), "goskill-update-check.json")
}

func updateCheckInterval() time.Duration {
	raw := os.Getenv("GOSKILL_UPDATE_CHECK_INTERVAL")
	if raw == "" {
		return defaultUpdateCheckInterval
	}
	duration, err := time.ParseDuration(raw)
	if err == nil && duration >= 0 {
		return duration
	}
	seconds, err := strconv.Atoi(raw)
	if err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	return defaultUpdateCheckInterval
}

func fetchLatestRelease() (releaseInfo, bool) {
	u := os.Getenv("GOSKILL_UPDATE_URL")
	if u == "" {
		apiBase := strings.TrimSuffix(envDefault("GITHUB_API_URL", "https://api.github.com"), "/")
		u = fmt.Sprintf("%s/repos/%s/releases/latest", apiBase, updateRepo())
	}
	ctx, cancel := context.WithTimeout(context.Background(), updateCheckTimeout())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return releaseInfo{}, false
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "goskill")
	if token := first(os.Getenv("GITHUB_TOKEN"), os.Getenv("GH_TOKEN")); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return releaseInfo{}, false
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return releaseInfo{}, false
	}
	var release releaseInfo
	if json.NewDecoder(res.Body).Decode(&release) != nil || release.TagName == "" {
		return releaseInfo{}, false
	}
	return release, true
}

func updateRepo() string {
	return envDefault("GOSKILL_UPDATE_REPO", defaultUpdateRepo)
}

func updateCheckTimeout() time.Duration {
	raw := os.Getenv("GOSKILL_UPDATE_TIMEOUT")
	if raw == "" {
		return 2 * time.Second
	}
	duration, err := time.ParseDuration(raw)
	if err == nil && duration > 0 {
		return duration
	}
	millis, err := strconv.Atoi(raw)
	if err == nil && millis > 0 {
		return time.Duration(millis) * time.Millisecond
	}
	return 2 * time.Second
}

func newerVersion(candidate, current string) bool {
	candidateParts, ok := parseVersion(candidate)
	if !ok {
		return false
	}
	currentParts, ok := parseVersion(current)
	if !ok {
		return false
	}
	for i := 0; i < len(candidateParts); i++ {
		if candidateParts[i] > currentParts[i] {
			return true
		}
		if candidateParts[i] < currentParts[i] {
			return false
		}
	}
	return false
}

func parseVersion(raw string) ([3]int, bool) {
	var out [3]int
	version := normalizeVersion(raw)
	if version == "" {
		return out, false
	}
	parts := strings.Split(version, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return out, false
	}
	for i, part := range parts {
		if part == "" {
			return out, false
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

func normalizeVersion(raw string) string {
	version := strings.TrimSpace(raw)
	version = strings.TrimPrefix(version, "refs/tags/")
	version = strings.TrimPrefix(version, "v")
	if cut := strings.IndexAny(version, "-+"); cut >= 0 {
		version = version[:cut]
	}
	return version
}
