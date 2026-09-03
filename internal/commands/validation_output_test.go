package commands

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tdeshazo/goskill/internal/skills"
	"github.com/tdeshazo/goskill/internal/source"
	"github.com/tdeshazo/goskill/internal/terminal"
)

func TestParseValidateFormats(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		format  validationFormat
		profile skills.Profile
		wantErr string
	}{
		{name: "text default", args: []string{"skill"}, format: validationFormatText, profile: skills.ProfileSpec},
		{name: "json alias", args: []string{"--json", "skill"}, format: validationFormatJSON, profile: skills.ProfileSpec},
		{name: "sarif alias", args: []string{"--sarif", "skill"}, format: validationFormatSARIF, profile: skills.ProfileSpec},
		{name: "format equals", args: []string{"--format=sarif", "skill"}, format: validationFormatSARIF, profile: skills.ProfileSpec},
		{name: "profile before format", args: []string{"--profile", "recommended", "--json", "skill"}, format: validationFormatJSON, profile: skills.ProfileRecommended},
		{name: "profile equals after format", args: []string{"--sarif", "--profile=portable", "skill"}, format: validationFormatSARIF, profile: skills.ProfilePortable},
		{name: "version info default profile", args: []string{"--version-info"}, format: validationFormatText, profile: skills.ProfileSpec},
		{name: "version info profile before", args: []string{"--profile", "recommended", "--version-info"}, format: validationFormatText, profile: skills.ProfileRecommended},
		{name: "version info profile after", args: []string{"--version-info", "--profile=portable"}, format: validationFormatText, profile: skills.ProfilePortable},
		{name: "mutually exclusive", args: []string{"--json", "--sarif", "skill"}, wantErr: "mutually exclusive"},
		{name: "format and alias", args: []string{"--format", "json", "--json", "skill"}, wantErr: "mutually exclusive"},
		{name: "invalid format", args: []string{"--format", "xml", "skill"}, wantErr: "invalid validation format"},
		{name: "missing profile", args: []string{"--profile"}, wantErr: "--profile requires a value"},
		{name: "invalid profile", args: []string{"--profile", "lint", "skill"}, wantErr: "invalid validation profile"},
		{name: "duplicate profile", args: []string{"--profile", "spec", "--profile=portable", "skill"}, wantErr: "mutually exclusive"},
		{name: "version info with source", args: []string{"--version-info", "skill"}, wantErr: "does not accept skill sources"},
		{name: "version info with JSON", args: []string{"--version-info", "--json"}, wantErr: "cannot be combined with output formats"},
		{name: "version info with text format", args: []string{"--format=text", "--version-info"}, wantErr: "cannot be combined with output formats"},
		{name: "unknown option", args: []string{"--unknown", "skill"}, wantErr: "unknown validate option"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			opts, err := parseValidate(test.args)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("parseValidate(%v) error = %v", test.args, err)
				}
				return
			}
			if err != nil || opts.Format != test.format || opts.Profile != test.profile {
				t.Fatalf("parseValidate(%v) = %#v, %v", test.args, opts, err)
			}
			if opts.VersionInfo && len(opts.Sources) != 0 {
				t.Fatalf("version info unexpectedly has sources: %#v", opts)
			}
			if !opts.VersionInfo && len(opts.Sources) != 1 {
				t.Fatalf("validation sources = %#v", opts.Sources)
			}
		})
	}
}

func TestValidateVersionInfoIsOfflineAndScriptFriendly(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		profile skills.Profile
	}{
		{name: "default", args: []string{"--version-info"}, profile: skills.ProfileSpec},
		{name: "explicit spec", args: []string{"--profile", "spec", "--version-info"}, profile: skills.ProfileSpec},
		{name: "recommended before", args: []string{"--profile=recommended", "--version-info"}, profile: skills.ProfileRecommended},
		{name: "portable after", args: []string{"--version-info", "--profile", "portable"}, profile: skills.ProfilePortable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var out bytes.Buffer
			app := App{Version: "9.8.7", Stdout: &out, Stderr: &bytes.Buffer{}, Cwd: t.TempDir()}
			if err := app.Run(append([]string{"validate"}, test.args...)); err != nil {
				t.Fatal(err)
			}
			want := "validator: goskill 9.8.7\n" +
				"spec: " + skills.SpecReference() + "\n" +
				"profile: " + string(test.profile) + "\n"
			if got := out.String(); got != want {
				t.Fatalf("version info = %q, want %q", got, want)
			}
			if out.String() != terminal.StripEscapes(out.String()) {
				t.Fatalf("version info contains ANSI: %q", out.String())
			}
		})
	}
}

