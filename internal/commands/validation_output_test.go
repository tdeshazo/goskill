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
		wantErr string
	}{
		{name: "text default", args: []string{"skill"}, format: validationFormatText},
		{name: "json alias", args: []string{"--json", "skill"}, format: validationFormatJSON},
		{name: "sarif alias", args: []string{"--sarif", "skill"}, format: validationFormatSARIF},
		{name: "format equals", args: []string{"--format=sarif", "skill"}, format: validationFormatSARIF},
		{name: "mutually exclusive", args: []string{"--json", "--sarif", "skill"}, wantErr: "mutually exclusive"},
		{name: "format and alias", args: []string{"--format", "json", "--json", "skill"}, wantErr: "mutually exclusive"},
		{name: "invalid format", args: []string{"--format", "xml", "skill"}, wantErr: "invalid validation format"},
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
			if err != nil || opts.Format != test.format || len(opts.Sources) != 1 {
				t.Fatalf("parseValidate(%v) = %#v, %v", test.args, opts, err)
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
	if run.Tool.Driver.Name != "goskill" || len(run.Tool.Driver.Rules) != len(skills.Rules()) || run.Properties.Revision != skills.SpecRevision {
		t.Fatalf("SARIF run = %#v", run)
	}
	for i, rule := range skills.Rules() {
		if run.Tool.Driver.Rules[i].ID != rule.Code || run.Tool.Driver.Rules[i].DefaultConfiguration.Level != "error" {
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
	}, 2)
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
		}}, 1)
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
	}, 0)
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
	for _, want := range []string{"goskill validate", "--format", "--json", "--sarif"} {
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
