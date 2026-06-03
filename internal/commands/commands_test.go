package commands

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddListRemoveLocalSkill(t *testing.T) {
	project := t.TempDir()
	source := makeSkill(t, t.TempDir(), "demo", "Demo skill")
	var out bytes.Buffer
	app := App{Version: "test", Stdout: &out, Stderr: &out, Cwd: project}
	if err := app.Run([]string{"add", source, "-y", "-a", "codex", "cursor"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(project, ".agents", "skills", "demo", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := app.Run([]string{"list", "--json"}); err != nil {
		t.Fatal(err)
	}
	var listed []map[string]any
	if err := json.Unmarshal(out.Bytes(), &listed); err != nil {
		t.Fatalf("json output: %v\n%s", err, out.String())
	}
	if len(listed) != 1 || listed[0]["name"] != "demo" {
		t.Fatalf("listed = %#v", listed)
	}
	out.Reset()
	if err := app.Run([]string{"remove", "demo", "-y", "-a", "codex"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(project, ".agents", "skills", "demo")); !os.IsNotExist(err) {
		t.Fatalf("skill should be removed, err=%v", err)
	}
}

func TestAddClaudeCreatesSymlinkWhenProjectClaudeExists(t *testing.T) {
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	source := makeSkill(t, t.TempDir(), "demo", "Demo skill")
	var out bytes.Buffer
	app := App{Version: "test", Stdout: &out, Stderr: &out, Cwd: project}
	if err := app.Run([]string{"add", source, "-y", "-a", "claude-code"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(filepath.Join(project, ".claude", "skills", "demo"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected symlink, mode=%s", info.Mode())
	}
}

func TestAddMultiSkillSourceWithSkillFilter(t *testing.T) {
	project := t.TempDir()
	source := makeMultiSkillSource(t, "alpha", "beta")
	var out bytes.Buffer
	app := App{Version: "test", Stdout: &out, Stderr: &out, Cwd: project}
	if err := app.Run([]string{"add", source, "-y", "-a", "codex", "--skill", "beta"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(project, ".agents", "skills", "beta", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(project, ".agents", "skills", "alpha")); !os.IsNotExist(err) {
		t.Fatalf("alpha should not be installed, err=%v", err)
	}
}

func TestAddMultiSkillSourcePromptsForSelection(t *testing.T) {
	project := t.TempDir()
	source := makeMultiSkillSource(t, "alpha", "beta")
	var out bytes.Buffer
	app := App{
		Version: "test",
		Stdin:   strings.NewReader("2\n"),
		Stdout:  &out,
		Stderr:  &out,
		Cwd:     project,
	}
	if err := app.Run([]string{"add", source, "-a", "codex"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Multiple skills found") {
		t.Fatalf("expected selection prompt, got: %s", out.String())
	}
	if _, err := os.Stat(filepath.Join(project, ".agents", "skills", "beta", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(project, ".agents", "skills", "alpha")); !os.IsNotExist(err) {
		t.Fatalf("alpha should not be installed, err=%v", err)
	}
}

func TestInitAndSyncAndInstallFromLock(t *testing.T) {
	project := t.TempDir()
	var out bytes.Buffer
	app := App{Version: "test", Stdout: &out, Stderr: &out, Cwd: project}
	if err := app.Run([]string{"init", "my-skill"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(project, "my-skill", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	pkgSkill := filepath.Join(project, "node_modules", "@acme", "pkg", "skills", "tool")
	if err := os.MkdirAll(pkgSkill, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, pkgSkill, "tool", "Tool skill")
	if err := app.Run([]string{"experimental_sync", "-y", "-a", "codex"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(project, ".agents", "skills", "tool", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(project, ".agents")); err != nil {
		t.Fatal(err)
	}
	if err := app.Run([]string{"experimental_install"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(project, ".agents", "skills", "tool", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
}

func TestCheckUsesMockedGitHubTree(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	t.Setenv("GITHUB_TREE_FIXTURE", `{"sha":"root","tree":[{"path":"skills/demo","type":"tree","sha":"new-tree"},{"path":"skills/demo/SKILL.md","type":"blob","sha":"blob"}]}`)
	if err := os.MkdirAll(filepath.Join(state, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "skills", ".skill-lock.json"), []byte(`{"version":3,"skills":{"demo":{"source":"owner/repo","sourceType":"github","sourceUrl":"owner/repo","ref":"main","skillPath":"skills/demo/SKILL.md","skillFolderHash":"old-tree","installedAt":"x","updatedAt":"x"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	app := App{Version: "test", Stdout: &out, Stderr: &out, Cwd: t.TempDir()}
	if err := app.Run([]string{"check", "-g"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Update available: demo") {
		t.Fatalf("output = %s", out.String())
	}
}

func TestValidateLocalSkill(t *testing.T) {
	project := t.TempDir()
	makeSkill(t, project, "demo-skill", "Demo skill")
	var out bytes.Buffer
	app := App{Version: "test", Stdout: &out, Stderr: &out, Cwd: project}
	if err := app.Run([]string{"validate", "demo-skill"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Validated 1 skill(s): OK") {
		t.Fatalf("output = %s", out.String())
	}
	if err := app.Run([]string{"validate"}); err == nil || !strings.Contains(err.Error(), "usage: skills validate <skills>") {
		t.Fatalf("expected usage error, got %v", err)
	}
}

func TestValidateReportsFrontmatterSpecIssues(t *testing.T) {
	source := t.TempDir()
	dir := filepath.Join(source, "bad-skill")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: Bad Skill\ndescription: " + strings.Repeat("x", 1025) + "\n---\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	app := App{Version: "test", Stdout: &out, Stderr: &out, Cwd: t.TempDir()}
	err := app.Run([]string{"validate", dir})
	if err == nil || !strings.Contains(err.Error(), "validation failed: 4 issue(s)") {
		t.Fatalf("expected validation failure, got %v", err)
	}
	for _, want := range []string{
		"name must match parent directory",
		"name must be lowercase",
		"name contains invalid characters",
		"description must be 1024 characters or fewer",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q in output:\n%s", want, out.String())
		}
	}
}

func TestValidateAllowsUnicodeNFKCNames(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "café")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	decomposedName := "cafe\u0301"
	writeSkill(t, dir, decomposedName, "Unicode skill")
	var out bytes.Buffer
	app := App{Version: "test", Stdout: &out, Stderr: &out, Cwd: root}
	if err := app.Run([]string{"validate", "café"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Validated 1 skill(s): OK") {
		t.Fatalf("output = %s", out.String())
	}
}

func TestValidateReportsNameFormatIssues(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "my--skill")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, dir, "my--skill", "Demo skill")
	var out bytes.Buffer
	app := App{Version: "test", Stdout: &out, Stderr: &out, Cwd: root}
	err := app.Run([]string{"validate", "my--skill"})
	if err == nil || !strings.Contains(err.Error(), "validation failed: 1 issue(s)") {
		t.Fatalf("expected validation failure, got %v", err)
	}
	if !strings.Contains(out.String(), "name cannot contain consecutive hyphens") {
		t.Fatalf("output = %s", out.String())
	}
}

func TestValidateReportsUnexpectedFieldsAndCompatibilityIssues(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "demo-skill")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: demo-skill\ndescription: Demo skill\nunknown_field: no\ncompatibility: " + strings.Repeat("x", 501) + "\n---\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	app := App{Version: "test", Stdout: &out, Stderr: &out, Cwd: root}
	err := app.Run([]string{"validate", "demo-skill"})
	if err == nil || !strings.Contains(err.Error(), "validation failed: 2 issue(s)") {
		t.Fatalf("expected validation failure, got %v", err)
	}
	for _, want := range []string{
		"unexpected frontmatter fields: unknown_field",
		"compatibility must be 500 characters or fewer",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q in output:\n%s", want, out.String())
		}
	}
}

func TestValidateAcceptsLowercaseSkillMD(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "demo-skill")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skill.md"), []byte("---\nname: demo-skill\ndescription: Demo skill\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	app := App{Version: "test", Stdout: &out, Stderr: &out, Cwd: root}
	if err := app.Run([]string{"validate", "demo-skill"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Validated 1 skill(s): OK") {
		t.Fatalf("output = %s", out.String())
	}
}

func TestValidateReportsInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: [unterminated\ndescription: Demo\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	app := App{Version: "test", Stdout: &out, Stderr: &out, Cwd: t.TempDir()}
	err := app.Run([]string{"validate", dir})
	if err == nil || !strings.Contains(err.Error(), "validation failed: 1 issue(s)") {
		t.Fatalf("expected validation failure, got %v", err)
	}
	if !strings.Contains(out.String(), "invalid YAML") {
		t.Fatalf("output = %s", out.String())
	}
}

func TestValidateReportsDuplicateSkillNames(t *testing.T) {
	root := t.TempDir()
	for _, parent := range []string{"alpha", "beta"} {
		dir := filepath.Join(root, parent, "demo-skill")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		writeSkill(t, dir, "demo-skill", "Demo skill")
	}
	var out bytes.Buffer
	app := App{Version: "test", Stdout: &out, Stderr: &out, Cwd: root}
	err := app.Run([]string{"validate", "."})
	if err == nil || !strings.Contains(err.Error(), "validation failed: 2 issue(s)") {
		t.Fatalf("expected validation failure, got %v", err)
	}
	if got := strings.Count(out.String(), `duplicate skill name "demo-skill"`); got != 2 {
		t.Fatalf("expected 2 duplicate messages, got %d:\n%s", got, out.String())
	}
}

func TestValidateReportsMissingLocalReferences(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "demo-skill")
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs", "guide.md"), []byte("# Guide\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: demo-skill\ndescription: Demo skill\n---\n\nSee [guide](docs/guide.md), [missing](docs/missing.md), and [outside](../outside.md).\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	app := App{Version: "test", Stdout: &out, Stderr: &out, Cwd: root}
	err := app.Run([]string{"validate", "demo-skill"})
	if err == nil || !strings.Contains(err.Error(), "validation failed: 2 issue(s)") {
		t.Fatalf("expected validation failure, got %v", err)
	}
	for _, want := range []string{
		"reference does not exist: docs/missing.md",
		"reference escapes skill directory: ../outside.md",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q in output:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "docs/guide.md") {
		t.Fatalf("valid reference should not be reported:\n%s", out.String())
	}
}

func makeSkill(t *testing.T, parent, name, desc string) string {
	t.Helper()
	dir := filepath.Join(parent, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, dir, name, desc)
	return dir
}

func writeSkill(t *testing.T, dir, name, desc string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: "+name+"\ndescription: "+desc+"\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func makeMultiSkillSource(t *testing.T, names ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, name := range names {
		dir := filepath.Join(root, "skills", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		writeSkill(t, dir, name, name+" skill")
	}
	return root
}
