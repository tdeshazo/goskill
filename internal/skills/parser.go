package skills

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

type parsedSkillDocument struct {
	frontmatter map[string]any
}

func parseSkillDocument(raw string) (parsedSkillDocument, error) {
	lines := splitLines(raw)
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "---") {
		return parsedSkillDocument{}, fmt.Errorf("SKILL.md must start with YAML frontmatter (---)")
	}

	closing := -1
	for i, line := range lines[1:] {
		if strings.TrimSpace(line) == "---" {
			closing = i + 1
			break
		}
	}
	if closing == -1 {
		return parsedSkillDocument{}, fmt.Errorf("SKILL.md frontmatter is not properly closed with ---")
	}

	var value any
	if err := yaml.Unmarshal([]byte(strings.Join(lines[1:closing], "\n")), &value); err != nil {
		return parsedSkillDocument{}, fmt.Errorf("invalid YAML in frontmatter: %w", err)
	}
	frontmatter, ok := value.(map[string]any)
	if !ok || frontmatter == nil {
		return parsedSkillDocument{}, fmt.Errorf("SKILL.md frontmatter must be a YAML mapping")
	}
	return parsedSkillDocument{frontmatter: frontmatter}, nil
}
