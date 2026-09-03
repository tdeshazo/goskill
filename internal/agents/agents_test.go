package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadBuiltinsPreservesDefaultPaths(t *testing.T) {
	home := t.TempDir()
	project := filepath.Join(t.TempDir(), "project")
	registry, err := Load(LoadOptions{Home: home, ConfigDir: filepath.Join(t.TempDir(), "missing")})
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := registry.Get(Codex)
	if !ok || entry.Origin != Builtin || entry.Config.DisplayName != "Codex" {
		t.Fatalf("Codex entry = %#v, %v", entry, ok)
	}
	projectDir, err := registry.BaseDir(Codex, false, project)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(project, ".agents", "skills"); projectDir != want {
		t.Fatalf("project path = %q, want %q", projectDir, want)
	}
	globalDir, err := registry.BaseDir(ClaudeCode, true, project)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".claude", "skills"); globalDir != want {
		t.Fatalf("global path = %q, want %q", globalDir, want)
	}
}

func TestBuiltinDetectPathsArePlatformAware(t *testing.T) {
	configs, err := parseConfigsWithTransform(builtins, "embedded built-ins", func(config Config) Config {
		return builtinConfigForPlatform(config, "linux")
	})
	if err != nil {
		t.Fatal(err)
	}
	var codex Config
	for _, config := range configs {
		if config.Name == Codex {
			codex = config
			break
		}
	}
	if !containsPath(codex.DetectPaths, "/etc/codex") {
		t.Fatalf("Unix built-in probes = %v, want /etc/codex", codex.DetectPaths)
	}

	windowsConfigs, err := parseConfigsWithTransform(builtins, "embedded built-ins", func(config Config) Config {
		return builtinConfigForPlatform(config, "windows")
	})
	if err != nil {
		t.Fatalf("Windows built-ins must validate: %v", err)
	}
	for _, config := range windowsConfigs {
		if config.Name == Codex && containsPath(config.DetectPaths, "/etc/codex") {
			t.Fatalf("Windows built-in probes = %v, must not include POSIX /etc/codex", config.DetectPaths)
		}
	}
}

func TestLoadUserOverrideAndCustomAgent(t *testing.T) {
	configDir := t.TempDir()
	writeAgentConfig(t, filepath.Join(configDir, "custom.yaml"), `version: 1
agents:
  - id: codex
    display_name: Local Codex
    project_skills_dir: .local-codex/skills
    global_skills_dir: ~/.local-codex/skills
  - id: opencode
    display_name: OpenCode
    project_skills_dir: .opencode/skills
    global_skills_dir: ~/.opencode/skills
    command: opencode
`)
	registry, err := Load(LoadOptions{Home: t.TempDir(), ConfigDir: configDir})
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := registry.Get(Codex)
	if !ok || entry.Origin != User || entry.Config.DisplayName != "Local Codex" {
		t.Fatalf("override = %#v, %v", entry, ok)
	}
	if origin, ok := registry.Origin("opencode"); !ok || origin != User {
		t.Fatalf("opencode origin = %q, %v", origin, ok)
	}
	valid, invalid := registry.Validate([]string{"opencode"})
	if len(invalid) != 0 || len(valid) != 1 || valid[0] != "opencode" {
		t.Fatalf("Validate custom = %v, %v", valid, invalid)
	}
}

func TestLoadRejectsInvalidAndDuplicateUserDefinitions(t *testing.T) {
	configDir := t.TempDir()
	writeAgentConfig(t, filepath.Join(configDir, "one.yaml"), `version: 1
agents:
  - id: opencode
    display_name: OpenCode
    project_skills_dir: ../escape
    global_skills_dir: ~/.opencode/skills
`)
	_, err := Load(LoadOptions{ConfigDir: configDir})
	if err == nil || !strings.Contains(err.Error(), "project_skills_dir") {
		t.Fatalf("invalid path error = %v", err)
	}

	writeAgentConfig(t, filepath.Join(configDir, "one.yaml"), `version: 1
agents:
  - id: opencode
    display_name: OpenCode
    project_skills_dir: .opencode/skills
    global_skills_dir: ~/.opencode/skills
`)
	writeAgentConfig(t, filepath.Join(configDir, "two.yml"), `version: 1
agents:
  - id: opencode
    display_name: Duplicate
    project_skills_dir: .duplicate/skills
    global_skills_dir: ~/.duplicate/skills
`)
	_, err = Load(LoadOptions{ConfigDir: configDir})
	if err == nil || !strings.Contains(err.Error(), "duplicate user agent id") {
		t.Fatalf("duplicate error = %v", err)
	}
}

