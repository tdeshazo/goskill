package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tdeshazo/goskill/internal/skills"
)

func TestValidationMachineOutputFixtures(t *testing.T) {
	const longBodyLines = 497 // Four frontmatter lines plus these lines make 501.
	tests := []struct {
		name        string
		profile     skills.Profile
		files       []validationContractFile
		wantValid   bool
		wantSummary validationReportSummary
		wantCodes   []string
	}{
		{
			name:        "spec-error",
			profile:     skills.ProfileSpec,
			wantSummary: validationReportSummary{Skills: 2, Diagnostics: 1, Errors: 1},
			wantCodes:   []string{skills.RuleNameLowercase},
			files: []validationContractFile{
				{directory: "valid", filename: "SKILL.md", reportPath: "https://example.invalid/spec/valid/SKILL.md"},
				{directory: "bAd-skill", filename: "SKILL.md", reportPath: "https://example.invalid/spec/bAd-skill/SKILL.md"},
			},
		},
		{
			name:        "recommended-warning",
			profile:     skills.ProfileRecommended,
			wantValid:   true,
			wantSummary: validationReportSummary{Skills: 1, Diagnostics: 1, Warnings: 1},
			wantCodes:   []string{skills.RuleSkillLineCount},
			files: []validationContractFile{
				{directory: "long-skill", filename: "SKILL.md", bodyLines: longBodyLines, reportPath: "https://example.invalid/recommended/long-skill/SKILL.md"},
			},
		},
		{
			name:        "portable-mixed",
			profile:     skills.ProfilePortable,
			wantSummary: validationReportSummary{Skills: 1, Diagnostics: 2, Errors: 1, Warnings: 1},
			wantCodes:   []string{skills.RuleSkillFilename, skills.RuleSkillLineCount},
			files: []validationContractFile{
				{directory: "demo", filename: "skill.md", bodyLines: longBodyLines, reportPath: "https://example.invalid/portable/demo/skill.md"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var results []validationResult
			var counts validationCounts
			for _, file := range tt.files {
				path := writeValidationContractSkill(t, file)
				issues := skills.ValidateSkillMDWithProfile(path, tt.profile)
				for _, issue := range issues {
					counts.add(issue)
				}
				results = append(results, validationResult{Path: path, ReportPath: file.reportPath, Issues: issues})
			}
			report := newValidationReport(results, counts, tt.profile)
			codes := make([]string, len(report.Diagnostics))
			for i, diagnostic := range report.Diagnostics {
				codes[i] = diagnostic.Code
			}
			if report.Valid != tt.wantValid || report.Summary != tt.wantSummary || !slices.Equal(codes, tt.wantCodes) {
				t.Fatalf("scenario %s changed: valid=%v summary=%+v codes=%v", tt.name, report.Valid, report.Summary, codes)
			}
			formats := []struct {
				name   string
				suffix string
				render func(validationReport) (string, error)
			}{
				{name: "JSON", suffix: "report.json", render: renderValidationJSON},
				{name: "SARIF", suffix: "sarif.json", render: renderValidationSARIF},
			}
			for _, format := range formats {
				output, err := format.render(report)
				if err != nil {
					t.Fatalf("render %s: %v", format.name, err)
				}
				fixture := filepath.Join("..", "..", "testdata", "validation-output", "v"+validationReportSchemaVersion, tt.name+"."+format.suffix)
				if os.Getenv("UPDATE_VALIDATION_FIXTURES") == "1" {
					if err := os.MkdirAll(filepath.Dir(fixture), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(fixture, []byte(output), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				want, err := os.ReadFile(fixture)
				if err != nil {
					t.Fatal(err)
				}
				if output != string(want) {
					t.Fatalf("%s changed; review the contract and regenerate fixtures with UPDATE_VALIDATION_FIXTURES=1 go test ./internal/commands -run TestValidationMachineOutputFixtures", fixture)
				}
			}
		})
	}
}

type validationContractFile struct {
	directory  string
	filename   string
	bodyLines  int
	reportPath string
}

func writeValidationContractSkill(t *testing.T, file validationContractFile) string {
	t.Helper()
	directory := filepath.Join(t.TempDir(), file.directory)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + file.directory + "\ndescription: Description\n---\n" + strings.Repeat("content\n", file.bodyLines)
	path := filepath.Join(directory, file.filename)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestValidationReportPublishedSchemaVersion(t *testing.T) {
	path := filepath.Join("..", "..", "schemas", "validation-report.v"+validationReportSchemaVersion+".schema.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Dialect    string `json:"$schema"`
		Properties struct {
			SchemaVersion struct {
				Const string `json:"const"`
			} `json:"schema_version"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	if schema.Dialect != "https://json-schema.org/draft/2020-12/schema" || schema.Properties.SchemaVersion.Const != validationReportSchemaVersion {
		t.Fatalf("published schema %s does not match report version %q", path, validationReportSchemaVersion)
	}
}
