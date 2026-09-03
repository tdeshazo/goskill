package skills

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type parsedSkillDocument struct {
	frontmatter map[string]any
	node        *yaml.Node
}

type parseSkillError struct {
	message  string
	location sourceLocation
}

func (e parseSkillError) Error() string {
	return e.message
}

var yamlErrorLine = regexp.MustCompile(`line ([0-9]+)`) // yaml.v3 reports source lines in errors.

func parseSkillDocument(raw string) (parsedSkillDocument, error) {
	lines := splitLines(raw)
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "---") {
		return parsedSkillDocument{}, parseSkillError{
			message:  "SKILL.md must start with YAML frontmatter (---)",
			location: fileLocation(),
		}
	}

	closing := -1
	for i, line := range lines[1:] {
		if strings.TrimSpace(line) == "---" {
			closing = i + 1
			break
		}
	}
	if closing == -1 {
		return parsedSkillDocument{}, parseSkillError{
			message:  "SKILL.md frontmatter is not properly closed with ---",
			location: fileLocation(),
		}
	}

	var node yaml.Node
	frontmatterYAML := strings.Join(lines[1:closing], "\n")
	if err := yaml.Unmarshal([]byte(frontmatterYAML), &node); err != nil {
		return parsedSkillDocument{}, parseSkillError{
			message:  fmt.Sprintf("invalid YAML in frontmatter: %v", err),
			location: yamlErrorLocation(err),
		}
	}
	var value any
	if err := node.Decode(&value); err != nil {
		return parsedSkillDocument{}, parseSkillError{
			message:  fmt.Sprintf("invalid YAML in frontmatter: %v", err),
			location: yamlErrorLocation(err),
		}
	}
	frontmatter, ok := value.(map[string]any)
	if !ok || frontmatter == nil {
		return parsedSkillDocument{}, parseSkillError{
			message:  "SKILL.md frontmatter must be a YAML mapping",
			location: nodeLocation(documentContent(&node)),
		}
	}
	return parsedSkillDocument{frontmatter: frontmatter, node: documentContent(&node)}, nil
}

func documentContent(node *yaml.Node) *yaml.Node {
	if node != nil && node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		return node.Content[0]
	}
	return node
}

func yamlErrorLocation(err error) sourceLocation {
	match := yamlErrorLine.FindStringSubmatch(err.Error())
	if len(match) != 2 {
		return fileLocation()
	}
	line, conversionErr := strconv.Atoi(match[1])
	if conversionErr != nil || line < 1 {
		return fileLocation()
	}
	return sourceLocation{Line: line + 1, Column: 1}
}
