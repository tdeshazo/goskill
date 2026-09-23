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
			"long_help \"Use a skill without installing it. A source may include an @skill selector.\"",
			"arg <source>\n",
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

func TestCobraUsageSpecExportsOptionalVariadicSkillArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	app := App{Version: "test", Stdout: &stdout, Stderr: &stderr, Cwd: t.TempDir()}
	if err := app.Run([]string{"--usage-spec"}); err != nil {
		t.Fatalf("Run(--usage-spec) error = %v", err)
	}

	const wantArg = `    arg "[skills]…" required=#false var=#true`
	usage := stdout.String()
	for _, name := range []string{"remove", "validate", "check", "update"} {
		t.Run(name, func(t *testing.T) {
			cmdMarker := "\ncmd " + name + " "
			cmdMarkerIndex := strings.Index(usage, cmdMarker)
			if cmdMarkerIndex == -1 {
				t.Fatalf("usage spec missing %q command:\n%s", name, usage)
			}
			cmdStart := cmdMarkerIndex + 1

			cmdEnd := len(usage)
			if nextCommand := strings.Index(usage[cmdStart+1:], "\ncmd "); nextCommand != -1 {
				cmdEnd = cmdStart + 1 + nextCommand
			}
			if !strings.Contains(usage[cmdStart:cmdEnd], wantArg) {
				t.Fatalf("usage spec command %q missing optional variadic skill arg %q:\n%s", name, wantArg, usage[cmdStart:cmdEnd])
			}
		})
	}
	if stderr.Len() != 0 {
		t.Fatalf("Run(--usage-spec) wrote to stderr: %q", stderr.String())
	}
}

func TestCobraUsageSpecRepeatableArrayFlags(t *testing.T) {
	spec := generateUsageSpec((App{Version: "test"}).rootCommand())
	tests := []struct {
		command string
		flag    string
		arg     string
	}{
		{command: "add", flag: "-a --agent", arg: "<AGENT>"},
		{command: "add", flag: "-s --skill", arg: "<SKILL>"},
		{command: "list", flag: "-a --agent", arg: "<AGENT>"},
		{command: "remove", flag: "-a --agent", arg: "<AGENT>"},
		{command: "remove", flag: "-s --skill", arg: "<SKILL>"},
		{command: "sync", flag: "-a --agent", arg: "<AGENT>"},
	}
	for _, tt := range tests {
		t.Run(tt.command+"/"+strings.ReplaceAll(tt.flag, " ", "_"), func(t *testing.T) {
			block := usageSpecCommandBlock(t, spec, tt.command)
			flagLine := findUsageSpecFlagLine(t, block, tt.flag)
			if !strings.Contains(flagLine, "var=#true") {
				t.Fatalf("%s flag is not marked repeatable:\n%s", tt.flag, flagLine)
			}
			argLine := findUsageSpecArgLine(t, block, tt.arg)
			if !strings.Contains(argLine, "var=#true") {
				t.Fatalf("%s argument is not marked variadic:\n%s", tt.flag, argLine)
			}
		})
	}

	useBlock := usageSpecCommandBlock(t, spec, "use")
	useFlag := findUsageSpecFlagLine(t, useBlock, "-a --agent")
	if strings.Contains(useFlag, "var=#true") {
		t.Fatalf("single-agent use flag is repeatable in Usage spec:\n%s", useFlag)
	}
	useArg := findUsageSpecArgLine(t, useBlock, "<AGENT>")
	if strings.Contains(useArg, "var=#true") {
		t.Fatalf("single-agent use argument is variadic in Usage spec:\n%s", useArg)
	}

	findBlock := usageSpecCommandBlock(t, spec, "find")
	providerFlag := findUsageSpecFlagLine(t, findBlock, "--provider")
	if strings.Contains(providerFlag, "var=#true") {
		t.Fatalf("ordinary provider flag is repeatable in Usage spec:\n%s", providerFlag)
	}
	providerArg := findUsageSpecArgLine(t, findBlock, "<PROVIDER>")
	if strings.Contains(providerArg, "var=#true") {
		t.Fatalf("ordinary provider argument is variadic in Usage spec:\n%s", providerArg)
	}
	addBlock := usageSpecCommandBlock(t, spec, "add")
	yesFlag := findUsageSpecFlagLine(t, addBlock, "-y --yes")
	if strings.Contains(yesFlag, "var=#true") {
		t.Fatalf("ordinary boolean flag is repeatable in Usage spec:\n%s", yesFlag)
	}
}

func usageSpecCommandBlock(t *testing.T, spec, name string) string {
	t.Helper()
	lines := strings.Split(spec, "\n")
	start := -1
	for i, line := range lines {
		if !strings.HasPrefix(line, "cmd ") {
			continue
		}
		value, ok := usageSpecNodeValue(line, "cmd")
		if ok && value == name {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("Usage spec has no %q command:\n%s", name, spec)
	}
	depth := 0
	var block []string
	for _, line := range lines[start:] {
		block = append(block, line)
		trimmed := strings.TrimSpace(line)
		if strings.HasSuffix(trimmed, " {") {
			depth++
		} else if trimmed == "}" {
			depth--
			if depth == 0 {
				break
			}
		}
	}
	return strings.Join(block, "\n")
}

func findUsageSpecFlagLine(t *testing.T, block, name string) string {
	t.Helper()
	for _, line := range strings.Split(block, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "flag ") {
			continue
		}
		value, ok := usageSpecNodeValue(trimmed, "flag")
		if ok && value == name {
			return line
		}
	}
	t.Fatalf("Usage spec command block has no %q flag:\n%s", name, block)
	return ""
}

func findUsageSpecArgLine(t *testing.T, block, name string) string {
	t.Helper()
	for _, line := range strings.Split(block, "\n") {
		if value, ok := usageSpecNodeValue(line, "arg"); ok && value == name {
			return line
		}
	}
	t.Fatalf("Usage spec command block has no %q argument:\n%s", name, block)
	return ""
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
