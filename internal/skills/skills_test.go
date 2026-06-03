package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseSkillMD(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	content := "---\nname: Demo Skill\ndescription: Does things\nmetadata:\n  internal: false\n---\n# Demo\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	skill, ok := ParseSkillMD(path, false)
	if !ok {
		t.Fatal("expected skill")
	}
	if skill.Name != "Demo Skill" || skill.Description != "Does things" {
		t.Fatalf("unexpected skill: %#v", skill)
	}
}

func TestDiscoverAndHash(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "skills", "demo")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: demo\ndescription: demo desc\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	found, err := Discover(root, "", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].Name != "demo" {
		t.Fatalf("found = %#v", found)
	}
	hashA, err := FolderHash(skillDir)
	if err != nil {
		t.Fatal(err)
	}
	hashB, err := FolderHash(skillDir)
	if err != nil {
		t.Fatal(err)
	}
	if hashA == "" || hashA != hashB {
		t.Fatalf("hashes not deterministic: %q %q", hashA, hashB)
	}
}

func TestSanitizeName(t *testing.T) {
	if got := SanitizeName("../My Skill!!"); got != "my-skill" {
		t.Fatalf("got %q", got)
	}
}
