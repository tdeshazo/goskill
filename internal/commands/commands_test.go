package commands

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tdeshazo/goskill/internal/agents"
	"github.com/tdeshazo/goskill/internal/installer"
	"github.com/tdeshazo/goskill/internal/skills"
	"github.com/tdeshazo/goskill/internal/source"
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

func TestListUsesDecoratedOutput(t *testing.T) {
	project := t.TempDir()
	source := makeSkill(t, t.TempDir(), "demo", "Demo skill")
	var out bytes.Buffer
	app := App{Version: "test", Stdout: &out, Stderr: &out, Cwd: project}
	if err := app.Run([]string{"add", source, "-y", "-a", "codex", "cursor"}); err != nil {
		t.Fatal(err)
	}

	out.Reset()
	if err := app.Run([]string{"list"}); err != nil {
		t.Fatal(err)
	}
	rendered := out.String()
	for _, want := range []string{
		"◆",
		"Installed skills",
		"│",
		"Project",
		"●",
		"demo",
		"Demo skill",
		"agents:",
		"Codex, Cursor",
		"path:",
		shorten(filepath.Join(project, ".agents", "skills", "demo"), project),
		"└",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("decorated list missing %q: %s", want, rendered)
		}
	}
}

func TestListGroupsProjectAndGlobalSkills(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))

	projectSource := makeSkill(t, t.TempDir(), "project-demo", "Local skill")
	globalSource := makeSkill(t, t.TempDir(), "global-demo", "Shared skill")
	var out bytes.Buffer
	app := App{Version: "test", Stdout: &out, Stderr: &out, Cwd: project}
	if err := app.Run([]string{"add", projectSource, "-y", "-a", "codex"}); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := app.Run([]string{"add", globalSource, "-g", "-y", "-a", "codex"}); err != nil {
		t.Fatal(err)
	}

	out.Reset()
	if err := app.Run([]string{"list"}); err != nil {
		t.Fatal(err)
	}
	rendered := out.String()
	projectIdx := strings.Index(rendered, "Project")
	globalIdx := strings.Index(rendered, "Global")
	if projectIdx < 0 || globalIdx < 0 {
		t.Fatalf("expected project and global groups:\n%s", rendered)
	}
	if projectIdx > globalIdx {
		t.Fatalf("project group should render before global group:\n%s", rendered)
	}
	if strings.Count(rendered, "Project") != 1 || strings.Count(rendered, "Global") != 1 {
		t.Fatalf("expected one project and one global group:\n%s", rendered)
	}
	if !strings.Contains(rendered, "project-demo") || !strings.Contains(rendered, "global-demo") {
		t.Fatalf("missing listed skills:\n%s", rendered)
	}
}

func TestListEmptyUsesDecoratedOutput(t *testing.T) {
	var out bytes.Buffer
	app := App{Version: "test", Stdout: &out, Stderr: &out, Cwd: t.TempDir()}
	if err := app.Run([]string{"list"}); err != nil {
		t.Fatal(err)
	}
	rendered := out.String()
	for _, want := range []string{"◆", "Installed skills", "│", "No skills found.", "└"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("decorated empty list missing %q: %s", want, rendered)
		}
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

func TestSkillSelectionModelFiltersAndSelects(t *testing.T) {
	discovered := []skills.Skill{
		{Name: "alpha", Description: "First skill"},
		{Name: "beta", Description: "Second skill"},
	}
	model := newSkillSelectionModel(discovered, "source")
	model.query = "sec"

	filtered := model.filtered()
	if len(filtered) != 1 || filtered[0] != 1 {
		t.Fatalf("filtered = %#v", filtered)
	}

	model.toggleCurrent()
	selected := model.selectedSkills()
	if len(selected) != 1 || selected[0].Name != "beta" {
		t.Fatalf("selected = %#v", selected)
	}
}

func TestSkillSelectionModelSortsSkillsByGroupAndName(t *testing.T) {
	discovered := []skills.Skill{
		{Name: "beta", Metadata: map[string]any{"plugin": "group"}},
		{Name: "alpha", Metadata: map[string]any{"plugin": "group"}},
		{Name: "gamma", Metadata: map[string]any{"plugin": "another"}},
	}
	model := newSkillSelectionModel(discovered, "source")

	if len(model.skills) != 3 {
		t.Fatalf("expected 3 skills, got %d", len(model.skills))
	}
	if model.skills[0].Name != "gamma" || model.skills[1].Name != "alpha" || model.skills[2].Name != "beta" {
		t.Fatalf("skills not sorted by group and name: %#v", model.skills)
	}
}

func TestSkillSelectionModelGroupsByTopLevelPluginField(t *testing.T) {
	discovered := []skills.Skill{
		{Name: "frontend-design", Description: "Review UI", Metadata: map[string]any{"plugin": "agent-skills"}},
		{Name: "code-review", Description: "Review code", Metadata: map[string]any{"plugin": "quality"}},
	}
	model := newSkillSelectionModel(discovered, "source")

	view := model.renderActive()
	if !strings.Contains(view, "Agent Skills") {
		t.Fatalf("expected group heading for agent-skills, got: %s", view)
	}
	if !strings.Contains(view, "Quality") {
		t.Fatalf("expected group heading for quality, got: %s", view)
	}
	if !strings.Contains(view, selectorBar()+"\n"+selectorGroupLine("Quality", model.width)) {
		t.Fatalf("expected blank line before second group heading, got: %s", view)
	}
}

func TestSkillSelectionModelGroupsByNestedRepoFolders(t *testing.T) {
	discovered := []skills.Skill{
		{Name: "react", Description: "Frontend skill", RepoPath: "skills/frontend/react/SKILL.md"},
		{Name: "postgres", Description: "Backend skill", RepoPath: "skills/backend/postgres/SKILL.md"},
		{Name: "sqlite", Description: "Data skill", RepoPath: "skills/backend/data/sqlite/SKILL.md"},
	}
	model := newSkillSelectionModel(discovered, "source")

	view := model.renderActive()
	for _, want := range []string{"Backend", "Backend / Data", "Frontend"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected nested folder heading %q, got: %s", want, view)
		}
	}
}

