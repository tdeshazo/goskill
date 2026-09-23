package commands

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/tdeshazo/goskill/internal/skills"
	"github.com/tdeshazo/goskill/internal/terminal"
)

func TestRulesListsCatalogInStableOrderAndJSONIsMachineSafe(t *testing.T) {
	var out bytes.Buffer
	app := App{Version: "test", Stdout: &out, Stderr: &bytes.Buffer{}, Cwd: t.TempDir()}
	if err := app.Run([]string{"rules", "--json"}); err != nil {
		t.Fatal(err)
	}
	if out.String() != terminal.StripEscapes(out.String()) {
		t.Fatalf("rules JSON contains ANSI: %q", out.String())
	}
	var got []skills.Rule
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("rules JSON: %v\n%s", err, out.String())
	}
	want := skills.Rules()
	if len(got) != len(want) {
		t.Fatalf("rules count = %d, want %d", len(got), len(want))
	}
	for i, rule := range want {
		if got[i] != rule {
			t.Fatalf("rule %d = %#v, want %#v", i, got[i], rule)
		}
	}

	first := out.String()
	out.Reset()
	if err := app.Run([]string{"rules", "--json"}); err != nil {
		t.Fatal(err)
	}
	if out.String() != first {
		t.Fatalf("rules JSON is not deterministic\nfirst: %s\nsecond: %s", first, out.String())
	}
}

func TestRulesTextAndExplainDisplayCatalogEvidence(t *testing.T) {
	var out bytes.Buffer
	app := App{Version: "test", Stdout: &out, Stderr: &bytes.Buffer{}, Cwd: t.TempDir()}
	if err := app.Run([]string{"rules"}); err != nil {
		t.Fatal(err)
	}
	text := terminal.StripEscapes(out.String())
	for _, want := range []string{
		skills.RuleSkillMDRequired,
		skills.RuleSkillLineCount,
		skills.RuleLocalReferenceMissing,
		skills.RuleLocalReferenceEscape,
		skills.RuleSkillFilename,
		"error · spec",
		"warning · recommended",
		"rationale: case-sensitive clients",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("rules text missing %q:\n%s", want, text)
		}
	}

	out.Reset()
	if err := app.Run([]string{"explain", "gp310"}); err != nil {
		t.Fatal(err)
	}
	text = terminal.StripEscapes(out.String())
	for _, want := range []string{"Rule GP310", "Profile: portable", "Rationale: case-sensitive clients require the exact uppercase filename"} {
		if !strings.Contains(text, want) {
			t.Fatalf("explain text missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Source:") {
		t.Fatalf("rationale-only rule rendered a source:\n%s", text)
	}
}

func TestExplainJSONNormalizesRuleCodeAndIncludesCompleteMetadata(t *testing.T) {
	var out bytes.Buffer
	app := App{Version: "test", Stdout: &out, Stderr: &bytes.Buffer{}, Cwd: t.TempDir()}
	if err := app.Run([]string{"explain", "--json", " as001 "}); err != nil {
		t.Fatal(err)
	}
	if out.String() != terminal.StripEscapes(out.String()) {
		t.Fatalf("explain JSON contains ANSI: %q", out.String())
	}
	var rule skills.Rule
	if err := json.Unmarshal(out.Bytes(), &rule); err != nil {
		t.Fatalf("explain JSON: %v\n%s", err, out.String())
	}
	if rule.Code != skills.RuleSkillMDRequired || rule.Source == "" || rule.Profile != skills.ProfileSpec {
		t.Fatalf("explained rule = %#v", rule)
	}
}

func TestRuleCommandsRejectInvalidInvocationWithoutOutput(t *testing.T) {
	tests := []struct {
		args    []string
		wantErr string
	}{
		{args: []string{"rules", "extra"}, wantErr: "unknown command"},
		{args: []string{"rules", "--json", "--json"}, wantErr: "may only be specified once"},
		{args: []string{"explain"}, wantErr: "accepts 1 arg(s)"},
		{args: []string{"explain", "AS001", "AS002"}, wantErr: "accepts 1 arg(s)"},
		{args: []string{"explain", "--unknown", "AS001"}, wantErr: "unknown flag"},
		{args: []string{"explain", "ZZ999"}, wantErr: "unknown rule code \"ZZ999\""},
		{args: []string{"explain", "aſ001"}, wantErr: "unknown rule code \"Aſ001\""},
	}
	for _, test := range tests {
		t.Run(strings.Join(test.args, " "), func(t *testing.T) {
			var out bytes.Buffer
			app := App{Version: "test", Stdout: &out, Stderr: &bytes.Buffer{}, Cwd: t.TempDir()}
			err := app.Run(test.args)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("Run(%v) error = %v", test.args, err)
			}
			if out.Len() != 0 {
				t.Fatalf("invalid invocation emitted output: %q", out.String())
			}
		})
	}
}

func TestRuleCommandHelpAndRootHelp(t *testing.T) {
	var out bytes.Buffer
	app := App{Version: "test", Stdout: &out, Stderr: &bytes.Buffer{}, Cwd: t.TempDir()}
	if err := app.Run([]string{"rules", "--help"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "goskill rules [--json]") {
		t.Fatalf("rules help = %s", out.String())
	}
	out.Reset()
	if err := app.Run([]string{"explain", "--help"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "goskill explain [--json] <code>") {
		t.Fatalf("explain help = %s", out.String())
	}
	if !strings.Contains(renderHelp(), "rules, explain") {
		t.Fatalf("root help = %s", renderHelp())
	}
}
