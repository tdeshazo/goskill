package commands

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestWarnIfNewerRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"tag_name":"v0.2.0","html_url":"https://example.test/releases/v0.2.0"}`)
	}))
	defer server.Close()

	t.Setenv("GOSKILL_UPDATE_URL", server.URL)
	t.Setenv("GOSKILL_UPDATE_CACHE", filepath.Join(t.TempDir(), "cache.json"))
	t.Setenv("GOSKILL_UPDATE_CHECK_INTERVAL", "0")

	var stderr bytes.Buffer
	app := App{Version: "0.1.0", Stderr: &stderr}
	app.warnIfNewerRelease("list")

	output := stderr.String()
	if !strings.Contains(output, "A newer goskill release is available: 0.2.0 (current: 0.1.0)") {
		t.Fatalf("missing update warning:\n%s", output)
	}
	if !strings.Contains(output, "https://example.test/releases/v0.2.0") {
		t.Fatalf("missing release URL:\n%s", output)
	}
}

func TestWarnIfNewerReleaseSkipsCurrentAndDisabled(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprintln(w, `{"tag_name":"v0.1.0","html_url":"https://example.test/releases/v0.1.0"}`)
	}))
	defer server.Close()

	t.Setenv("GOSKILL_UPDATE_URL", server.URL)
	t.Setenv("GOSKILL_UPDATE_CACHE", filepath.Join(t.TempDir(), "cache.json"))
	t.Setenv("GOSKILL_UPDATE_CHECK_INTERVAL", "0")

	var stderr bytes.Buffer
	app := App{Version: "0.1.0", Stderr: &stderr}
	app.warnIfNewerRelease("list")

	if stderr.String() != "" {
		t.Fatalf("unexpected warning:\n%s", stderr.String())
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}

	t.Setenv("GOSKILL_NO_UPDATE_CHECK", "1")
	app.warnIfNewerRelease("add")
	if calls != 1 {
		t.Fatalf("disabled check should not call server, calls = %d", calls)
	}
}

func TestWarnIfNewerReleaseUsesCache(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprintln(w, `{"tag_name":"v0.2.0","html_url":"https://example.test/releases/v0.2.0"}`)
	}))
	defer server.Close()

	t.Setenv("GOSKILL_UPDATE_URL", server.URL)
	t.Setenv("GOSKILL_UPDATE_CACHE", filepath.Join(t.TempDir(), "cache.json"))

	var stderr bytes.Buffer
	app := App{Version: "0.1.0", Stderr: &stderr}
	app.warnIfNewerRelease("list")
	app.warnIfNewerRelease("list")

	if calls != 1 {
		t.Fatalf("calls = %d, want cached single call", calls)
	}
	if got := strings.Count(stderr.String(), "A newer goskill release is available"); got != 2 {
		t.Fatalf("warning count = %d, output:\n%s", got, stderr.String())
	}
}

func TestNewerVersion(t *testing.T) {
	tests := []struct {
		candidate string
		current   string
		want      bool
	}{
		{"v0.2.0", "0.1.9", true},
		{"0.1.1", "v0.1.0", true},
		{"0.1.0", "0.1.0", false},
		{"0.1.0", "0.2.0", false},
		{"0.1.0-beta.1", "0.1.0", false},
		{"not-a-version", "0.1.0", false},
		{"0.2.0", "test", false},
	}
	for _, tt := range tests {
		if got := newerVersion(tt.candidate, tt.current); got != tt.want {
			t.Fatalf("newerVersion(%q, %q) = %v, want %v", tt.candidate, tt.current, got, tt.want)
		}
	}
}

func TestUpdateRepoUsesBuildDefaultAndEnvOverride(t *testing.T) {
	old := defaultUpdateRepo
	defaultUpdateRepo = "fork-owner/fork-repo"
	defer func() { defaultUpdateRepo = old }()

	if got := updateRepo(); got != "fork-owner/fork-repo" {
		t.Fatalf("updateRepo() = %q, want build default", got)
	}

	t.Setenv("GOSKILL_UPDATE_REPO", "override/repo")
	if got := updateRepo(); got != "override/repo" {
		t.Fatalf("updateRepo() = %q, want env override", got)
	}
}