func TestSkillDiscoveryListUsesSelectorGroupHeaders(t *testing.T) {
	discovered := []skills.Skill{
		{Name: "frontend-design", Description: "Review UI", Metadata: map[string]any{"plugin": "agent-skills"}},
		{Name: "code-review", Description: "Review code", Metadata: map[string]any{"plugin": "quality"}},
		{Name: "react", Description: "Frontend skill", RepoPath: "skills/frontend/react/SKILL.md"},
	}

	view := renderSkillDiscoveryList(discovered, "Discovered skills")
	for _, want := range []string{"Agent Skills", "Quality", "Frontend"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected group heading %q, got: %s", want, view)
		}
	}
	if !strings.Contains(view, selectorBar()+"\n"+selectorGroupLine("Frontend", 88)) {
		t.Fatalf("expected blank line before later group heading, got: %s", view)
	}
	if !strings.Contains(view, "frontend-design") || !strings.Contains(view, "code-review") || !strings.Contains(view, "react") {
		t.Fatalf("expected discovered skills, got: %s", view)
	}
}

func TestSkillSelectionModelFiltersByNestedFolderGroup(t *testing.T) {
	discovered := []skills.Skill{
		{Name: "react", Description: "Frontend skill", RepoPath: "skills/frontend/react/SKILL.md"},
		{Name: "postgres", Description: "Backend skill", RepoPath: "skills/backend/postgres/SKILL.md"},
	}
	model := newSkillSelectionModel(discovered, "source")
	model.query = "frontend"

	filtered := model.filtered()
	if len(filtered) != 1 || model.skills[filtered[0]].Name != "react" {
		t.Fatalf("filtered = %#v, skills = %#v", filtered, model.skills)
	}
}

func TestSkillsWithRepoPathsAddsRelativeSkillMDPath(t *testing.T) {
	base := t.TempDir()
	skillDir := filepath.Join(base, "skills", "frontend", "react")
	list := []skills.Skill{{Name: "react", Path: skillDir}}

	got := skillsWithRepoPaths(list, base)
	if got[0].RepoPath != "skills/frontend/react/SKILL.md" {
		t.Fatalf("RepoPath = %q", got[0].RepoPath)
	}
	if list[0].RepoPath != "" {
		t.Fatalf("skillsWithRepoPaths should not mutate input: %#v", list[0])
	}
}

func TestSkillResolveSpinnerRendersAndClearsLine(t *testing.T) {
	var out bytes.Buffer

	renderSkillResolveSpinner(&out, "owner/repo", 0)
	clearSkillResolveSpinner(&out)

	got := out.String()
	if !strings.Contains(got, "Loading skills from owner/repo") {
		t.Fatalf("spinner output missing loading label: %q", got)
	}
	if strings.Contains(got, "\x1b[?2026") || strings.Contains(got, "\x1b[?2027") || strings.Contains(got, "\x1b[?1u") {
		t.Fatalf("spinner output contains terminal capability query: %q", got)
	}
	if !strings.HasSuffix(got, "\r\x1b[2K") {
		t.Fatalf("spinner output should clear the line, got %q", got)
	}
}

