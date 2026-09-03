package skills

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/tdeshazo/goskill/internal/terminal"
)

type Skill struct {
	Name        string
	Description string
	Path        string
	RawContent  string
	Metadata    map[string]any
	Files       []SnapshotFile
	RepoPath    string
	Hash        string
}

type SnapshotFile struct {
	Path     string `json:"path"`
	Contents string `json:"contents"`
}

var skipDirs = map[string]bool{
	"node_modules": true,
	".git":         true,
	"dist":         true,
	"build":        true,
	"__pycache__":  true,
}

func ParseSkillMD(path string, includeInternal bool) (Skill, bool) {
	rawBytes, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, false
	}
	raw := string(rawBytes)
	data := ParseFrontmatter(raw)
	name, _ := data["name"].(string)
	desc, _ := data["description"].(string)
	if name == "" || desc == "" {
		return Skill{}, false
	}
	if metadata, ok := data["metadata"].(map[string]any); ok {
		if internal, _ := metadata["internal"].(bool); internal && !includeInternal && !ShouldInstallInternal() {
			return Skill{}, false
		}
	}
	return Skill{
		Name:        terminal.Metadata(name),
		Description: terminal.Metadata(desc),
		Path:        filepath.Dir(path),
		RawContent:  raw,
		Metadata:    metadataFrom(data, "plugin", "pluginName", "source"),
	}, true
}

func ParseFrontmatter(raw string) map[string]any {
	out := map[string]any{}
	if !strings.HasPrefix(raw, "---\n") && !strings.HasPrefix(raw, "---\r\n") {
		return out
	}
	lines := splitLines(raw)
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return out
	}
	inMetadata := false
	metadata := map[string]any{}
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "---" {
			break
		}
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			if inMetadata {
				k, v, ok := parseYAMLScalar(strings.TrimSpace(line))
				if ok {
					metadata[k] = v
				}
			}
			continue
		}
		k, v, ok := parseYAMLScalar(strings.TrimSpace(line))
		if !ok {
			inMetadata = false
			continue
		}
		if k == "metadata" {
			inMetadata = true
			if asMap, ok := v.(map[string]any); ok {
				for mk, mv := range asMap {
					metadata[mk] = mv
				}
			}
			out["metadata"] = metadata
			continue
		}
		inMetadata = false
		out[k] = v
	}
	if len(metadata) > 0 {
		out["metadata"] = metadata
	}
	return out
}

func Discover(basePath, subpath string, includeInternal, fullDepth bool) ([]Skill, error) {
	if subpath != "" && !SubpathSafe(basePath, subpath) {
		return nil, errors.New("invalid subpath resolves outside repository")
	}
	searchPath := basePath
	if subpath != "" {
		searchPath = filepath.Join(basePath, subpath)
	}
	var out []Skill
	seen := map[string]bool{}
	if hasSkillMD(searchPath) {
		if skill, ok := ParseSkillMD(filepath.Join(searchPath, "SKILL.md"), includeInternal); ok {
			out = append(out, skill)
			seen[strings.ToLower(skill.Name)] = true
			if !fullDepth {
				return out, nil
			}
		}
	}

	for _, dir := range priorityDirs(searchPath) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			skillDir := filepath.Join(dir, entry.Name())
			if !hasSkillMD(skillDir) {
				continue
			}
			if skill, ok := ParseSkillMD(filepath.Join(skillDir, "SKILL.md"), includeInternal); ok {
				key := strings.ToLower(skill.Name)
				if !seen[key] {
					out = append(out, skill)
					seen[key] = true
				}
			}
		}
	}
	if len(out) == 0 || fullDepth {
		dirs := findSkillDirs(searchPath, 5)
		for _, dir := range dirs {
			if skill, ok := ParseSkillMD(filepath.Join(dir, "SKILL.md"), includeInternal); ok {
				key := strings.ToLower(skill.Name)
				if !seen[key] {
					out = append(out, skill)
					seen[key] = true
				}
			}
		}
	}
	return out, nil
}

func FindSkillDirs(root string, maxDepth int) []string {
	return findSkillDirs(root, maxDepth)
}