func TestValidateVersionInfoRejectsSourcesAndMachineFormatsWithoutOutput(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "source is never resolved", args: []string{"--version-info", "https://example.invalid/skills"}},
		{name: "JSON alias", args: []string{"--version-info", "--json"}},
		{name: "SARIF alias", args: []string{"--sarif", "--version-info"}},
		{name: "format option", args: []string{"--version-info", "--format", "text"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var out bytes.Buffer
			app := App{Version: "test", Stdout: &out, Stderr: &bytes.Buffer{}, Cwd: t.TempDir()}
			err := app.Run(append([]string{"validate"}, test.args...))
			if err == nil || !strings.Contains(err.Error(), "usage: goskill validate") {
				t.Fatalf("error = %v", err)
			}
			if out.Len() != 0 {
				t.Fatalf("invalid version-info invocation emitted output: %q", out.String())
			}
		})
	}
}

func TestValidateJSONOutputIsCompleteAndMachineSafe(t *testing.T) {
	root := t.TempDir()
	dir := makeSkill(t, root, "bad-skill", "Description")
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: Bad Skill\ndescription: Description\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	app := App{Version: "test", Stdout: &out, Stderr: &bytes.Buffer{}, Cwd: root}
	err := app.Run([]string{"validate", "--json", "bad-skill"})
	if code, ok := ExitCode(err); !ok || code != 1 {
		t.Fatalf("ExitCode(%v) = %d, %v", err, code, ok)
	}
	if out.String() != terminal.StripEscapes(out.String()) {
		t.Fatalf("JSON contains ANSI: %q", out.String())
	}
	var report validationReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out.String())
	}
	if report.Valid || report.SchemaVersion != validationReportSchemaVersion || report.Summary.Errors == 0 {
		t.Fatalf("report = %#v", report)
	}
	if report.Specification.Revision != skills.SpecRevision || len(report.Diagnostics) == 0 || report.Diagnostics[0].Code != skills.RuleNameLowercase {
		t.Fatalf("report = %#v", report)
	}
}

func TestValidateJSONValidResult(t *testing.T) {
	root := t.TempDir()
	makeSkill(t, root, "demo", "Description")
	var out bytes.Buffer
	app := App{Version: "test", Stdout: &out, Stderr: &bytes.Buffer{}, Cwd: root}
	if err := app.Run([]string{"validate", "--format", "json", "demo"}); err != nil {
		t.Fatal(err)
	}
	var report validationReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if !report.Valid || report.Summary.Skills != 1 || report.Summary.Diagnostics != 0 {
		t.Fatalf("report = %#v", report)
	}
	if report.Diagnostics == nil || len(report.Files) != 1 || report.Files[0].Diagnostics == nil {
		t.Fatalf("valid diagnostics must serialize as arrays: %s", out.String())
	}
}

func TestValidateProfilesPreserveExitAndTextBehavior(t *testing.T) {
	root := t.TempDir()
	longSkill := makeSkill(t, root, "long-skill", "Description")
	longContent := "---\nname: long-skill\ndescription: Description\n---\n" + strings.Repeat("content\n", 497)
	if err := os.WriteFile(filepath.Join(longSkill, "SKILL.md"), []byte(longContent), 0o644); err != nil {
		t.Fatal(err)
	}

	run := func(args ...string) (string, error) {
		t.Helper()
		var out bytes.Buffer
		app := App{Version: "test", Stdout: &out, Stderr: &bytes.Buffer{}, Cwd: root}
		err := app.Run(append([]string{"validate"}, args...))
		return out.String(), err
	}

	defaultText, err := run("long-skill")
	if err != nil {
		t.Fatal(err)
	}
	specText, err := run("--profile", "spec", "long-skill")
	if err != nil {
		t.Fatal(err)
	}
	if defaultText != specText {
		t.Fatalf("default output differs from spec profile:\ndefault: %s\nspec: %s", defaultText, specText)
	}

	jsonOutput, err := run("--profile", "recommended", "--json", "long-skill")
	if err != nil {
		t.Fatalf("warning-only recommended profile returned error: %v", err)
	}
	if jsonOutput != terminal.StripEscapes(jsonOutput) {
		t.Fatalf("JSON contains ANSI: %q", jsonOutput)
	}
	var report validationReport
	if err := json.Unmarshal([]byte(jsonOutput), &report); err != nil {
		t.Fatal(err)
	}
	if !report.Valid || len(report.Files) != 1 || !report.Files[0].Valid {
		t.Fatalf("recommended report = %#v", report)
	}
	if report.Profile != string(skills.ProfileRecommended) || report.Summary.Errors != 0 || report.Summary.Warnings != 1 {
		t.Fatalf("recommended report = %#v", report)
	}
	if len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != skills.RuleSkillLineCount || report.Diagnostics[0].Severity != skills.SeverityWarning {
		t.Fatalf("recommended report = %#v", report)
	}
}

