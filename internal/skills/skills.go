package skills

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/tdeshazo/goskill/internal/terminal"
	"golang.org/x/text/unicode/norm"
	"gopkg.in/yaml.v3"
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

type ValidationIssue struct {
	Message string
}

const (
	maxSkillNameLength     = 64
	maxDescriptionLength   = 1024
	maxCompatibilityLength = 500
)

var allowedFrontmatterFields = map[string]bool{
	"name":          true,
	"description":   true,
	"license":       true,
	"allowed-tools": true,
	"metadata":      true,
	"compatibility": true,
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

func ValidateSkillMD(path string) []ValidationIssue {
	rawBytes, err := os.ReadFile(path)
	if err != nil {
		return []ValidationIssue{{Message: "failed to read SKILL.md: " + err.Error()}}
	}
	raw := string(rawBytes)
	referenceIssues := validateLocalReferences(path, raw)
	frontmatter, ok := frontmatterBlock(raw)
	if !ok {
		return append([]ValidationIssue{{Message: "missing frontmatter"}}, referenceIssues...)
	}
	data := map[string]any{}
	if err := yaml.Unmarshal([]byte(frontmatter), &data); err != nil {
		return append([]ValidationIssue{{Message: "invalid YAML: " + err.Error()}}, referenceIssues...)
	}

	var issues []ValidationIssue
	issues = append(issues, validateFrontmatterFields(data)...)
	name, nameOK := stringField(data, "name")
	if !nameOK {
		issues = append(issues, ValidationIssue{Message: "name is required"})
	} else {
		issues = append(issues, validateSkillName(name, filepath.Dir(path))...)
	}
	desc, descOK := stringField(data, "description")
	if !descOK {
		issues = append(issues, ValidationIssue{Message: "description is required"})
	} else if utf8.RuneCountInString(desc) > maxDescriptionLength {
		issues = append(issues, ValidationIssue{Message: "description must be 1024 characters or fewer"})
	}
	if compatibility, ok := data["compatibility"]; ok {
		compat, ok := compatibility.(string)
		if !ok {
			issues = append(issues, ValidationIssue{Message: "compatibility must be a string"})
		} else if utf8.RuneCountInString(compat) > maxCompatibilityLength {
			issues = append(issues, ValidationIssue{Message: "compatibility must be 500 characters or fewer"})
		}
	}
	return append(issues, referenceIssues...)
}

func SkillMDName(path string) (string, bool) {
	rawBytes, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	frontmatter, ok := frontmatterBlock(string(rawBytes))
	if !ok {
		return "", false
	}
	data := map[string]any{}
	if err := yaml.Unmarshal([]byte(frontmatter), &data); err != nil {
		return "", false
	}
	name, ok := stringField(data, "name")
	if !ok {
		return "", false
	}
	return norm.NFKC.String(strings.TrimSpace(name)), true
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

func frontmatterBlock(raw string) (string, bool) {
	lines := splitLines(raw)
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return "", false
	}
	for i, line := range lines[1:] {
		if strings.TrimSpace(line) == "---" {
			return strings.Join(lines[1:i+1], "\n"), true
		}
	}
	return "", false
}

func stringField(data map[string]any, key string) (string, bool) {
	value, ok := data[key]
	if !ok {
		return "", false
	}
	s, ok := value.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return "", false
	}
	return s, true
}

func validateFrontmatterFields(data map[string]any) []ValidationIssue {
	var extra []string
	for field := range data {
		if !allowedFrontmatterFields[field] {
			extra = append(extra, field)
		}
	}
	if len(extra) == 0 {
		return nil
	}
	sort.Strings(extra)
	return []ValidationIssue{{Message: "unexpected frontmatter fields: " + strings.Join(extra, ", ")}}
}

func validateSkillName(rawName, skillDir string) []ValidationIssue {
	name := norm.NFKC.String(strings.TrimSpace(rawName))
	var issues []ValidationIssue
	if utf8.RuneCountInString(name) > maxSkillNameLength {
		issues = append(issues, ValidationIssue{Message: "name must be 64 characters or fewer"})
	}
	if name != strings.ToLower(name) {
		issues = append(issues, ValidationIssue{Message: "name must be lowercase"})
	}
	if strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") {
		issues = append(issues, ValidationIssue{Message: "name cannot start or end with a hyphen"})
	}
	if strings.Contains(name, "--") {
		issues = append(issues, ValidationIssue{Message: "name cannot contain consecutive hyphens"})
	}
	if !isValidSkillNameCharacters(name) {
		issues = append(issues, ValidationIssue{Message: "name contains invalid characters; only letters, digits, and hyphens are allowed"})
	}
	parent := norm.NFKC.String(filepath.Base(skillDir))
	if parent != name {
		issues = append(issues, ValidationIssue{Message: "name must match parent directory"})
	}
	return issues
}

func isValidSkillNameCharacters(name string) bool {
	for _, r := range name {
		if r == '-' || unicode.IsLetter(r) || unicode.IsNumber(r) {
			continue
		}
		return false
	}
	return name != ""
}

func validateLocalReferences(skillMDPath, raw string) []ValidationIssue {
	var issues []ValidationIssue
	seen := map[string]bool{}
	for _, ref := range markdownReferences(raw) {
		normalized, ok := normalizeLocalReference(ref)
		if !ok || seen[normalized] {
			continue
		}
		seen[normalized] = true
		if filepath.IsAbs(normalized) {
			issues = append(issues, ValidationIssue{Message: "reference must be relative: " + ref})
			continue
		}
		target := filepath.Join(filepath.Dir(skillMDPath), filepath.FromSlash(normalized))
		if !PathSafe(filepath.Dir(skillMDPath), target) {
			issues = append(issues, ValidationIssue{Message: "reference escapes skill directory: " + ref})
			continue
		}
		if _, err := os.Stat(target); err != nil {
			issues = append(issues, ValidationIssue{Message: "reference does not exist: " + ref})
		}
	}
	return issues
}

func markdownReferences(raw string) []string {
	var refs []string
	inline := regexp.MustCompile(`!?\[[^\]]*\]\(([^)\s]+)(?:\s+["'][^"']*["'])?\)`)
	for _, match := range inline.FindAllStringSubmatch(raw, -1) {
		refs = append(refs, match[1])
	}
	definition := regexp.MustCompile(`(?m)^\s*\[[^\]]+\]:\s*(\S+)`)
	for _, match := range definition.FindAllStringSubmatch(raw, -1) {
		refs = append(refs, match[1])
	}
	return refs
}

func normalizeLocalReference(ref string) (string, bool) {
	ref = strings.TrimSpace(strings.Trim(ref, "<>"))
	if ref == "" || strings.HasPrefix(ref, "#") || strings.HasPrefix(ref, "//") {
		return "", false
	}
	u, err := url.Parse(ref)
	if err == nil && u.Scheme != "" {
		return "", false
	}
	if i := strings.IndexAny(ref, "?#"); i >= 0 {
		ref = ref[:i]
	}
	if ref == "" {
		return "", false
	}
	if unescaped, err := url.PathUnescape(ref); err == nil {
		ref = unescaped
	}
	return filepath.ToSlash(filepath.Clean(filepath.FromSlash(ref))), true
}

func validationSkillFile(dir string) (string, bool) {
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
