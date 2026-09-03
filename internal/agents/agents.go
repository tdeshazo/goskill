// Package agents loads declarative agent installation targets.
package agents

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type Type string

const (
	ClaudeCode Type = "claude-code"
	Codex      Type = "codex"
	Cursor     Type = "cursor"
)

type Origin string

const (
	Builtin Origin = "builtin"
	User    Origin = "user"
)

// Config describes an agent's skill locations and optional launcher. YAML
// paths use '/' and are expanded by Registry without invoking a shell.
type Config struct {
	Name               Type     `yaml:"id"`
	DisplayName        string   `yaml:"display_name"`
	SkillsDir          string   `yaml:"project_skills_dir"`
	GlobalSkillsDir    string   `yaml:"global_skills_dir"`
	GlobalConfigEnv    string   `yaml:"global_config_env"`
	GlobalConfigSuffix string   `yaml:"global_config_suffix"`
	DetectPaths        []string `yaml:"detect_paths"`
	UniversalProject   bool     `yaml:"universal_project"`
	ProjectGuardDir    string   `yaml:"project_guard_dir"`
	Command            string   `yaml:"command"`
}

type Entry struct {
	Config Config
	Origin Origin
}

type LoadOptions struct {
	Home      string
	ConfigDir string
	Env       func(string) string
}

// Registry holds agent definitions for one command invocation.
type Registry struct {
	home    string
	env     func(string) string
	entries map[Type]Entry
}

//go:embed builtins.yaml
var builtins []byte

// Load creates a registry from embedded built-ins and user YAML files. User
// entries replace built-ins by id; duplicate user ids are actionable errors.
func Load(options LoadOptions) (*Registry, error) {
	home := strings.TrimSpace(options.Home)
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	env := options.Env
	if env == nil {
		env = os.Getenv
	}
	registry := &Registry{home: home, env: env, entries: map[Type]Entry{}}
	builtinConfigs, err := parseConfigsWithTransform(builtins, "embedded built-ins", func(config Config) Config {
		return builtinConfigForPlatform(config, runtime.GOOS)
	})
	if err != nil {
		return nil, err
	}
	for _, config := range builtinConfigs {
		if err := registry.Register(config, Builtin); err != nil {
			return nil, err
		}
	}

	configDir := options.ConfigDir
	if configDir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return registry, nil
		}
		configDir = filepath.Join(base, "goskill", "agents")
	}
	files, err := userConfigFiles(configDir)
	if err != nil {
		return nil, err
	}
	seen := map[Type]string{}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("read agent config %q: %w", file, err)
		}
		configs, err := parseConfigs(data, file)
		if err != nil {
			return nil, err
		}
		for _, config := range configs {
			if previous, ok := seen[config.Name]; ok {
				return nil, fmt.Errorf("duplicate user agent id %q in %s and %s", config.Name, previous, file)
			}
			seen[config.Name] = file
			if err := registry.Register(config, User); err != nil {
				return nil, fmt.Errorf("register agent config %q: %w", file, err)
			}
		}
	}
	return registry, nil
}

// LoadFile validates one standalone YAML config file without reading user
// configuration directories. It powers `goskill agent validate`.
func LoadFile(file string) ([]Config, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("read agent config %q: %w", file, err)
	}
	return parseConfigs(data, file)
}

func userConfigFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read agent config directory %q: %w", dir, err)
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		extension := strings.ToLower(filepath.Ext(entry.Name()))
		if extension == ".yaml" || extension == ".yml" {
			files = append(files, filepath.Join(dir, entry.Name()))
		}
	}
	sort.Strings(files)
	return files, nil
}

func parseConfigs(data []byte, source string) ([]Config, error) {
	return parseConfigsWithTransform(data, source, nil)
}

func parseConfigsWithTransform(data []byte, source string, transform func(Config) Config) ([]Config, error) {
	var document struct {
		Version int      `yaml:"version"`
		Agents  []Config `yaml:"agents"`
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("decode agent config %q: %w", source, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("agent config %q must contain exactly one YAML document", source)
		}
		return nil, fmt.Errorf("decode agent config %q: %w", source, err)
	}
	if document.Version != 1 {
		return nil, fmt.Errorf("agent config %q must declare version: 1", source)
	}
	if len(document.Agents) == 0 {
		return nil, fmt.Errorf("agent config %q must declare at least one agent", source)
	}
	seen := map[Type]bool{}
	configs := make([]Config, 0, len(document.Agents))
	for _, config := range document.Agents {
		if transform != nil {
			config = transform(config)
		}
		if err := validateConfig(config); err != nil {
			return nil, fmt.Errorf("invalid agent config %q: %w", source, err)
		}
		if seen[config.Name] {
			return nil, fmt.Errorf("invalid agent config %q: duplicate agent id %q", source, config.Name)
		}
		seen[config.Name] = true
		config.SkillsDir = filepath.FromSlash(config.SkillsDir)
		config.GlobalSkillsDir = filepath.FromSlash(config.GlobalSkillsDir)
		config.GlobalConfigSuffix = filepath.FromSlash(config.GlobalConfigSuffix)
		config.ProjectGuardDir = filepath.FromSlash(config.ProjectGuardDir)
		configs = append(configs, config)
	}
	return configs, nil
}

