package wellknown

import (
	"testing"

	"github.com/tdeshazo/goskill/internal/skills"
)

func TestYAMLDescriptionUsesSkillFrontmatter(t *testing.T) {
	files := []skills.SnapshotFile{
		{
			Path:     "SKILL.md",
			Contents: "---\nname: demo-skill\ndescription: YAML description\n---\n\n# Body\n\nLong body text.",
		},
	}

	desc, ok := yamlDescription(files)
	if !ok {
		t.Fatal("expected YAML description")
	}
	if desc != "YAML description" {
		t.Fatalf("desc = %q, want YAML description", desc)
	}
}

func TestYAMLDescriptionIgnoresNonSkillFiles(t *testing.T) {
	files := []skills.SnapshotFile{
		{
			Path:     "README.md",
			Contents: "---\nname: readme\ndescription: README description\n---\n",
		},
	}

	if desc, ok := yamlDescription(files); ok {
		t.Fatalf("desc = %q, want no SKILL.md description", desc)
	}
}