func TestLoadFileRejectsMalformedAndUnsupportedDocuments(t *testing.T) {
	file := filepath.Join(t.TempDir(), "broken.yaml")
	writeAgentConfig(t, file, "agents: [\n")
	if _, err := LoadFile(file); err == nil || !strings.Contains(err.Error(), "decode agent config") {
		t.Fatalf("malformed YAML error = %v", err)
	}
	writeAgentConfig(t, file, "version: 2\nagents: []\n")
	if _, err := LoadFile(file); err == nil || !strings.Contains(err.Error(), "version: 1") {
		t.Fatalf("version error = %v", err)
	}
	writeAgentConfig(t, file, "version: 1\nagents:\n  - id: opencode\n    display_name: OpenCode\n    project_skills_dir: .opencode\n    global_skills_dir: ~/.opencode\n    unsupported: true\n")
	if _, err := LoadFile(file); err == nil || !strings.Contains(err.Error(), "field unsupported") {
		t.Fatalf("unknown field error = %v", err)
	}
	writeAgentConfig(t, file, "version: 1\nagents:\n  - id: opencode\n    display_name: OpenCode\n    project_skills_dir: .opencode\n    global_skills_dir: ~/.opencode\n---\nunsupported: true\n")
	if _, err := LoadFile(file); err == nil || !strings.Contains(err.Error(), "exactly one YAML document") {
		t.Fatalf("multiple document error = %v", err)
	}
}

func TestBaseDirExpandsConfiguredGlobalPathWithoutShell(t *testing.T) {
	home := t.TempDir()
	registry, err := Load(LoadOptions{
		Home:      home,
		ConfigDir: filepath.Join(t.TempDir(), "missing"),
		Env: func(name string) string {
			if name == "CODEX_HOME" {
				return "~/configured-codex"
			}
			return ""
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := registry.BaseDir(Codex, true, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, "configured-codex", "skills"); got != want {
		t.Fatalf("expanded path = %q, want %q", got, want)
	}
}

func TestDetectInstalledCustomAgent(t *testing.T) {
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, ".opencode"), 0o755); err != nil {
		t.Fatal(err)
	}
	configDir := t.TempDir()
	writeAgentConfig(t, filepath.Join(configDir, "opencode.yaml"), `version: 1
agents:
  - id: opencode
    display_name: OpenCode
    project_skills_dir: .opencode/skills
    global_skills_dir: ~/.opencode/skills
    detect_paths: [~/.opencode]
`)
	registry, err := Load(LoadOptions{Home: home, ConfigDir: configDir})
	if err != nil {
		t.Fatal(err)
	}
	installed := registry.DetectInstalled(t.TempDir())
	if len(installed) != 1 || installed[0] != "opencode" {
		t.Fatalf("detected = %v", installed)
	}
}

func TestDetectInstalledRespectsBuiltinConfigEnvironment(t *testing.T) {
	configured := t.TempDir()
	registry, err := Load(LoadOptions{
		Home:      t.TempDir(),
		ConfigDir: filepath.Join(t.TempDir(), "missing"),
		Env: func(name string) string {
			if name == "CLAUDE_CONFIG_DIR" {
				return configured
			}
			return ""
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	installed := registry.DetectInstalled(t.TempDir())
	if len(installed) != 1 || installed[0] != ClaudeCode {
		t.Fatalf("detected = %v", installed)
	}
}

func writeAgentConfig(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func containsPath(paths []string, want string) bool {
	for _, path := range paths {
		if path == want {
			return true
		}
	}
	return false
}
