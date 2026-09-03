package commands

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tdeshazo/goskill/internal/skills"
)

func TestParseUse(t *testing.T) {
	source, opts, err := parseUse([]string{
		"vercel-labs/agent-skills@web-design-guidelines",
		"--agent",
		"codex",
		"--full-depth",
	})
	if err != nil {
		t.Fatal(err)
	}
	if source != "vercel-labs/agent-skills@web-design-guidelines" {
		t.Fatalf("source = %q", source)
	}
	if len(opts.Agent) != 1 || opts.Agent[0] != "codex" || !opts.FullDepth {
		t.Fatalf("options = %#v", opts)
	}
}

func TestParseUseRejectsInvalidOptions(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing skill", args: []string{"source", "--skill"}, want: "requires a skill name"},
		{name: "repeated skill", args: []string{"source", "--skill", "one", "--skill", "two"}, want: "only one --skill"},
		{name: "unknown option", args: []string{"source", "--wat"}, want: "unknown option"},
		{name: "multiple sources", args: []string{"one", "two"}, want: "expected one source"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := parseUse(test.args)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestSplitUseSourceSelector(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantBase  string
		wantSkill string
	}{
		{name: "GitHub shorthand", input: "owner/repo@skill", wantBase: "owner/repo", wantSkill: "skill"},
		{name: "local path", input: "./skills@skill", wantBase: "./skills", wantSkill: "skill"},
		{name: "full URL", input: "https://example.com/skills@skill", wantBase: "https://example.com/skills", wantSkill: "skill"},
		{name: "fragment selector", input: "owner/repo#main@skill", wantBase: "owner/repo#main@skill"},
		{name: "URL user info", input: "https://user@example.com", wantBase: "https://user@example.com"},
		{name: "SSH source", input: "git@github.com:owner/repo@skill", wantBase: "git@github.com:owner/repo", wantSkill: "skill"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			base, selector := splitUseSourceSelector(test.input)
			if base != test.wantBase || selector != test.wantSkill {
				t.Fatalf("splitUseSourceSelector(%q) = (%q, %q), want (%q, %q)", test.input, base, selector, test.wantBase, test.wantSkill)
			}
		})
	}
}

