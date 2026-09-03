package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const (
	maxSkillNameLength     = 64
	maxDescriptionLength   = 1024
	maxCompatibilityLength = 500
)

// ValidateSkillDirectory validates a skill directory using the Agent Skills
// conformance profile. It accepts either SKILL.md or skill.md, matching the
// pinned reference parser's compatibility behavior.
func ValidateSkillDirectory(dir string) []Diagnostic {
	return ValidateSkillDirectoryWithProfile(dir, ProfileSpec)
}

// ValidateSkillDirectoryWithProfile validates a skill directory with profile.
// It accepts either SKILL.md or skill.md, matching the pinned reference
// parser's compatibility behavior.
func ValidateSkillDirectoryWithProfile(dir string, profile Profile) []Diagnostic {
	path, ok := validationSkillFile(dir)
	if !ok {
		return []Diagnostic{diagnostic(RuleSkillMDRequired, dir, "missing required file: SKILL.md")}
	}
	return ValidateSkillMDWithProfile(path, profile)
}

// ValidateSkillMD validates one SKILL.md file using only normative Agent
// Skills requirements. It deliberately excludes repository-local checks such
// as duplicate names and linked-file existence.
func ValidateSkillMD(path string) []Diagnostic {
	return ValidateSkillMDWithProfile(path, ProfileSpec)
}

// ValidateSkillMDWithProfile validates one SKILL.md file using profile. The
// spec profile deliberately excludes repository-local checks such as duplicate
// names and linked-file existence.
func ValidateSkillMDWithProfile(path string, profile Profile) []Diagnostic {
	raw, err := os.ReadFile(path)
	if err != nil {
		return []Diagnostic{diagnostic(RuleSkillMDRequired, path, "failed to read SKILL.md: "+err.Error())}
	}

	var diagnostics []Diagnostic
	document, err := parseSkillDocument(string(raw))
	if err != nil {
		code := RuleFrontmatterYAML
		if strings.Contains(err.Error(), "must start") || strings.Contains(err.Error(), "properly closed") {
			code = RuleFrontmatterRequired
		}
		diagnostics = append(diagnostics, diagnostic(code, path, err.Error()))
	} else {
		diagnostics = append(diagnostics, validateFrontmatter(document.frontmatter, filepath.Dir(path), path)...)
	}
	diagnostics = append(diagnostics, profileDiagnostics(path, string(raw), profile)...)
	SortDiagnostics(diagnostics)
	return diagnostics
}

func profileDiagnostics(path, raw string, profile Profile) []Diagnostic {
	if profile != ProfileRecommended && profile != ProfilePortable {
		return nil
	}

	diagnostics := []Diagnostic{}
	if lines := skillLineCount(raw); lines > 500 {
		diagnostics = append(diagnostics, diagnosticWithSeverity(
			RuleSkillLineCount,
			SeverityWarning,
			path,
			fmt.Sprintf("SKILL.md has %d lines; official guidance recommends 500 lines or fewer", lines),
		))
	}
	if profile == ProfilePortable && filepath.Base(path) == "skill.md" {
		diagnostics = append(diagnostics, diagnosticWithSeverity(
			RuleSkillFilename,
			SeverityError,
			path,
			"portable profile requires the exact uppercase filename SKILL.md; lowercase skill.md is not portable across case-sensitive clients",
		))
	}
	return diagnostics
}

func skillLineCount(raw string) int {
	if raw == "" {
		return 0
	}
	normalized := strings.ReplaceAll(raw, "\r\n", "\n")
	count := strings.Count(normalized, "\n")
	if !strings.HasSuffix(normalized, "\n") {
		count++
	}
	return count
}

func validateFrontmatter(frontmatter map[string]any, skillDir, path string) []Diagnostic {
	var diagnostics []Diagnostic

	var extra []string
	for field := range frontmatter {
		if !allowedFrontmatterFields[field] {
			extra = append(extra, field)
		}
	}
	if len(extra) > 0 {
		sort.Strings(extra)
		diagnostics = append(diagnostics, diagnostic(RuleTopLevelFields, path, "unexpected frontmatter fields: "+strings.Join(extra, ", ")))
	}

	diagnostics = append(diagnostics, validateName(frontmatter, skillDir, path)...)
	diagnostics = append(diagnostics, validateDescription(frontmatter, path)...)
	diagnostics = append(diagnostics, validateCompatibility(frontmatter, path)...)
	diagnostics = append(diagnostics, validateMetadata(frontmatter, path)...)
	diagnostics = append(diagnostics, validateStringField(frontmatter, "allowed-tools", RuleAllowedToolsType, path)...)
	return diagnostics
}