func TestValidatePortableSARIFReportsLowercaseFilename(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skill.md"), []byte("---\nname: demo\ndescription: Description\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	app := App{Version: "test", Stdout: &out, Stderr: &bytes.Buffer{}, Cwd: root}
	err := app.Run([]string{"validate", "--profile=portable", "--sarif", "demo"})
	if code, ok := ExitCode(err); !ok || code != 1 {
		t.Fatalf("ExitCode(%v) = %d, %v", err, code, ok)
	}
	if out.String() != terminal.StripEscapes(out.String()) {
		t.Fatalf("SARIF contains ANSI: %q", out.String())
	}
	var log sarifLog
	if err := json.Unmarshal(out.Bytes(), &log); err != nil {
		t.Fatal(err)
	}
	run := log.Runs[0]
	if run.Properties.Profile != string(skills.ProfilePortable) || run.Properties.Summary.Errors != 1 || run.Properties.Summary.Warnings != 0 {
		t.Fatalf("SARIF properties = %#v", run.Properties)
	}
	if run.Properties.Specification.Revision != skills.SpecRevision {
		t.Fatalf("SARIF properties = %#v", run.Properties)
	}
	if len(run.Results) != 1 || run.Results[0].RuleID != skills.RuleSkillFilename || run.Results[0].Level != "error" {
		t.Fatalf("SARIF results = %#v", run.Results)
	}
	if filepath.Base(run.Results[0].Properties.Path) != "skill.md" {
		t.Fatalf("SARIF diagnostic path lost actual filename casing: %#v", run.Results[0].Properties)
	}
	foundGuidance, foundPortable := false, false
	for _, rule := range run.Tool.Driver.Rules {
		switch rule.ID {
		case skills.RuleSkillLineCount:
			foundGuidance = rule.DefaultConfiguration.Level == "warning"
		case skills.RuleSkillFilename:
			foundPortable = rule.DefaultConfiguration.Level == "error"
		}
	}
	if !foundGuidance || !foundPortable {
		t.Fatalf("SARIF rules = %#v", run.Tool.Driver.Rules)
	}
}

func TestValidatePortableJSONReportsLowercaseFilename(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skill.md"), []byte("---\nname: demo\ndescription: Description\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	app := App{Version: "test", Stdout: &out, Stderr: &bytes.Buffer{}, Cwd: root}
	err := app.Run([]string{"validate", "--profile=portable", "--json", "demo"})
	if code, ok := ExitCode(err); !ok || code != 1 {
		t.Fatalf("ExitCode(%v) = %d, %v", err, code, ok)
	}
	var report validationReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Files) != 1 || filepath.Base(report.Files[0].Path) != "skill.md" {
		t.Fatalf("JSON file path lost actual filename casing: %#v", report.Files)
	}
	if len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != skills.RuleSkillFilename || filepath.Base(report.Diagnostics[0].Path) != "skill.md" {
		t.Fatalf("JSON diagnostics = %#v", report.Diagnostics)
	}
}