// builtinConfigForPlatform removes POSIX-only built-in probes on Windows.
// User-provided paths are still validated exactly as entered on their host.
func builtinConfigForPlatform(config Config, goos string) Config {
	if goos != "windows" {
		return config
	}
	detectPaths := make([]string, 0, len(config.DetectPaths))
	for _, detectPath := range config.DetectPaths {
		// A leading slash is a POSIX-rooted path. filepath.IsAbs cannot
		// recognize it after Windows normalization, so exclude it before
		// validation rather than making all embedded definitions unusable.
		if strings.HasPrefix(strings.TrimSpace(detectPath), "/") {
			continue
		}
		detectPaths = append(detectPaths, detectPath)
	}
	config.DetectPaths = detectPaths
	return config
}

func validateConfig(config Config) error {
	id := string(config.Name)
	if !validID(id) {
		return errors.New("id must use lowercase letters, digits, and hyphens")
	}
	if strings.TrimSpace(config.DisplayName) == "" {
		return fmt.Errorf("agent %q must declare display_name", id)
	}
	if !safeProjectPath(config.SkillsDir) {
		return fmt.Errorf("agent %q project_skills_dir must be a relative path below the project", id)
	}
	if !safeGlobalPath(config.GlobalSkillsDir) {
		return fmt.Errorf("agent %q global_skills_dir must be an absolute or ~/ path", id)
	}
	if config.GlobalConfigEnv != "" && !validEnvName(config.GlobalConfigEnv) {
		return fmt.Errorf("agent %q global_config_env is not a valid environment variable name", id)
	}
	if config.GlobalConfigSuffix != "" && !safeProjectPath(config.GlobalConfigSuffix) {
		return fmt.Errorf("agent %q global_config_suffix must be a relative path", id)
	}
	if config.ProjectGuardDir != "" && !safeProjectPath(config.ProjectGuardDir) {
		return fmt.Errorf("agent %q project_guard_dir must be a relative path below the project", id)
	}
	for _, detectPath := range config.DetectPaths {
		if !safeDetectPath(detectPath) {
			return fmt.Errorf("agent %q has unsafe detect path %q", id, detectPath)
		}
	}
	return nil
}

func validID(value string) bool {
	if value == "" || value == "*" {
		return false
	}
	for index, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || (r == '-' && index > 0) {
			continue
		}
		return false
	}
	return true
}

func validEnvName(value string) bool {
	for index, r := range value {
		if (r >= 'A' && r <= 'Z') || r == '_' || (index > 0 && r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return value != ""
}

func safeProjectPath(value string) bool {
	value = filepath.FromSlash(strings.TrimSpace(value))
	if value == "" || filepath.IsAbs(value) || strings.HasPrefix(value, "~") {
		return false
	}
	clean := filepath.Clean(value)
	return clean != "." && clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator))
}

func safeGlobalPath(value string) bool {
	value = filepath.FromSlash(strings.TrimSpace(value))
	if value == "~" || strings.HasPrefix(value, "~"+string(filepath.Separator)) {
		return true
	}
	return filepath.IsAbs(value)
}

func safeDetectPath(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if strings.HasPrefix(value, "{project}/") || value == "{project}" {
		return value == "{project}" || safeProjectPath(strings.TrimPrefix(value, "{project}/"))
	}
	return safeGlobalPath(value)
}

func (r *Registry) List() []Entry {
	entries := make([]Entry, 0, len(r.entries))
	for _, entry := range r.entries {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Config.Name < entries[j].Config.Name })
	return entries
}

func (r *Registry) Get(t Type) (Entry, bool) { entry, ok := r.entries[t]; return entry, ok }

// Origin reports where an agent definition came from.
func (r *Registry) Origin(t Type) (Origin, bool) {
	entry, ok := r.Get(t)
	return entry.Origin, ok
}

// Register adds an agent definition to this registry. A user definition may
// replace an embedded definition with the same id; duplicate definitions from
// the same origin are rejected so configuration errors remain unambiguous.
func (r *Registry) Register(config Config, origin Origin) error {
	if origin != Builtin && origin != User {
		return fmt.Errorf("invalid agent origin %q", origin)
	}
	if err := validateConfig(config); err != nil {
		return err
	}
	if previous, exists := r.entries[config.Name]; exists && previous.Origin == origin {
		return fmt.Errorf("duplicate %s agent id %q", origin, config.Name)
	}
	if previous, exists := r.entries[config.Name]; exists && previous.Origin == User && origin == Builtin {
		return nil
	}
	config.SkillsDir = filepath.FromSlash(config.SkillsDir)
	config.GlobalSkillsDir = filepath.FromSlash(config.GlobalSkillsDir)
	config.GlobalConfigSuffix = filepath.FromSlash(config.GlobalConfigSuffix)
	config.ProjectGuardDir = filepath.FromSlash(config.ProjectGuardDir)
	r.entries[config.Name] = Entry{Config: config, Origin: origin}
	return nil
}