func validateName(frontmatter map[string]any, skillDir, path string) []Diagnostic {
	value, exists := frontmatter["name"]
	if !exists {
		return []Diagnostic{diagnostic(RuleNameRequired, path, "name is required")}
	}
	name, ok := value.(string)
	if !ok || strings.TrimSpace(name) == "" {
		return []Diagnostic{diagnostic(RuleNameType, path, "name must be a non-empty string")}
	}
	name = norm.NFKC.String(strings.TrimSpace(name))
	var diagnostics []Diagnostic
	if utf8.RuneCountInString(name) > maxSkillNameLength {
		diagnostics = append(diagnostics, diagnostic(RuleNameLength, path, "name must be 64 characters or fewer"))
	}
	if name != strings.ToLower(name) {
		diagnostics = append(diagnostics, diagnostic(RuleNameLowercase, path, "name must be lowercase"))
	}
	if strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") {
		diagnostics = append(diagnostics, diagnostic(RuleNameHyphenBoundary, path, "name cannot start or end with a hyphen"))
	}
	if strings.Contains(name, "--") {
		diagnostics = append(diagnostics, diagnostic(RuleNameConsecutiveHyphens, path, "name cannot contain consecutive hyphens"))
	}
	for _, r := range name {
		if r != '-' && !unicode.IsLetter(r) && !unicode.IsNumber(r) {
			diagnostics = append(diagnostics, diagnostic(RuleNameCharacters, path, "name contains invalid characters; only letters, digits, and hyphens are allowed"))
			break
		}
	}
	if norm.NFKC.String(filepath.Base(skillDir)) != name {
		diagnostics = append(diagnostics, diagnostic(RuleNameDirectory, path, fmt.Sprintf("name %q must match parent directory %q", name, filepath.Base(skillDir))))
	}
	return diagnostics
}

func validateDescription(frontmatter map[string]any, path string) []Diagnostic {
	value, exists := frontmatter["description"]
	if !exists {
		return []Diagnostic{diagnostic(RuleDescriptionRequired, path, "description is required")}
	}
	description, ok := value.(string)
	if !ok || strings.TrimSpace(description) == "" {
		return []Diagnostic{diagnostic(RuleDescriptionType, path, "description must be a non-empty string")}
	}
	if utf8.RuneCountInString(description) > maxDescriptionLength {
		return []Diagnostic{diagnostic(RuleDescriptionLength, path, "description must be 1024 characters or fewer")}
	}
	return nil
}

func validateCompatibility(frontmatter map[string]any, path string) []Diagnostic {
	value, exists := frontmatter["compatibility"]
	if !exists {
		return nil
	}
	compatibility, ok := value.(string)
	if !ok {
		return []Diagnostic{diagnostic(RuleCompatibilityType, path, "compatibility must be a string")}
	}
	length := utf8.RuneCountInString(compatibility)
	if length == 0 {
		return []Diagnostic{diagnostic(RuleCompatibilityLength, path, "compatibility must be at least 1 character")}
	}
	if length > maxCompatibilityLength {
		return []Diagnostic{diagnostic(RuleCompatibilityLength, path, "compatibility must be 500 characters or fewer")}
	}
	return nil
}

func validateMetadata(frontmatter map[string]any, path string) []Diagnostic {
	value, exists := frontmatter["metadata"]
	if !exists {
		return nil
	}
	metadata := reflect.ValueOf(value)
	if !metadata.IsValid() || metadata.Kind() != reflect.Map {
		return []Diagnostic{diagnostic(RuleMetadataMapping, path, "metadata must be a mapping")}
	}
	iterator := metadata.MapRange()
	for iterator.Next() {
		key := iterator.Key()
		value := iterator.Value()
		for key.Kind() == reflect.Interface {
			key = key.Elem()
		}
		for value.Kind() == reflect.Interface {
			value = value.Elem()
		}
		if key.Kind() != reflect.String || value.Kind() != reflect.String {
			return []Diagnostic{diagnostic(RuleMetadataValues, path, "metadata keys and values must be strings")}
		}
	}
	return nil
}

func validateStringField(frontmatter map[string]any, field, code, path string) []Diagnostic {
	value, exists := frontmatter[field]
	if !exists {
		return nil
	}
	if _, ok := value.(string); !ok {
		return []Diagnostic{diagnostic(code, path, field+" must be a string")}
	}
	return nil
}

func diagnostic(code, path, message string) Diagnostic {
	return diagnosticWithSeverity(code, SeverityError, path, message)
}

func diagnosticWithSeverity(code string, severity Severity, path, message string) Diagnostic {
	return Diagnostic{Code: code, Severity: severity, Message: message, Path: path}
}

// SortDiagnostics provides stable rendering and machine consumption order.
func SortDiagnostics(diagnostics []Diagnostic) {
	sort.SliceStable(diagnostics, func(i, j int) bool {
		left, right := diagnostics[i], diagnostics[j]
		if left.Path != right.Path {
			return left.Path < right.Path
		}
		if left.Line != right.Line {
			return left.Line < right.Line
		}
		if left.Column != right.Column {
			return left.Column < right.Column
		}
		return left.Code < right.Code
	})
}