func TestValidatePortableDirectFileCanonicalizesCaseInsensitivePath(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	actualPath := filepath.Join(dir, "skill.md")
	if err := os.WriteFile(actualPath, []byte("---\nname: demo\ndescription: Description\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	syntheticPath := filepath.Join(dir, "SKILL.md")
	if _, err := os.Stat(syntheticPath); err != nil {
		t.Skip("filesystem is case-sensitive")
	}

	var out bytes.Buffer
	app := App{Version: "test", Stdout: &out, Stderr: &bytes.Buffer{}, Cwd: root}
	err := app.Run([]string{"validate", "--profile=portable", "--json", syntheticPath})
	if code, ok := ExitCode(err); !ok || code != 1 {
		t.Fatalf("ExitCode(%v) = %d, %v", err, code, ok)
	}
	var report validationReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Files) != 1 || report.Files[0].Path != actualPath {
		t.Fatalf("direct file report path = %#v, want %q", report.Files, actualPath)
	}
	if len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != skills.RuleSkillFilename || report.Diagnostics[0].Path != actualPath {
		t.Fatalf("direct file diagnostics = %#v", report.Diagnostics)
	}
}

func TestValidateSARIFValidResult(t *testing.T) {
	root := t.TempDir()
	makeSkill(t, root, "demo", "Description")
	var out bytes.Buffer
	app := App{Version: "test", Stdout: &out, Stderr: &bytes.Buffer{}, Cwd: root}
	if err := app.Run([]string{"validate", "--format", "sarif", "demo"}); err != nil {
		t.Fatal(err)
	}
	var log sarifLog
	if err := json.Unmarshal(out.Bytes(), &log); err != nil {
		t.Fatal(err)
	}
	if len(log.Runs) != 1 || len(log.Runs[0].Results) != 0 || len(log.Runs[0].Artifacts) != 1 {
		t.Fatalf("SARIF = %#v", log)
	}
}

func TestValidateSARIFContainsRuleCatalogAndArtifactLocations(t *testing.T) {
	root := t.TempDir()
	dir := makeSkill(t, root, "bad-skill", "Description")
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: Bad Skill\ndescription: Description\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	app := App{Version: "test", Stdout: &out, Stderr: &bytes.Buffer{}, Cwd: root}
	err := app.Run([]string{"validate", "--sarif", "bad-skill"})
	if code, ok := ExitCode(err); !ok || code != 1 {
		t.Fatalf("ExitCode(%v) = %d, %v", err, code, ok)
	}
	if out.String() != terminal.StripEscapes(out.String()) {
		t.Fatalf("SARIF contains ANSI: %q", out.String())
	}
	var log sarifLog
	if err := json.Unmarshal(out.Bytes(), &log); err != nil {
		t.Fatalf("invalid SARIF: %v\n%s", err, out.String())
	}
	if log.Version != sarifVersion || log.Schema != sarifSchemaURI || len(log.Runs) != 1 {
		t.Fatalf("SARIF header = %#v", log)
	}
	run := log.Runs[0]
	if run.Tool.Driver.Name != "goskill" || len(run.Tool.Driver.Rules) != len(skills.RulesForProfile(skills.ProfileSpec)) || run.Properties.Specification.Revision != skills.SpecRevision {
		t.Fatalf("SARIF run = %#v", run)
	}
	for i, rule := range skills.RulesForProfile(skills.ProfileSpec) {
		if run.Tool.Driver.Rules[i].ID != rule.Code || run.Tool.Driver.Rules[i].DefaultConfiguration.Level != sarifLevel(rule.Severity) {
			t.Fatalf("SARIF rules = %#v", run.Tool.Driver.Rules)
		}
	}
	if len(run.Results) == 0 || run.Results[0].RuleID != skills.RuleNameLowercase || run.Results[0].Level != "error" || run.Results[0].Message.Text == "" || len(run.Results[0].Locations) != 1 {
		t.Fatalf("SARIF results = %#v", run.Results)
	}
	physical := run.Results[0].Locations[0].PhysicalLocation
	if physical.ArtifactLocation.URI == "" || physical.Region != nil {
		t.Fatalf("SARIF physical location = %#v", physical)
	}
	if len(run.Artifacts) != 1 || !strings.HasPrefix(run.Artifacts[0].Location.URI, "file://") {
		t.Fatalf("SARIF artifacts = %#v", run.Artifacts)
	}
}

func TestValidationSerializationOrderingAndKnownLocations(t *testing.T) {
	report := newValidationReport([]validationResult{
		{Path: "z/SKILL.md", Issues: []skills.Diagnostic{{Code: skills.RuleNameType, Severity: skills.SeverityError, Message: "z", Path: "z/SKILL.md"}}},
		{Path: "a/SKILL.md", Issues: []skills.Diagnostic{{Code: skills.RuleDescriptionType, Severity: skills.SeverityError, Message: "a", Path: "a/SKILL.md", Line: 3, Column: 2}}},
	}, validationCounts{Diagnostics: 2, Errors: 2}, skills.ProfileSpec)
	if report.Files[0].Path != "a/SKILL.md" || report.Diagnostics[0].Path != "a/SKILL.md" {
		t.Fatalf("report ordering = %#v", report)
	}
	encoded, err := renderValidationSARIF(report)
	if err != nil {
		t.Fatal(err)
	}
	var log sarifLog
	if err := json.Unmarshal([]byte(encoded), &log); err != nil {
		t.Fatal(err)
	}
	first := log.Runs[0].Results[0].Locations
	second := log.Runs[0].Results[1].Locations
	if len(first) != 1 || first[0].PhysicalLocation.Region == nil || first[0].PhysicalLocation.Region.StartLine != 3 || len(second) != 1 || second[0].PhysicalLocation.Region != nil {
		t.Fatalf("locations = %#v", log.Runs[0].Results)
	}
}

func TestRemoteValidationReportPathsAreStable(t *testing.T) {
	sourceID := "https://github.com/acme/skills"
	reportForRoot := func(root string) (string, string) {
		file := filepath.Join(root, "skills", "demo", "SKILL.md")
		inputs := validationFilesForReport(root, sourceID, []string{file})
		report := newValidationReport([]validationResult{{
			Path:       inputs[0].Path,
			ReportPath: inputs[0].ReportPath,
			Issues: []skills.Diagnostic{{
				Code:     skills.RuleNameType,
				Severity: skills.SeverityError,
				Message:  "invalid name",
				Path:     file,
			}},
		}}, validationCounts{Diagnostics: 1, Errors: 1}, skills.ProfileSpec)
		jsonOutput, err := renderValidationJSON(report)
		if err != nil {
			t.Fatal(err)
		}
		sarifOutput, err := renderValidationSARIF(report)
		if err != nil {
			t.Fatal(err)
		}
		return jsonOutput, sarifOutput
	}

	firstRoot := filepath.Join(t.TempDir(), "skills-123")
	secondRoot := filepath.Join(t.TempDir(), "skills-987")
	firstJSON, firstSARIF := reportForRoot(firstRoot)
	secondJSON, secondSARIF := reportForRoot(secondRoot)
	if firstJSON != secondJSON || firstSARIF != secondSARIF {
		t.Fatalf("remote reports differ across clone roots:\nfirst JSON: %s\nsecond JSON: %s\nfirst SARIF: %s\nsecond SARIF: %s", firstJSON, secondJSON, firstSARIF, secondSARIF)
	}
	wantPath := "https://github.com/acme/skills/skills/demo/SKILL.md"
	combined := firstJSON + firstSARIF
	if !strings.Contains(combined, wantPath) || strings.Contains(combined, firstRoot) || strings.Contains(combined, secondRoot) {
		t.Fatalf("remote report path is not stable: %s", combined)
	}
}

func TestValidationSourceID(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		ref  string
		want string
	}{
		{name: "GitHub HTTPS", raw: "https://github.com/acme/skills.git", want: "https://github.com/acme/skills"},
		{name: "authenticated HTTPS with ref", raw: "https://token@example.com/team/skills.git?token=secret#fragment", ref: "feature/one", want: "https://example.com/team/skills?ref=feature%2Fone"},
		{name: "SCP SSH with ref", raw: "git@example.com:team/skills.git", ref: "release 1", want: "ssh://example.com/team/skills?ref=release+1"},
		{name: "hostless file URL", raw: "file:///tmp/team/skills.git", ref: "main", want: "file:///tmp/team/skills?ref=main"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := validationSourceID(source.Parsed{URL: test.raw, Ref: test.ref}); got != test.want {
				t.Fatalf("validationSourceID(%q) = %q, want %q", test.raw, got, test.want)
			}
		})
	}
}