func TestSkillSelectionModelShowsDescriptionOnlyForCursor(t *testing.T) {
	discovered := []skills.Skill{
		{Name: "alpha", Description: "First skill"},
		{Name: "beta", Description: "Second skill"},
	}
	model := newSkillSelectionModel(discovered, "source")

	view := model.renderActive()
	if !strings.Contains(view, "First skill") {
		t.Fatalf("view missing active description: %s", view)
	}
	if strings.Contains(view, "Second skill") {
		t.Fatalf("view includes inactive description: %s", view)
	}

	model.moveCursor(1)
	view = model.renderActive()
	if strings.Contains(view, "First skill") {
		t.Fatalf("view includes inactive description after cursor move: %s", view)
	}
	if !strings.Contains(view, "Second skill") {
		t.Fatalf("view missing active description after cursor move: %s", view)
	}
}

func TestSkillSelectionModelRendersResolvedSourceLabel(t *testing.T) {
	discovered := []skills.Skill{{Name: "alpha", Description: "First skill"}}
	model := newSkillSelectionModel(discovered, "https://github.com/example/repo")

	view := model.renderActive()
	if !strings.Contains(view, "Source:") {
		t.Fatalf("view missing source label: %s", view)
	}
	if !strings.Contains(view, "https://github.com/example/repo") {
		t.Fatalf("view missing resolved source: %s", view)
	}
}

func TestSkillSelectorSourceLabelShowsFullGitHubURL(t *testing.T) {
	parsed, err := source.Parse("vercel-labs/agent-skills")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := skillSelectorSourceLabel(parsed, "vercel-labs/agent-skills", t.TempDir()), "https://github.com/vercel-labs/agent-skills"; got != want {
		t.Fatalf("source label = %q, want %q", got, want)
	}
}

func TestSkillSelectorSourceLabelPreservesGitHubTreeURL(t *testing.T) {
	parsed, err := source.Parse("https://github.com/acme/repo/tree/main/skills/demo")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := skillSelectorSourceLabel(parsed, "https://github.com/acme/repo/tree/main/skills/demo", t.TempDir()), "https://github.com/acme/repo/tree/main/skills/demo"; got != want {
		t.Fatalf("source label = %q, want %q", got, want)
	}
}

func TestSkillSelectionModelWrapsSelectedSummary(t *testing.T) {
	discovered := []skills.Skill{
		{Name: "alpha-extra-long-skill-name", Description: "First skill"},
		{Name: "beta-extra-long-skill-name", Description: "Second skill"},
		{Name: "gamma-extra-long-skill-name", Description: "Third skill"},
	}
	model := newSkillSelectionModel(discovered, "source")
	model.width = 32
	model.selected = map[int]bool{0: true, 1: true, 2: true}

	lines := model.renderSelectedSummaryLines()
	if len(lines) < 2 {
		t.Fatalf("expected wrapped summary, got %#v", lines)
	}
	joined := strings.Join(lines, "\n")
	for _, skill := range discovered {
		if !strings.Contains(joined, skill.Name) {
			t.Fatalf("summary missing %q: %s", skill.Name, joined)
		}
	}
}

func TestSkillSelectionModelRenderSubmittedSummary(t *testing.T) {
	cwd := t.TempDir()
	discovered := []skills.Skill{
		{Name: "alpha", Description: "First skill", Path: filepath.Join(cwd, "source", "alpha")},
	}
	model := newSkillSelectionModel(discovered, "source", selectorInstallContext{
		targets: []agents.Type{agents.Codex, agents.Cursor},
		global:  false,
		cwd:     cwd,
		mode:    installer.Copy,
	})
	model.selected = map[int]bool{0: true}

	view := model.renderSubmitted()
	for _, want := range []string{
		"Installation Summary",
		"Ready to install 1 skill.",
		"scope:",
		"project",
		"mode:",
		"copy",
		"Codex:",
		"Cursor:",
		filepath.Join(cwd, ".agents", "skills", "alpha"),
		filepath.Join(cwd, "source", "alpha"),
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("submitted summary missing %q: %s", want, view)
		}
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