func (r *Registry) IsValid(name string) bool { _, ok := r.Get(Type(name)); return ok }

func (r *Registry) Validate(names []string) ([]Type, []string) {
	out := make([]Type, 0, len(names))
	invalid := make([]string, 0)
	seen := map[Type]bool{}
	for _, name := range names {
		if name == "*" {
			return r.Ordered(), []string{}
		}
		typeName := Type(name)
		if _, ok := r.Get(typeName); !ok {
			invalid = append(invalid, name)
			continue
		}
		if !seen[typeName] {
			out = append(out, typeName)
			seen[typeName] = true
		}
	}
	return out, invalid
}

func (r *Registry) Ordered() []Type {
	entries := r.List()
	types := make([]Type, 0, len(entries))
	for _, entry := range entries {
		types = append(types, entry.Config.Name)
	}
	return types
}

func (r *Registry) DetectInstalled(cwd string) []Type {
	types := make([]Type, 0)
	for _, entry := range r.List() {
		if entry.Config.GlobalConfigEnv != "" {
			if configured := strings.TrimSpace(r.env(entry.Config.GlobalConfigEnv)); configured != "" {
				if path, err := r.expandHome(configured); err == nil && exists(path) {
					types = append(types, entry.Config.Name)
					continue
				}
			}
		}
		for _, rawPath := range entry.Config.DetectPaths {
			path, err := r.expandDetectPath(rawPath, cwd)
			if err == nil && exists(path) {
				types = append(types, entry.Config.Name)
				break
			}
		}
	}
	return types
}

func (r *Registry) DefaultTargets(cwd string) []Type {
	if detected := r.DetectInstalled(cwd); len(detected) > 0 {
		return detected
	}
	defaults := []Type{Codex, Cursor}
	valid := make([]Type, 0, len(defaults))
	for _, target := range defaults {
		if _, ok := r.Get(target); ok {
			valid = append(valid, target)
		}
	}
	return valid
}

func (r *Registry) CanonicalSkillsDir(global bool, cwd string) string {
	if global {
		return filepath.Join(r.home, ".agents", "skills")
	}
	return filepath.Join(cwd, ".agents", "skills")
}

func (r *Registry) BaseDir(t Type, global bool, cwd string) (string, error) {
	entry, ok := r.Get(t)
	if !ok {
		return "", fmt.Errorf("unknown agent %q", t)
	}
	if !global {
		if entry.Config.UniversalProject {
			return r.CanonicalSkillsDir(false, cwd), nil
		}
		return filepath.Join(cwd, entry.Config.SkillsDir), nil
	}
	if entry.Config.GlobalConfigEnv != "" {
		if configured := strings.TrimSpace(r.env(entry.Config.GlobalConfigEnv)); configured != "" {
			base, err := r.expandHome(configured)
			if err != nil {
				return "", err
			}
			return filepath.Join(base, entry.Config.GlobalConfigSuffix), nil
		}
	}
	return r.expandHome(entry.Config.GlobalSkillsDir)
}

func (r *Registry) IsUniversalProject(t Type) bool {
	entry, ok := r.Get(t)
	return ok && entry.Config.UniversalProject
}

func (r *Registry) Display(t Type) string {
	entry, ok := r.Get(t)
	if !ok {
		return string(t)
	}
	return entry.Config.DisplayName
}

func (r *Registry) Command(t Type) string {
	entry, ok := r.Get(t)
	if !ok {
		return ""
	}
	return entry.Config.Command
}

// HasProjectGuard reports whether this target only applies when its configured
// project directory exists. It preserves opt-in project behavior declaratively.
func (r *Registry) HasProjectGuard(t Type, cwd string) bool {
	entry, ok := r.Get(t)
	if !ok || entry.Config.ProjectGuardDir == "" {
		return false
	}
	return exists(filepath.Join(cwd, entry.Config.ProjectGuardDir))
}

func (r *Registry) expandDetectPath(value, cwd string) (string, error) {
	if value == "{project}" {
		return cwd, nil
	}
	if strings.HasPrefix(value, "{project}/") {
		return filepath.Join(cwd, filepath.FromSlash(strings.TrimPrefix(value, "{project}/"))), nil
	}
	return r.expandHome(value)
}

func (r *Registry) expandHome(value string) (string, error) {
	value = filepath.FromSlash(strings.TrimSpace(value))
	if value == "~" {
		return r.home, nil
	}
	if strings.HasPrefix(value, "~"+string(filepath.Separator)) {
		return filepath.Join(r.home, strings.TrimPrefix(value, "~"+string(filepath.Separator))), nil
	}
	if !filepath.IsAbs(value) {
		return "", fmt.Errorf("path %q must be absolute or start with ~/", value)
	}
	return filepath.Clean(value), nil
}

func PathForDisplay(path string) string {
	if runtime.GOOS == "windows" {
		return filepath.ToSlash(path)
	}
	return path
}

func exists(path string) bool { _, err := os.Stat(path); return err == nil }