func TestValidationReportPathsDistinguishGitRefs(t *testing.T) {
	repository := "https://github.com/acme/skills.git"
	fileForRef := func(root, ref string) validationFile {
		files := validationFilesForReport(
			root,
			validationSourceID(source.Parsed{URL: repository, Ref: ref}),
			[]string{filepath.Join(root, "demo", "SKILL.md")},
		)
		return files[0]
	}
	mainFile := fileForRef(filepath.Join(t.TempDir(), "clone-main"), "main")
	releaseFile := fileForRef(filepath.Join(t.TempDir(), "clone-release"), "release/v1")
	if mainFile.ReportPath == releaseFile.ReportPath {
		t.Fatalf("distinct refs collided at %q", mainFile.ReportPath)
	}
	if mainFile.ReportPath != "https://github.com/acme/skills/demo/SKILL.md?ref=main" || releaseFile.ReportPath != "https://github.com/acme/skills/demo/SKILL.md?ref=release%2Fv1" {
		t.Fatalf("ref report paths = %q, %q", mainFile.ReportPath, releaseFile.ReportPath)
	}

	report := newValidationReport([]validationResult{
		{Path: releaseFile.Path, ReportPath: releaseFile.ReportPath},
		{Path: mainFile.Path, ReportPath: mainFile.ReportPath},
	}, validationCounts{}, skills.ProfileSpec)
	if len(report.Files) != 2 || report.Files[0].Path != mainFile.ReportPath || report.Files[1].Path != releaseFile.ReportPath {
		t.Fatalf("ref-aware report ordering = %#v", report.Files)
	}
}

