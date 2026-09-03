package skills

import (
	"os"
	"path/filepath"
	"runtime"
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

func TestPinnedSpecificationReferenceUsesCentralRepositoryAndShortRevision(t *testing.T) {
	if len(SpecDisplayRevision()) != 7 || SpecDisplayRevision() != SpecRevision[:7] {
		t.Fatalf("display revision = %q, full revision = %q", SpecDisplayRevision(), SpecRevision)
	}
	if got, want := SpecReference(), SpecRepository+"@"+SpecDisplayRevision(); got != want {
		t.Fatalf("spec reference = %q, want %q", got, want)
	}
	if !strings.HasPrefix(SpecSourceURL, SpecRepositoryURL+"/blob/"+SpecRevision) {
		t.Fatalf("spec source URL = %q", SpecSourceURL)
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
		if !(strings.HasPrefix(rule.Code, "AS") || strings.HasPrefix(rule.Code, "GS") || strings.HasPrefix(rule.Code, "GP")) || seen[rule.Code] {
			t.Fatalf("invalid or duplicate rule %#v", rule)
		}
		if !rule.Profile.Valid() || (rule.Source == "" && rule.Rationale == "") {
			t.Fatalf("rule metadata = %#v", rule)
		}
		seen[rule.Code] = true
		codes = append(codes, rule.Code)
	}
	sort.Strings(codes)
	if len(codes) != len(rules) {
		t.Fatalf("codes = %#v", codes)
	}
}

func TestRuleForCodeNormalizesUserInput(t *testing.T) {
	for _, input := range []string{"as001", " AS001 ", "As001"} {
		rule, ok := RuleForCode(input)
		if !ok || rule.Code != RuleSkillMDRequired {
			t.Fatalf("RuleForCode(%q) = %#v, %v", input, rule, ok)
		}
	}
	if got := NormalizeRuleCode(" gp310 "); got != RuleSkillFilename {
		t.Fatalf("NormalizeRuleCode = %q", got)
	}
	if _, ok := RuleForCode("AS 001"); ok {
		t.Fatal("malformed rule code was accepted")
	}
	if got := NormalizeRuleCode("aſ001"); got != "Aſ001" {
		t.Fatalf("NormalizeRuleCode confusable = %q", got)
	}
	if _, ok := RuleForCode("aſ001"); ok {
		t.Fatal("Unicode-confusable rule code was accepted")
	}
}

func TestValidationLocationsUseYAMLNodesAndFileFallback(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "skill")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "SKILL.md")
	content := "---\n" +
		"name: Bad Skill\n" +
		"description: 42\n" +
		"compatibility: \"\"\n" +
		"metadata:\n" +
		"  version: 1\n" +
		"allowed-tools: [read]\n" +
		"unknown: value\n" +
		"---\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	locations := map[string]sourceLocation{}
	for _, diagnostic := range ValidateSkillMD(path) {
		locations[diagnostic.Code] = sourceLocation{Line: diagnostic.Line, Column: diagnostic.Column}
	}
	want := map[string]sourceLocation{
		RuleNameLowercase:       {Line: 2, Column: 7},
		RuleNameDirectory:       {Line: 2, Column: 7},
		RuleDescriptionType:     {Line: 3, Column: 14},
		RuleCompatibilityLength: {Line: 4, Column: 16},
		RuleMetadataValues:      {Line: 6, Column: 12},
		RuleAllowedToolsType:    {Line: 7, Column: 16},
		RuleTopLevelFields:      {Line: 8, Column: 1},
	}
	for code, wantLocation := range want {
		if got := locations[code]; got != wantLocation {
			t.Errorf("%s location = %#v, want %#v", code, got, wantLocation)
		}
	}

	missing := ValidateSkillMD(filepath.Join(dir, "missing.md"))
	if len(missing) != 1 || missing[0].Line != 1 || missing[0].Column != 1 {
		t.Fatalf("missing file location = %#v", missing)
	}
	if err := os.WriteFile(path, []byte("---\ndescription: Description\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	missingField := ValidateSkillMD(path)
	if len(missingField) != 1 || missingField[0].Code != RuleNameRequired || missingField[0].Line != 1 || missingField[0].Column != 1 {
		t.Fatalf("missing field location = %#v", missingField)
	}
	if err := os.WriteFile(path, []byte("---\nname: [unterminated\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	invalid := ValidateSkillMD(path)
	if len(invalid) != 1 || invalid[0].Code != RuleFrontmatterYAML || invalid[0].Line != 2 || invalid[0].Column != 1 {
		t.Fatalf("invalid YAML location = %#v", invalid)
	}
	if err := os.WriteFile(path, []byte("---\nname: skill\nname: duplicate\ndescription: Description\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	duplicate := ValidateSkillMD(path)
	if len(duplicate) != 1 || duplicate[0].Code != RuleFrontmatterYAML || duplicate[0].Line != 3 || duplicate[0].Column != 1 {
		t.Fatalf("duplicate key location = %#v", duplicate)
	}
}

func TestRulesForProfileLayersDeterministically(t *testing.T) {
	tests := []struct {
		profile Profile
		want    []string
	}{
		{profile: ProfileSpec, want: []string{RuleSkillMDRequired, RuleAllowedToolsType}},
		{profile: ProfileRecommended, want: []string{RuleSkillMDRequired, RuleAllowedToolsType, RuleSkillLineCount}},
		{profile: ProfilePortable, want: []string{RuleSkillMDRequired, RuleAllowedToolsType, RuleSkillLineCount, RuleSkillFilename}},
	}
	for _, test := range tests {
		t.Run(string(test.profile), func(t *testing.T) {
			rules := RulesForProfile(test.profile)
			codes := []string{rules[0].Code, rules[len(specRuleCatalog)-1].Code}
			if test.profile == ProfileRecommended || test.profile == ProfilePortable {
				codes = append(codes, rules[len(specRuleCatalog)].Code)
			}
			if test.profile == ProfilePortable {
				codes = append(codes, rules[len(specRuleCatalog)+len(recommendedRuleCatalog)].Code)
			}
			if strings.Join(codes, ",") != strings.Join(test.want, ",") {
				t.Fatalf("rules = %#v, want %v", codes, test.want)
			}
		})
	}
}

func TestValidateProfiles(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	long := "---\nname: demo\ndescription: Description\n---\n" + strings.Repeat("content\n", 497)
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(long), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		profile  Profile
		path     string
		wantCode []string
		valid    bool
	}{
		{name: "spec excludes guidance", profile: ProfileSpec, path: filepath.Join(dir, "SKILL.md"), valid: true},
		{name: "recommended warns on 501 lines", profile: ProfileRecommended, path: filepath.Join(dir, "SKILL.md"), wantCode: []string{RuleSkillLineCount}, valid: true},
		{name: "portable includes guidance", profile: ProfilePortable, path: filepath.Join(dir, "SKILL.md"), wantCode: []string{RuleSkillLineCount}, valid: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			diagnostics := ValidateSkillMDWithProfile(test.path, test.profile)
			codes := make([]string, 0, len(diagnostics))
			errors := 0
			for _, diagnostic := range diagnostics {
				codes = append(codes, diagnostic.Code)
				if diagnostic.Severity == SeverityError {
					errors++
				}
			}
			if strings.Join(codes, ",") != strings.Join(test.wantCode, ",") || (errors == 0) != test.valid {
				t.Fatalf("diagnostics = %#v, errors = %d", diagnostics, errors)
			}
			if test.profile == ProfileRecommended && (len(diagnostics) != 1 || diagnostics[0].Severity != SeverityWarning || !strings.Contains(diagnostics[0].Message, "501 lines")) {
				t.Fatalf("recommended diagnostic = %#v", diagnostics)
			}
		})
	}

	if err := os.WriteFile(filepath.Join(dir, "skill.md"), []byte("---\nname: demo\ndescription: Description\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lowercase := filepath.Join(dir, "skill.md")
	if got := ValidateSkillMDWithProfile(lowercase, ProfileSpec); len(got) != 0 {
		t.Fatalf("spec lowercase diagnostics = %#v", got)
	}
	portable := ValidateSkillMDWithProfile(lowercase, ProfilePortable)
	if len(portable) != 1 || portable[0].Code != RuleSkillFilename || portable[0].Severity != SeverityError {
		t.Fatalf("portable lowercase diagnostics = %#v", portable)
	}
}

func TestValidationSkillFilePreservesDirectoryEntryCase(t *testing.T) {
	lowercaseDir := filepath.Join(t.TempDir(), "demo")
	if err := os.MkdirAll(lowercaseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lowercaseDir, "skill.md"), []byte("---\nname: demo\ndescription: Description\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path, ok := ValidationSkillFile(lowercaseDir)
	if !ok || filepath.Base(path) != "skill.md" {
		t.Fatalf("ValidationSkillFile() = %q, %v; want actual lowercase entry", path, ok)
	}
	diagnostics := ValidateSkillDirectoryWithProfile(lowercaseDir, ProfilePortable)
	if len(diagnostics) != 1 || diagnostics[0].Code != RuleSkillFilename {
		t.Fatalf("portable lowercase diagnostics = %#v", diagnostics)
	}

	bothDir := filepath.Join(t.TempDir(), "both")
	if err := os.MkdirAll(bothDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"skill.md", "SKILL.md"} {
		if err := os.WriteFile(filepath.Join(bothDir, name), []byte("fixture"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(bothDir)
	if err != nil {
		t.Fatal(err)
	}
	exactEntries := map[string]bool{}
	for _, entry := range entries {
		exactEntries[entry.Name()] = true
	}
	path, ok = ValidationSkillFile(bothDir)
	if !ok {
		t.Fatal("ValidationSkillFile() did not find either accepted entry")
	}
	if exactEntries["SKILL.md"] && exactEntries["skill.md"] && filepath.Base(path) != "SKILL.md" {
		t.Fatalf("ValidationSkillFile() = %q; want uppercase preference when both entries exist", path)
	}
	if !exactEntries[filepath.Base(path)] {
		t.Fatalf("ValidationSkillFile() returned synthetic casing %q; entries = %#v", path, exactEntries)
	}
}

func TestValidationSkillFileFallsBackWhenDirectoryCannotBeListed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX directory permissions are required")
	}
	tests := []struct {
		name     string
		filename string
		symlink  bool
	}{
		{name: "uppercase symlink", filename: "SKILL.md", symlink: true},
		{name: "lowercase file", filename: "skill.md"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "demo")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			content := []byte("---\nname: demo\ndescription: Description\n---\n")
			if test.symlink {
				target := filepath.Join(root, "target.md")
				if err := os.WriteFile(target, content, 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, filepath.Join(dir, test.filename)); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(filepath.Join(dir, test.filename), content, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(dir, 0o111); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
			if _, err := os.ReadDir(dir); err == nil {
				t.Skip("filesystem or test identity can still list the search-only directory")
			}

			path, ok := ValidationSkillFile(dir)
			if !ok || filepath.Base(path) != test.filename {
				t.Fatalf("ValidationSkillFile() fallback = %q, %v; want %q", path, ok, test.filename)
			}
			if diagnostics := ValidateSkillDirectory(dir); len(diagnostics) != 0 {
				t.Fatalf("searchable skill diagnostics = %#v", diagnostics)
			}
		})
	}
}

func TestSortDiagnostics(t *testing.T) {
	diagnostics := []Diagnostic{
		{Path: "b", Line: 1, Column: 1, Code: RuleDescriptionType},
		{Path: "a", Line: 4, Column: 1, Code: RuleNameType},
		{Path: "a", Line: 3, Column: 2, Code: RuleDescriptionType},
		{Path: "a", Line: 3, Column: 1, Code: RuleNameLength},
	}
	SortDiagnostics(diagnostics)
	got := []string{diagnostics[0].Code, diagnostics[1].Code, diagnostics[2].Code, diagnostics[3].Code}
	want := []string{RuleNameLength, RuleDescriptionType, RuleNameType, RuleDescriptionType}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v", got, want)
	}
}
