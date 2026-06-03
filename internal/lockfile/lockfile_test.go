package lockfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLocalLockRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := AddLocal(dir, "demo", LocalEntry{Source: "owner/repo", SourceType: "github", SkillPath: "skills/demo/SKILL.md", ComputedHash: "abc"}); err != nil {
		t.Fatal(err)
	}
	lock := ReadLocal(dir)
	if lock.Version != 1 || lock.Skills["demo"].ComputedHash != "abc" {
		t.Fatalf("unexpected lock: %#v", lock)
	}
	data, err := os.ReadFile(filepath.Join(dir, "skills-lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		t.Fatal("local lock should end with newline")
	}
}

func TestGlobalLockUsesXDGStateHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	if err := AddGlobal("demo", GlobalEntry{Source: "owner/repo", SourceType: "github", SourceURL: "owner/repo", SkillFolderHash: "tree"}); err != nil {
		t.Fatal(err)
	}
	lock := ReadGlobal()
	if lock.Version != 3 || lock.Skills["demo"].SkillFolderHash != "tree" {
		t.Fatalf("unexpected lock: %#v", lock)
	}
	if _, err := os.Stat(filepath.Join(dir, "skills", ".skill-lock.json")); err != nil {
		t.Fatal(err)
	}
}