func TestUsePrintsOnlyPromptForSingleSkill(t *testing.T) {
	sourceDir := makeSkill(t, t.TempDir(), "single-skill", "Single skill")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := App{Version: "test", Stdout: &stdout, Stderr: &stderr, Cwd: t.TempDir()}

	if err := app.Run([]string{"use", sourceDir}); err != nil {
		t.Fatal(err)
	}
	prompt := stdout.String()
	if !strings.HasPrefix(prompt, "You are being given a Skill") {
		t.Fatalf("stdout is not a raw prompt:\n%s", prompt)
	}
	for _, want := range []string{"<SKILL.md>", "name: single-skill", "</SKILL.md>"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "Supporting files for this skill") {
		t.Fatalf("prompt unexpectedly contains supporting-file instructions:\n%s", prompt)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestUseMaterializesSupportingFiles(t *testing.T) {
	sourceDir := makeSkill(t, t.TempDir(), "with-files", "Uses a script")
	if err := os.MkdirAll(filepath.Join(sourceDir, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "scripts", "run.sh"), []byte("echo hi\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	app := App{Version: "test", Stdout: &stdout, Stderr: &bytes.Buffer{}, Cwd: t.TempDir()}

	if err := app.Run([]string{"use", sourceDir}); err != nil {
		t.Fatal(err)
	}
	supportDir := supportDirectory(stdout.String())
	if supportDir == "" {
		t.Fatalf("prompt missing support directory:\n%s", stdout.String())
	}
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(supportDir)) })
	content, err := os.ReadFile(filepath.Join(supportDir, "scripts", "run.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "echo hi\n" {
		t.Fatalf("script content = %q", content)
	}
	if !strings.Contains(filepath.Base(filepath.Dir(supportDir)), "skill-use-") {
		t.Fatalf("support directory is not under skill-use-* root: %s", supportDir)
	}
}

func TestMaterializeUseSkillWritesSnapshotFilesSafely(t *testing.T) {
	skill := skills.Skill{
		Name: "snapshot",
		Files: []skills.SnapshotFile{
			{Path: "SKILL.md", Contents: "---\nname: snapshot\ndescription: Snapshot\n---\n"},
			{Path: "references/guide.md", Contents: "# Guide\n"},
			{Path: "../outside", Contents: "unsafe\n"},
		},
	}
	materialized, err := materializeUseSkill(skill)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(materialized.Root) })
	if !materialized.HasSupportingFiles {
		t.Fatal("snapshot support file was not detected")
	}
	if _, err := os.Stat(filepath.Join(materialized.Dir, "references", "guide.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(materialized.Root, "outside")); !os.IsNotExist(err) {
		t.Fatalf("unsafe snapshot path was materialized: %v", err)
	}
}

func TestUseSelectsExactlyOneSkill(t *testing.T) {
	root := t.TempDir()
	makeSkill(t, filepath.Join(root, "skills"), "one", "One")
	makeSkill(t, filepath.Join(root, "skills"), "two", "Two")
	var stdout bytes.Buffer
	app := App{Version: "test", Stdout: &stdout, Stderr: &bytes.Buffer{}, Cwd: t.TempDir()}

	err := app.Run([]string{"use", root})
	if err == nil || !strings.Contains(err.Error(), "contains multiple skills") {
		t.Fatalf("error = %v", err)
	}
	if !strings.Contains(err.Error(), "one") || !strings.Contains(err.Error(), "two") {
		t.Fatalf("error does not list available skills: %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %s", stdout.String())
	}

	if err := app.Run([]string{"use", root, "--skill", "two"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "name: two") || strings.Contains(stdout.String(), "name: one") {
		t.Fatalf("selected prompt = %s", stdout.String())
	}
}

func TestUseSelectsLocalPathSuffix(t *testing.T) {
	root := t.TempDir()
	makeSkill(t, filepath.Join(root, "skills"), "one", "One")
	makeSkill(t, filepath.Join(root, "skills"), "two", "Two")
	var stdout bytes.Buffer
	app := App{Version: "test", Stdout: &stdout, Stderr: &bytes.Buffer{}, Cwd: t.TempDir()}

	if err := app.Run([]string{"use", root + "@two"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "name: two") || strings.Contains(stdout.String(), "name: one") {
		t.Fatalf("selected prompt = %s", stdout.String())
	}
}

func TestUseRemovesUnexposedSnapshot(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("TMPDIR", tempDir)
	sourceDir := makeSkill(t, t.TempDir(), "one-shot", "One shot")
	app := App{Version: "test", Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Cwd: t.TempDir()}

	if err := app.Run([]string{"use", sourceDir}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "skill-use-") {
			t.Fatalf("unexposed snapshot was not removed: %s", entry.Name())
		}
	}
}

func TestUseAgentRemovesSupportingFileSnapshot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test helper is a POSIX shell script")
	}
	binDir := t.TempDir()
	promptPath := filepath.Join(t.TempDir(), "prompt")
	commandPath := filepath.Join(binDir, "codex")
	script := "#!/bin/sh\nprintf '%s' \"$1\" > \"$GOSKILL_TEST_ARGS\"\n"
	if err := os.WriteFile(commandPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GOSKILL_TEST_ARGS", promptPath)

	sourceDir := makeSkill(t, t.TempDir(), "with-files", "Uses a script")
	if err := os.WriteFile(filepath.Join(sourceDir, "reference.md"), []byte("reference\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := App{Version: "test", Stdin: strings.NewReader(""), Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Cwd: t.TempDir()}

	if err := app.Run([]string{"use", sourceDir, "--agent", "codex"}); err != nil {
		t.Fatal(err)
	}
	prompt, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatal(err)
	}
	supportDir := supportDirectory(string(prompt))
	if supportDir == "" {
		t.Fatalf("agent prompt missing support directory:\n%s", prompt)
	}
	if _, err := os.Stat(filepath.Dir(supportDir)); !os.IsNotExist(err) {
		t.Fatalf("agent snapshot was not removed: %v", err)
	}
}

func TestUseDoesNotInstallOrWriteLockfile(t *testing.T) {
	project := t.TempDir()
	sourceDir := makeSkill(t, t.TempDir(), "one-shot", "One shot")
	app := App{Version: "test", Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Cwd: project}
	if err := app.Run([]string{"use", sourceDir}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(project, "skills-lock.json"),
		filepath.Join(project, ".agents", "skills", "one-shot"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("use created persistent path %s: %v", path, err)
		}
	}
}

func TestUseRejectsSelectorConflict(t *testing.T) {
	app := App{Version: "test", Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Cwd: t.TempDir()}
	err := app.Run([]string{"use", "owner/repo@one", "--skill", "two"})
	if err == nil || !strings.Contains(err.Error(), "conflicting skill selectors") {
		t.Fatalf("error = %v", err)
	}
}

func TestUseFullDepth(t *testing.T) {
	root := t.TempDir()
	makeSkill(t, root, "root-skill", "Root")
	makeSkill(t, filepath.Join(root, "nested"), "target", "Target")
	app := App{Version: "test", Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Cwd: t.TempDir()}

	err := app.Run([]string{"use", root, "--skill", "target"})
	if err == nil || !strings.Contains(err.Error(), "no matching skill") {
		t.Fatalf("shallow error = %v", err)
	}
	if err := app.Run([]string{"use", root, "--skill", "target", "--full-depth"}); err != nil {
		t.Fatal(err)
	}
}

func TestUseAgentValidation(t *testing.T) {
	tests := []struct {
		names []string
		want  string
	}{
		{names: []string{"*"}, want: "does not support"},
		{names: []string{"claude-code", "codex"}, want: "exactly one"},
		{names: []string{"unknown"}, want: "invalid agent"},
		{names: []string{"cursor"}, want: "not supported yet"},
	}
	for _, test := range tests {
		err := validateUseAgent(test.names)
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("validateUseAgent(%v) = %v", test.names, err)
		}
	}
}

func TestLaunchUseAgentPassesPromptAndExitCode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test helper is a POSIX shell script")
	}
	binDir := t.TempDir()
	argsPath := filepath.Join(t.TempDir(), "args")
	commandPath := filepath.Join(binDir, "codex")
	script := "#!/bin/sh\nprintf '%s' \"$1\" > \"$GOSKILL_TEST_ARGS\"\nexit 37\n"
	if err := os.WriteFile(commandPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GOSKILL_TEST_ARGS", argsPath)
	app := App{Stdin: strings.NewReader(""), Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}

	err := app.launchUseAgent("codex", "prompt body")
	code, ok := ExitCode(err)
	if !ok || code != 37 {
		t.Fatalf("exit error = %v, code = %d, ok = %v", err, code, ok)
	}
	content, readErr := os.ReadFile(argsPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(content) != "prompt body" {
		t.Fatalf("prompt argument = %q", content)
	}
}

func TestLaunchUseAgentReportsMissingExecutable(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	app := App{Stdin: strings.NewReader(""), Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	err := app.launchUseAgent("claude-code", "prompt")
	if err == nil || !strings.Contains(err.Error(), "command not found: claude") {
		t.Fatalf("error = %v", err)
	}
	if _, ok := ExitCode(err); ok {
		t.Fatalf("missing executable should not be a child exit status: %v", err)
	}
}

func TestUseHelp(t *testing.T) {
	var stdout bytes.Buffer
	app := App{Version: "test", Stdout: &stdout, Stderr: &bytes.Buffer{}, Cwd: t.TempDir()}
	if err := app.Run([]string{"use", "--help"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "Usage: goskill use") {
		t.Fatalf("help = %s", stdout.String())
	}
}

func TestExitCodeIgnoresOrdinaryErrors(t *testing.T) {
	if _, ok := ExitCode(errors.New("ordinary")); ok {
		t.Fatal("ordinary error unexpectedly has an exit code")
	}
}

func supportDirectory(prompt string) string {
	const marker = "Supporting files for this skill were downloaded to:\n"
	remainder := strings.SplitN(prompt, marker, 2)
	if len(remainder) != 2 {
		return ""
	}
	return strings.SplitN(remainder[1], "\n", 2)[0]
}
