package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tdeshazo/goskill/internal/agents"
)

func TestAgentListShowAndValidate(t *testing.T) {
	configDir := t.TempDir()
	configFile := filepath.Join(configDir, "opencode.yaml")
	contents := `version: 1
agents:
  - id: opencode
    display_name: OpenCode
    project_skills_dir: .opencode/skills
    global_skills_dir: ~/.opencode/skills
    command: opencode
`
	if err := os.WriteFile(configFile, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	registry, err := agents.Load(agents.LoadOptions{Home: t.TempDir(), ConfigDir: configDir})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	app := App{Agents: registry, Stdout: &out, Cwd: t.TempDir()}
	if err := app.Run([]string{"agent", "list"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "opencode") || !strings.Contains(out.String(), "(user)") {
		t.Fatalf("list output = %q", out.String())
	}
	out.Reset()
	if err := app.Run([]string{"agent", "show", "opencode"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Command: opencode") || !strings.Contains(out.String(), "Origin: user") {
		t.Fatalf("show output = %q", out.String())
	}
	out.Reset()
	if err := app.Run([]string{"agent", "validate", configFile}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Agent configuration valid") {
		t.Fatalf("validate output = %q", out.String())
	}
}

func TestCustomAgentWorksWithAddResolution(t *testing.T) {
	registry := registryWithOpenCode(t)
	project := t.TempDir()
	app := App{Agents: registry, Cwd: project}
	targets, err := app.resolveAgents(registry, []string{"opencode"})
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0] != "opencode" {
		t.Fatalf("targets = %v", targets)
	}
	if err := validateUseAgentWithRegistry(registry, []string{"opencode"}); err != nil {
		t.Fatalf("custom use agent rejected: %v", err)
	}
	source := makeSkill(t, t.TempDir(), "demo", "Demo skill")
	if err := app.Add([]string{source}, AddOptions{Agent: []string{"opencode"}, Yes: true}); err != nil {
		t.Fatalf("custom add agent: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".opencode", "skills", "demo", "SKILL.md")); err != nil {
		t.Fatalf("custom agent skill was not installed: %v", err)
	}
}

func registryWithOpenCode(t *testing.T) *agents.Registry {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "opencode.yaml"), []byte(`version: 1
agents:
  - id: opencode
    display_name: OpenCode
    project_skills_dir: .opencode/skills
    global_skills_dir: ~/.opencode/skills
    command: opencode
`), 0o644); err != nil {
		t.Fatal(err)
	}
	registry, err := agents.Load(agents.LoadOptions{Home: t.TempDir(), ConfigDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	return registry
}
