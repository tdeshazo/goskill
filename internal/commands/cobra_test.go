package commands

import (
	"bytes"
	"strings"
	"testing"
)

func TestCobraHelpAndCompletion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	app := App{Version: "test", Stdout: &stdout, Stderr: &stderr, Cwd: t.TempDir()}
	if err := app.Run([]string{"add", "--help"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"add <source>", "--agent", "--skill", "--full-depth"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("add help missing %q:\n%s", want, stdout.String())
		}
	}
	stdout.Reset()
	if err := app.Run([]string{"completion", "bash"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "bash completion V2 for goskill") || stderr.Len() != 0 {
		t.Fatalf("completion output length = %d, stderr = %q", stdout.Len(), stderr.String())
	}
}

func TestCobraUsageSpec(t *testing.T) {
	for _, args := range [][]string{{"--usage-spec"}, {"find", "--usage-spec"}} {
		var stdout, stderr bytes.Buffer
		app := App{Version: "test-version", Stdout: &stdout, Stderr: &stderr, Cwd: t.TempDir()}
		if err := app.Run(args); err != nil {
			t.Fatalf("Run(%v) error = %v", args, err)
		}
		got := stdout.String()
		for _, want := range []string{
			"name goskill\n",
			"bin goskill\n",
			"version test-version\n",
			"cmd add ",
			"alias search f s\n",
			"flag --provider ",
			"arg \"[query]…\" required=#false var=#true\n",
			"cmd use ",
			"arg \"[source]\" required=#false\n",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("Run(%v) usage spec missing %q:\n%s", args, want, got)
			}
		}
		if strings.Contains(got, "\x1b[") || stderr.Len() != 0 {
			t.Errorf("Run(%v) emitted terminal output: stderr %q", args, stderr.String())
		}
	}
}

func TestCobraUsageSpecAfterTerminatorIsPositional(t *testing.T) {
	var stdout bytes.Buffer
	app := App{Version: "test", Stdout: &stdout, Stderr: &bytes.Buffer{}, Cwd: t.TempDir()}
	if err := app.Run([]string{"--", "--usage-spec"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "The open agent skills ecosystem") {
		t.Fatalf("expected banner after -- terminator, got %q", stdout.String())
	}
}

func TestCobraRunEmptyArgsShowsBanner(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "nil"},
		{name: "empty", args: []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			app := App{Version: "test", Stdout: &stdout, Stderr: &stderr, Cwd: t.TempDir()}

			if err := app.Run(tc.args); err != nil {
				t.Fatalf("Run(%v) error = %v", tc.args, err)
			}
			if !strings.Contains(stdout.String(), "The open agent skills ecosystem") {
				t.Fatalf("Run(%v) output does not contain the banner:\n%s", tc.args, stdout.String())
			}
			if stderr.Len() != 0 {
				t.Fatalf("Run(%v) stderr = %q", tc.args, stderr.String())
			}
		})
	}
}

func TestCobraRejectsUnknownFlags(t *testing.T) {
	for _, args := range [][]string{
		{"add", "--unknown"},
		{"list", "--unknown"},
		{"sync", "--unknown"},
		{"validate", "--unknown"},
	} {
		err := (App{Version: "test", Stderr: &bytes.Buffer{}}).Run(args)
		if err == nil || !strings.Contains(err.Error(), "unknown flag") {
			t.Errorf("Run(%v) error = %v", args, err)
		}
	}
}

func TestCobraPreservesMultiValueAgentAndSkillFlags(t *testing.T) {
	cmd := (App{Version: "test"}).addCommand()
	args := expandVariadicFlags([]string{"add", "source", "--agent", "codex", "cursor", "--skill", "one", "two"})
	if err := cmd.ParseFlags(args[1:]); err != nil {
		t.Fatal(err)
	}
	agents, _ := cmd.Flags().GetStringArray("agent")
	skills, _ := cmd.Flags().GetStringArray("skill")
	if strings.Join(agents, ",") != "codex,cursor" || strings.Join(skills, ",") != "one,two" {
		t.Fatalf("agents = %q, skills = %q", agents, skills)
	}
}

func TestCobraFindRequiresSeparateProviderValue(t *testing.T) {
	t.Setenv("GOSKILL_DISABLE_SKILLMD", "1")
	t.Setenv("GOSKILL_DISABLE_TRUEFOUNDRY_CATALOG", "1")
	t.Setenv("GOSKILL_PROVIDER_CONFIG_JSON", `{"providers":[]}`)
	t.Setenv("SKILLS_API_URL", "http://[")

	var stdout, stderr bytes.Buffer
	app := App{Version: "test", Stdout: &stdout, Stderr: &stderr, Cwd: t.TempDir()}
	err := app.Run([]string{"find", "--provider", "--json", "react"})
	if err == nil || err.Error() != "--provider requires a value" {
		t.Fatalf("find error = %v, want --provider requires a value", err)
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("find wrote output before rejecting --provider: stdout %q, stderr %q", stdout.String(), stderr.String())
	}
}

func TestCobraFindProviderValueParsingBoundaries(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{
			name:    "option-looking separate value",
			args:    []string{"find", "--provider", "--json", "react"},
			wantErr: true,
		},
		{
			name:    "missing separate value",
			args:    []string{"find", "--provider"},
			wantErr: true,
		},
		{
			name:    "option after terminator is a query",
			args:    []string{"find", "--", "--provider", "--json", "react"},
			wantErr: false,
		},
		{
			name:    "terminator cannot be provider value",
			args:    []string{"find", "--provider", "--", "react"},
			wantErr: true,
		},
		{
			name:    "explicit assignment",
			args:    []string{"find", "--provider=--json", "react"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateFindProviderValue(tt.args)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateFindProviderValue(%v) error = %v, wantErr %v", tt.args, err, tt.wantErr)
			}
		})
	}

	cmd := (App{Version: "test"}).findCommand()
	if err := cmd.ParseFlags([]string{"--provider=--json", "react"}); err != nil {
		t.Fatal(err)
	}
	provider, err := cmd.Flags().GetString("provider")
	if err != nil {
		t.Fatal(err)
	}
	if provider != "--json" {
		t.Fatalf("explicit --provider=value parsed as %q, want %q", provider, "--json")
	}
}
