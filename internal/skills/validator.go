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
	"gopkg.in/yaml.v3"
)

const (
	maxSkillNameLength     = 64
	maxDescriptionLength   = 1024
	maxCompatibilityLength = 500
)

type sourceLocation struct {
	Line   int
	Column int
}

func fileLocation() sourceLocation {
	return sourceLocation{Line: 1, Column: 1}
}

func nodeLocation(node *yaml.Node) sourceLocation {
	if node == nil || node.Line < 1 {
		return fileLocation()
	}
	column := node.Column
	if column < 1 {
		column = 1
	}
	// yaml.v3 parses the frontmatter after its opening delimiter, so its first
	// source line is line two in SKILL.md.
	return sourceLocation{Line: node.Line + 1, Column: column}
}

func frontmatterFieldNode(document parsedSkillDocument, field string) *yaml.Node {
	if document.node == nil || document.node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(document.node.Content); i += 2 {
		if document.node.Content[i].Value == field {
			return document.node.Content[i+1]
		}
	}
	return nil
}

func frontmatterFieldKeyNode(document parsedSkillDocument, field string) *yaml.Node {
	if document.node == nil || document.node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(document.node.Content); i += 2 {
		if document.node.Content[i].Value == field {
			return document.node.Content[i]
		}
	}
	return nil
}

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
		return []Diagnostic{diagnosticAt(RuleSkillMDRequired, dir, "missing required file: SKILL.md", fileLocation())}
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
		return []Diagnostic{diagnosticAt(RuleSkillMDRequired, path, "failed to read SKILL.md: "+err.Error(), fileLocation())}
	}

	var diagnostics []Diagnostic
	document, err := parseSkillDocument(string(raw))
	if err != nil {
		code := RuleFrontmatterYAML
		location := fileLocation()
		if strings.Contains(err.Error(), "must start") || strings.Contains(err.Error(), "properly closed") {
			code = RuleFrontmatterRequired
		}
		if parseErr, ok := err.(parseSkillError); ok {
			location = parseErr.location
		}
		diagnostics = append(diagnostics, diagnosticAt(code, path, err.Error(), location))
	} else {
		diagnostics = append(diagnostics, validateFrontmatter(document, filepath.Dir(path), path)...)
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
		diagnostics = append(diagnostics, diagnosticWithSeverityAt(
			RuleSkillLineCount,
			SeverityWarning,
			path,
			fmt.Sprintf("SKILL.md has %d lines; official guidance recommends 500 lines or fewer", lines),
			fileLocation(),
		))
	}
	if profile == ProfilePortable && filepath.Base(path) == "skill.md" {
		diagnostics = append(diagnostics, diagnosticWithSeverityAt(
			RuleSkillFilename,
			SeverityError,
			path,
			"portable profile requires the exact uppercase filename SKILL.md; lowercase skill.md is not portable across case-sensitive clients",
			fileLocation(),
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

func validateFrontmatter(document parsedSkillDocument, skillDir, path string) []Diagnostic {
	frontmatter := document.frontmatter
	var diagnostics []Diagnostic

	var extra []string
	for field := range frontmatter {
		if !allowedFrontmatterFields[field] {
			extra = append(extra, field)
		}
	}
	if len(extra) > 0 {
		sort.Strings(extra)
		diagnostics = append(diagnostics, diagnosticAt(
			RuleTopLevelFields,
			path,
			"unexpected frontmatter fields: "+strings.Join(extra, ", "),
			nodeLocation(frontmatterFieldKeyNode(document, extra[0])),
		))
	}

	diagnostics = append(diagnostics, validateName(document, skillDir, path)...)
	diagnostics = append(diagnostics, validateDescription(document, path)...)
	diagnostics = append(diagnostics, validateCompatibility(document, path)...)
	diagnostics = append(diagnostics, validateMetadata(document, path)...)
	diagnostics = append(diagnostics, validateStringField(document, "allowed-tools", RuleAllowedToolsType, path)...)
	return diagnostics
}

func validateName(document parsedSkillDocument, skillDir, path string) []Diagnostic {
	frontmatter := document.frontmatter
	value, exists := frontmatter["name"]
	if !exists {
		return []Diagnostic{diagnosticAt(RuleNameRequired, path, "name is required", fileLocation())}
	}
	location := nodeLocation(frontmatterFieldNode(document, "name"))
	name, ok := value.(string)
	if !ok || strings.TrimSpace(name) == "" {
		return []Diagnostic{diagnosticAt(RuleNameType, path, "name must be a non-empty string", location)}
	}
	name = norm.NFKC.String(strings.TrimSpace(name))
	var diagnostics []Diagnostic
	if utf8.RuneCountInString(name) > maxSkillNameLength {
		diagnostics = append(diagnostics, diagnosticAt(RuleNameLength, path, "name must be 64 characters or fewer", location))
	}
	if name != strings.ToLower(name) {
		diagnostics = append(diagnostics, diagnosticAt(RuleNameLowercase, path, "name must be lowercase", location))
	}
	if strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") {
		diagnostics = append(diagnostics, diagnosticAt(RuleNameHyphenBoundary, path, "name cannot start or end with a hyphen", location))
	}
	if strings.Contains(name, "--") {
		diagnostics = append(diagnostics, diagnosticAt(RuleNameConsecutiveHyphens, path, "name cannot contain consecutive hyphens", location))
	}
	for _, r := range name {
		if r != '-' && !unicode.IsLetter(r) && !unicode.IsNumber(r) {
			diagnostics = append(diagnostics, diagnosticAt(RuleNameCharacters, path, "name contains invalid characters; only letters, digits, and hyphens are allowed", location))
			break
		}
	}
	if norm.NFKC.String(filepath.Base(skillDir)) != name {
		diagnostics = append(diagnostics, diagnosticAt(RuleNameDirectory, path, fmt.Sprintf("name %q must match parent directory %q", name, filepath.Base(skillDir)), location))
	}
	return diagnostics
}

func validateDescription(document parsedSkillDocument, path string) []Diagnostic {
	frontmatter := document.frontmatter
	value, exists := frontmatter["description"]
	if !exists {
		return []Diagnostic{diagnosticAt(RuleDescriptionRequired, path, "description is required", fileLocation())}
	}
	location := nodeLocation(frontmatterFieldNode(document, "description"))
	description, ok := value.(string)
	if !ok || strings.TrimSpace(description) == "" {
		return []Diagnostic{diagnosticAt(RuleDescriptionType, path, "description must be a non-empty string", location)}
	}
	if utf8.RuneCountInString(description) > maxDescriptionLength {
		return []Diagnostic{diagnosticAt(RuleDescriptionLength, path, "description must be 1024 characters or fewer", location)}
	}
	return nil
}

func validateCompatibility(document parsedSkillDocument, path string) []Diagnostic {
	frontmatter := document.frontmatter
	value, exists := frontmatter["compatibility"]
	if !exists {
		return nil
	}
	location := nodeLocation(frontmatterFieldNode(document, "compatibility"))
	compatibility, ok := value.(string)
	if !ok {
		return []Diagnostic{diagnosticAt(RuleCompatibilityType, path, "compatibility must be a string", location)}
	}
	length := utf8.RuneCountInString(compatibility)
	if length == 0 {
		return []Diagnostic{diagnosticAt(RuleCompatibilityLength, path, "compatibility must be at least 1 character", location)}
	}
	if length > maxCompatibilityLength {
		return []Diagnostic{diagnosticAt(RuleCompatibilityLength, path, "compatibility must be 500 characters or fewer", location)}
	}
	return nil
}

func validateMetadata(document parsedSkillDocument, path string) []Diagnostic {
	frontmatter := document.frontmatter
	value, exists := frontmatter["metadata"]
	if !exists {
		return nil
	}
	metadataNode := frontmatterFieldNode(document, "metadata")
	metadata := reflect.ValueOf(value)
	if !metadata.IsValid() || metadata.Kind() != reflect.Map {
		return []Diagnostic{diagnosticAt(RuleMetadataMapping, path, "metadata must be a mapping", nodeLocation(metadataNode))}
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
			return []Diagnostic{diagnosticAt(RuleMetadataValues, path, "metadata keys and values must be strings", invalidMetadataLocation(metadataNode))}
		}
	}
	return nil
}

func invalidMetadataLocation(node *yaml.Node) sourceLocation {
	if node == nil || node.Kind != yaml.MappingNode {
		return nodeLocation(node)
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
			return nodeLocation(key)
		}
		if value.Kind != yaml.ScalarNode || value.Tag != "!!str" {
			return nodeLocation(value)
		}
	}
	return nodeLocation(node)
}

func validateStringField(document parsedSkillDocument, field, code, path string) []Diagnostic {
	frontmatter := document.frontmatter
	value, exists := frontmatter[field]
	if !exists {
		return nil
	}
	if _, ok := value.(string); !ok {
		return []Diagnostic{diagnosticAt(code, path, field+" must be a string", nodeLocation(frontmatterFieldNode(document, field)))}
	}
	return nil
}

func diagnostic(code, path, message string) Diagnostic {
	return diagnosticAt(code, path, message, fileLocation())
}

func diagnosticWithSeverity(code string, severity Severity, path, message string) Diagnostic {
	return diagnosticWithSeverityAt(code, severity, path, message, fileLocation())
}

func diagnosticAt(code, path, message string, location sourceLocation) Diagnostic {
	return diagnosticWithSeverityAt(code, SeverityError, path, message, location)
}

func diagnosticWithSeverityAt(code string, severity Severity, path, message string, location sourceLocation) Diagnostic {
	return Diagnostic{
		Code:     code,
		Severity: severity,
		Message:  message,
		Path:     path,
		Line:     location.Line,
		Column:   location.Column,
	}
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