func FindValidationSkillFiles(root string, maxDepth int) []string {
	var out []string
	var walk func(string, int)
	walk = func(dir string, depth int) {
		if depth > maxDepth {
			return
		}
		if path, ok := validationSkillFile(dir); ok {
			out = append(out, path)
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, entry := range entries {
			if entry.IsDir() && !skipDirs[entry.Name()] {
				walk(filepath.Join(dir, entry.Name()), depth+1)
			}
		}
	}
	walk(root, 0)
	sort.Strings(out)
	return out
}

func ValidationSkillFile(dir string) (string, bool) {
	return validationSkillFile(dir)
}

// ResolveValidationSkillFile returns path using the actual casing of its
// directory entry. This matters when a lowercase skill.md is addressed as
// SKILL.md on a case-insensitive filesystem.
func ResolveValidationSkillFile(path string) (string, bool) {
	base := filepath.Base(path)
	if base != "SKILL.md" && base != "skill.md" {
		return "", false
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		return path, true
	}
	for _, entry := range entries {
		if entry.Name() == base {
			return filepath.Join(filepath.Dir(path), entry.Name()), true
		}
	}
	for _, entry := range entries {
		name := entry.Name()
		if name != "SKILL.md" && name != "skill.md" {
			continue
		}
		candidate := filepath.Join(filepath.Dir(path), name)
		candidateInfo, err := os.Stat(candidate)
		if err == nil && candidateInfo.Mode().IsRegular() && os.SameFile(info, candidateInfo) {
			return candidate, true
		}
	}
	return path, true
}

func Filter(list []Skill, names []string) []Skill {
	if len(names) == 0 {
		return list
	}
	for _, n := range names {
		if n == "*" {
			return list
		}
	}
	want := map[string]bool{}
	for _, n := range names {
		want[strings.ToLower(n)] = true
	}
	var out []Skill
	for _, s := range list {
		if want[strings.ToLower(s.Name)] || want[strings.ToLower(filepath.Base(s.Path))] {
			out = append(out, s)
		}
	}
	return out
}

func validationSkillFile(dir string) (string, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return validationSkillFileByStat(dir)
	}
	paths := map[string]string{}
	for _, entry := range entries {
		name := entry.Name()
		if name == "SKILL.md" || name == "skill.md" {
			paths[name] = filepath.Join(dir, name)
		}
	}
	for _, name := range []string{"SKILL.md", "skill.md"} {
		path, ok := paths[name]
		if !ok {
			continue
		}
		info, err := os.Stat(path)
		if err == nil && info.Mode().IsRegular() {
			return path, true
		}
	}
	return "", false
}

func validationSkillFileByStat(dir string) (string, bool) {
	for _, name := range []string{"SKILL.md", "skill.md"} {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err == nil && info.Mode().IsRegular() {
			return path, true
		}
	}
	return "", false
}

func SanitizeName(name string) string {
	s := strings.ToLower(name)
	s = regexp.MustCompile(`[^a-z0-9._]+`).ReplaceAllString(s, "-")
	s = strings.Trim(s, ".-")
	if len(s) > 255 {
		s = s[:255]
	}
	if s == "" {
		return "unnamed-skill"
	}
	return s
}

func SubpathSafe(basePath, subpath string) bool {
	base, err := filepath.Abs(basePath)
	if err != nil {
		return false
	}
	target, err := filepath.Abs(filepath.Join(basePath, subpath))
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." && !filepath.IsAbs(rel))
}

func PathSafe(basePath, targetPath string) bool {
	base, err := filepath.Abs(basePath)
	if err != nil {
		return false
	}
	target, err := filepath.Abs(targetPath)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." && !filepath.IsAbs(rel))
}

func FolderHash(dir string) (string, error) {
	type fileData struct {
		path string
		data []byte
	}
	var files []fileData
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		files = append(files, fileData{path: filepath.ToSlash(rel), data: data})
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })
	h := sha256.New()
	for _, f := range files {
		h.Write([]byte(f.path))
		h.Write(f.data)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func ShouldInstallInternal() bool {
	v := os.Getenv("INSTALL_INTERNAL_SKILLS")
	return v == "1" || strings.EqualFold(v, "true")
}

func hasSkillMD(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, "SKILL.md"))
	return err == nil && info.Mode().IsRegular()
}

func findSkillDirs(root string, maxDepth int) []string {
	var out []string
	var walk func(string, int)
	walk = func(dir string, depth int) {
		if depth > maxDepth {
			return
		}
		if hasSkillMD(dir) {
			out = append(out, dir)
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, entry := range entries {
			if entry.IsDir() && !skipDirs[entry.Name()] {
				walk(filepath.Join(dir, entry.Name()), depth+1)
			}
		}
	}
	walk(root, 0)
	return out
}

func priorityDirs(searchPath string) []string {
	return []string{
		searchPath,
		filepath.Join(searchPath, "skills"),
		filepath.Join(searchPath, "skills", ".curated"),
		filepath.Join(searchPath, "skills", ".experimental"),
		filepath.Join(searchPath, "skills", ".system"),
		filepath.Join(searchPath, ".agents", "skills"),
		filepath.Join(searchPath, ".claude", "skills"),
		filepath.Join(searchPath, ".codex", "skills"),
		filepath.Join(searchPath, ".cursor", "skills"),
	}
}

func parseYAMLScalar(line string) (string, any, bool) {
	i := strings.Index(line, ":")
	if i < 0 {
		return "", nil, false
	}
	key := strings.TrimSpace(line[:i])
	val := strings.TrimSpace(line[i+1:])
	if key == "" {
		return "", nil, false
	}
	if val == "" {
		return key, map[string]any{}, true
	}
	val = strings.Trim(val, `"'`)
	switch strings.ToLower(val) {
	case "true":
		return key, true, true
	case "false":
		return key, false, true
	}
	return key, val, true
}

func splitLines(raw string) []string {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	return strings.Split(raw, "\n")
}

func metadataFrom(data map[string]any, keys ...string) map[string]any {
	md := map[string]any{}
	if metadata, ok := data["metadata"].(map[string]any); ok {
		for k, v := range metadata {
			md[k] = v
		}
	}
	for _, key := range keys {
		if value, ok := data[key]; ok {
			if _, exists := md[key]; !exists {
				md[key] = value
			}
		}
	}
	if len(md) == 0 {
		return nil
	}
	return md
}
