package source

import "testing"

func TestParseGitHubShorthandWithSkill(t *testing.T) {
	got, err := Parse("vercel-labs/agent-skills@find-skills")
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != GitHub {
		t.Fatalf("type = %s", got.Type)
	}
	if got.URL != "https://github.com/vercel-labs/agent-skills.git" {
		t.Fatalf("url = %s", got.URL)
	}
	if got.SkillFilter != "find-skills" {
		t.Fatalf("skill filter = %q", got.SkillFilter)
	}
}

func TestParseTreeURLWithSubpath(t *testing.T) {
	got, err := Parse("https://github.com/acme/repo/tree/main/skills/demo")
	if err != nil {
		t.Fatal(err)
	}
	if got.Ref != "main" || got.Subpath != "skills/demo" {
		t.Fatalf("ref/subpath = %q/%q", got.Ref, got.Subpath)
	}
}

func TestRejectUnsafeSubpath(t *testing.T) {
	_, err := Parse("owner/repo/../secret")
	if err == nil {
		t.Fatal("expected unsafe subpath error")
	}
}