func TestValidateHostlessFileGitRemote(t *testing.T) {
	repository := filepath.Join(t.TempDir(), "catalog")
	if err := os.MkdirAll(filepath.Join(repository, "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, filepath.Join(repository, "demo"), "demo", "File remote skill")
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repository
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
	runGit("init", "--quiet")
	runGit("add", "demo/SKILL.md")
	runGit("-c", "user.name=goskill test", "-c", "user.email=goskill@example.invalid", "commit", "--quiet", "-m", "fixture")

	sourceURL := artifactURI(repository)
	parsed, err := source.Parse(sourceURL)
	if err != nil || parsed.Type != source.Git {
		t.Fatalf("source.Parse(%q) = %#v, %v", sourceURL, parsed, err)
	}
	wantPath := joinValidationReportPath(validationSourceID(parsed), "demo/SKILL.md")
	if !strings.HasPrefix(wantPath, "file:///") {
		t.Fatalf("file report path = %q", wantPath)
	}

	var out bytes.Buffer
	app := App{Version: "test", Stdout: &out, Stderr: &bytes.Buffer{}, Cwd: t.TempDir()}
	if err := app.Run([]string{"validate", "--json", sourceURL}); err != nil {
		t.Fatal(err)
	}
	var report validationReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if !report.Valid || len(report.Files) != 1 || report.Files[0].Path != wantPath || report.Diagnostics == nil {
		t.Fatalf("file remote report = %#v\n%s", report, out.String())
	}
}

func TestArtifactURI(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "drive letter", path: `C:\Users\Ada Lovelace\skill#1\SKILL.md`, want: "file:///C:/Users/Ada%20Lovelace/skill%231/SKILL.md"},
		{name: "UNC", path: `\\server\shared skills\skill?one\SKILL.md`, want: "file://server/shared%20skills/skill%3Fone/SKILL.md"},
		{name: "forward slash UNC", path: `//server/share/skill%one/SKILL.md`, want: "file://server/share/skill%25one/SKILL.md"},
		{name: "hostless file URL", path: "file:///tmp/skill%20one/SKILL.md", want: "file:///tmp/skill%20one/SKILL.md"},
		{name: "POSIX reserved characters", path: "/tmp/skill #1?.md", want: "file:///tmp/skill%20%231%3F.md"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := artifactURI(test.path); got != test.want {
				t.Fatalf("artifactURI(%q) = %q, want %q", test.path, got, test.want)
			}
		})
	}
}

func TestValidateHelpDocumentsMachineFormats(t *testing.T) {
	var out bytes.Buffer
	app := App{Version: "test", Stdout: &out, Cwd: t.TempDir()}
	if err := app.Run([]string{"validate", "--help"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"goskill validate", "--profile", "--version-info", "--format", "--json", "--sarif"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("validate help missing %q:\n%s", want, out.String())
		}
	}
}

func TestValidateOperationalFailureDoesNotEmitPartialMachineOutput(t *testing.T) {
	var out bytes.Buffer
	app := App{Version: "test", Stdout: &out, Cwd: t.TempDir()}
	err := app.Run([]string{"validate", "--json"})
	if err == nil || strings.Contains(err.Error(), "validation failed") {
		t.Fatalf("operational error = %v", err)
	}
	if _, ok := ExitCode(err); ok {
		t.Fatalf("operational error unexpectedly uses the validation exit path: %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("operational failure emitted partial output: %q", out.String())
	}
}
