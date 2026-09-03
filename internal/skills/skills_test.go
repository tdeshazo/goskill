package skills

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
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

func TestConformanceFixtures(t *testing.T) {
	for _, kind := range []string{"valid", "invalid"} {
		entries, err := os.ReadDir(filepath.Join("..", "..", "testdata", "conformance", kind))
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			t.Run(kind+"/"+entry.Name(), func(t *testing.T) {
				dir := filepath.Join("..", "..", "testdata", "conformance", kind, entry.Name())
				diagnostics := ValidateSkillDirectory(dir)
				if kind == "valid" {
					if len(diagnostics) != 0 {
						t.Fatalf("diagnostics = %#v", diagnostics)
					}
					return
				}
				wantData, err := os.ReadFile(filepath.Join(dir, "expected.txt"))
				if err != nil {
					t.Fatal(err)
				}
				want := strings.TrimSpace(string(wantData))
				if len(diagnostics) != 1 || diagnostics[0].Code != want {
					t.Fatalf("diagnostics = %#v, want one %s", diagnostics, want)
				}
			})
		}
	}
}

func TestRulesHaveUniqueStableCodes(t *testing.T) {
	rules := Rules()
	if len(rules) == 0 {
		t.Fatal("rule catalog is empty")
	}
	seen := map[string]bool{}
	codes := make([]string, 0, len(rules))
	for _, rule := range rules {
		if !strings.HasPrefix(rule.Code, "AS") || seen[rule.Code] {
			t.Fatalf("invalid or duplicate rule %#v", rule)
		}
		seen[rule.Code] = true
		codes = append(codes, rule.Code)
	}
	sort.Strings(codes)
	if len(codes) != len(rules) {
		t.Fatalf("codes = %#v", codes)
	}
}

func TestSortDiagnostics(t *testing.T) {
	diagnostics := []Diagnostic{
		{Path: "b", Code: RuleDescriptionType},
		{Path: "a", Code: RuleNameType},
		{Path: "a", Code: RuleDescriptionType},
	}
	SortDiagnostics(diagnostics)
	got := []string{diagnostics[0].Path + diagnostics[0].Code, diagnostics[1].Path + diagnostics[1].Code, diagnostics[2].Path + diagnostics[2].Code}
	want := []string{"a" + RuleNameType, "a" + RuleDescriptionType, "b" + RuleDescriptionType}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v", got, want)
	}
}
