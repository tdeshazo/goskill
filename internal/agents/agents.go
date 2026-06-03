package agents

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type Type string

const (
	ClaudeCode Type = "claude-code"
	Codex      Type = "codex"
	Cursor     Type = "cursor"
)

type Config struct {
	Name            Type
	DisplayName     string
	SkillsDir       string
	GlobalSkillsDir string
}

func homeDir() string {
	home, _ := os.UserHomeDir()
	return home
}

func claudeHome() string {
	if v := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")); v != "" {
		return v
	}
	return filepath.Join(homeDir(), ".claude")
}

func codexHome() string {
	if v := strings.TrimSpace(os.Getenv("CODEX_HOME")); v != "" {
		return v
	}
	return filepath.Join(homeDir(), ".codex")
}

func All() map[Type]Config {
	home := homeDir()
	return map[Type]Config{
		ClaudeCode: {
			Name:            ClaudeCode,
			DisplayName:     "Claude Code",
			SkillsDir:       filepath.Join(".claude", "skills"),
			GlobalSkillsDir: filepath.Join(claudeHome(), "skills"),
		},
		Codex: {
			Name:            Codex,
			DisplayName:     "Codex",
			SkillsDir:       filepath.Join(".agents", "skills"),
			GlobalSkillsDir: filepath.Join(codexHome(), "skills"),
		},
		Cursor: {
			Name:            Cursor,
			DisplayName:     "Cursor",
			SkillsDir:       filepath.Join(".agents", "skills"),
			GlobalSkillsDir: filepath.Join(home, ".cursor", "skills"),
		},
	}
}

func Ordered() []Type {
	return []Type{ClaudeCode, Codex, Cursor}
}

func Get(t Type) (Config, bool) {
	cfg, ok := All()[t]
	return cfg, ok
}

func IsValid(name string) bool {
	_, ok := Get(Type(name))
	return ok
}

func Validate(names []string) ([]Type, []string) {
	var out []Type
	var invalid []string
	seen := map[Type]bool{}
	for _, n := range names {
		if n == "*" {
			return Ordered(), nil
		}
		t := Type(n)
		if _, ok := Get(t); !ok {
			invalid = append(invalid, n)
			continue
		}
		if !seen[t] {
			out = append(out, t)
			seen[t] = true
		}
	}
	return out, invalid
}

func DetectInstalled(cwd string) []Type {
	home := homeDir()
	var out []Type
	for _, t := range Ordered() {
		switch t {
		case ClaudeCode:
			if exists(claudeHome()) || exists(filepath.Join(cwd, ".claude")) {
				out = append(out, t)
			}
		case Codex:
			if exists(codexHome()) || exists(filepath.Join(cwd, ".agents")) || exists("/etc/codex") {
				out = append(out, t)
			}
		case Cursor:
			if exists(filepath.Join(home, ".cursor")) || exists(filepath.Join(cwd, ".agents")) {
				out = append(out, t)
			}
		}
	}
	return out
}

func DefaultTargets(cwd string) []Type {
	if detected := DetectInstalled(cwd); len(detected) > 0 {
		return detected
	}
	return []Type{Codex, Cursor}
}

func IsUniversalProject(t Type) bool {
	cfg, ok := Get(t)
	return ok && filepath.Clean(cfg.SkillsDir) == filepath.Join(".agents", "skills")
}

func CanonicalSkillsDir(global bool, cwd string) string {
	if global {
		return filepath.Join(homeDir(), ".agents", "skills")
	}
	return filepath.Join(cwd, ".agents", "skills")
}

func BaseDir(t Type, global bool, cwd string) string {
	cfg, _ := Get(t)
	if global {
		return cfg.GlobalSkillsDir
	}
	if IsUniversalProject(t) {
		return CanonicalSkillsDir(false, cwd)
	}
	return filepath.Join(cwd, cfg.SkillsDir)
}

func Display(t Type) string {
	cfg, ok := Get(t)
	if !ok {
		return string(t)
	}
	return cfg.DisplayName
}

func PathForDisplay(path string) string {
	if runtime.GOOS == "windows" {
		return filepath.ToSlash(path)
	}
	return path
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
