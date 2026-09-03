package commands

import (
	"encoding/json"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/tdeshazo/goskill/internal/skills"
)

const (
	validationReportSchemaVersion = "1"
	sarifVersion                  = "2.1.0"
	sarifSchemaURI                = "https://docs.oasis-open.org/sarif/sarif/v2.1.0/errata01/os/schemas/sarif-schema-2.1.0.json"
)

type validationReport struct {
	SchemaVersion string                  `json:"schema_version"`
	Profile       string                  `json:"profile"`
	Valid         bool                    `json:"valid"`
	Summary       validationReportSummary `json:"summary"`
	Specification validationSpecification `json:"specification"`
	Files         []validationFileReport  `json:"files"`
	Diagnostics   []skills.Diagnostic     `json:"diagnostics"`
}

type validationReportSummary struct {
	Skills      int `json:"skills"`
	Diagnostics int `json:"diagnostics"`
	Errors      int `json:"errors"`
	Warnings    int `json:"warnings"`
}

type validationCounts struct {
	Diagnostics int
	Errors      int
	Warnings    int
}

func (counts *validationCounts) add(diagnostic skills.Diagnostic) {
	counts.Diagnostics++
	if diagnostic.Severity == skills.SeverityWarning {
		counts.Warnings++
		return
	}
	counts.Errors++
}

type validationSpecification struct {
	VersioningStatus string `json:"versioning_status"`
	Revision         string `json:"revision"`
	CanonicalURL     string `json:"canonical_url"`
	PinnedSourceURL  string `json:"pinned_source_url"`
}

type validationFileReport struct {
	Path        string              `json:"path"`
	Valid       bool                `json:"valid"`
	Diagnostics []skills.Diagnostic `json:"diagnostics"`
}

func newValidationReport(results []validationResult, counts validationCounts, profile skills.Profile) validationReport {
	orderedResults := append([]validationResult(nil), results...)
	sort.SliceStable(orderedResults, func(i, j int) bool {
		return validationResultPath(orderedResults[i]) < validationResultPath(orderedResults[j])
	})
	report := validationReport{
		SchemaVersion: validationReportSchemaVersion,
		Profile:       string(profile),
		Valid:         counts.Errors == 0,
		Summary: validationReportSummary{
			Skills:      len(orderedResults),
			Diagnostics: counts.Diagnostics,
			Errors:      counts.Errors,
			Warnings:    counts.Warnings,
		},
		Specification: validationSpecification{
			VersioningStatus: skills.SpecVersioningStatus,
			Revision:         skills.SpecRevision,
			CanonicalURL:     skills.SpecCanonicalURL,
			PinnedSourceURL:  skills.SpecSourceURL,
		},
		Files:       make([]validationFileReport, 0, len(orderedResults)),
		Diagnostics: make([]skills.Diagnostic, 0, counts.Diagnostics),
	}
	for _, result := range orderedResults {
		reportPath := validationResultPath(result)
		diagnostics := make([]skills.Diagnostic, len(result.Issues))
		copy(diagnostics, result.Issues)
		for i := range diagnostics {
			diagnostics[i].Path = reportPath
		}
		skills.SortDiagnostics(diagnostics)
		report.Files = append(report.Files, validationFileReport{
			Path:        reportPath,
			Valid:       !hasValidationErrors(diagnostics),
			Diagnostics: diagnostics,
		})
		report.Diagnostics = append(report.Diagnostics, diagnostics...)
	}
	skills.SortDiagnostics(report.Diagnostics)
	return report
}

func hasValidationErrors(diagnostics []skills.Diagnostic) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == skills.SeverityError {
			return true
		}
	}
	return false
}

func validationResultPath(result validationResult) string {
	if result.ReportPath != "" {
		return result.ReportPath
	}
	return result.Path
}

func renderValidationJSON(report validationReport) (string, error) {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", err
	}
	return string(append(data, '\n')), nil
}

type sarifLog struct {
	Version string     `json:"version"`
	Schema  string     `json:"$schema"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool       sarifTool       `json:"tool"`
	Artifacts  []sarifArtifact `json:"artifacts,omitempty"`
	Results    []sarifResult   `json:"results"`
	Properties sarifProperties `json:"properties"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string          `json:"name"`
	InformationURI string          `json:"informationUri"`
	Rules          []sarifRule     `json:"rules"`
	Properties     sarifProperties `json:"properties"`
}

type sarifProperties struct {
	Profile                 string                  `json:"goskill_profile"`
	ValidationSchemaVersion string                  `json:"goskill_validation_schema_version"`
	Summary                 validationReportSummary `json:"goskill_summary"`
	Specification           validationSpecification `json:"goskill_specification"`
}

type sarifRule struct {
	ID                   string                    `json:"id"`
	Name                 string                    `json:"name"`
	ShortDescription     sarifMessage              `json:"shortDescription"`
	DefaultConfiguration sarifDefaultConfiguration `json:"defaultConfiguration"`
}

type sarifDefaultConfiguration struct {
	Level string `json:"level"`
}

type sarifResult struct {
	RuleID     string            `json:"ruleId"`
	Level      string            `json:"level"`
	Message    sarifMessage      `json:"message"`
	Locations  []sarifLocation   `json:"locations,omitempty"`
	Properties skills.Diagnostic `json:"properties"`
}

type sarifMessage struct {
	Text string `json:"text"`
}

type sarifArtifact struct {
	Location sarifArtifactLocation `json:"location"`
}

type sarifArtifactLocation struct {
	URI string `json:"uri"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
	Region           *sarifRegion          `json:"region,omitempty"`
}

type sarifRegion struct {
	StartLine   int `json:"startLine"`
	StartColumn int `json:"startColumn,omitempty"`
}

func renderValidationSARIF(report validationReport) (string, error) {
	profile := skills.Profile(report.Profile)
	rules := make([]sarifRule, 0, len(skills.RulesForProfile(profile)))
	for _, rule := range skills.RulesForProfile(profile) {
		rules = append(rules, sarifRule{
			ID:                   rule.Code,
			Name:                 rule.Code,
			ShortDescription:     sarifMessage{Text: rule.Summary},
			DefaultConfiguration: sarifDefaultConfiguration{Level: sarifLevel(rule.Severity)},
		})
	}

	artifacts := sarifArtifacts(report.Files)
	results := make([]sarifResult, 0, len(report.Diagnostics))
	for _, diagnostic := range report.Diagnostics {
		result := sarifResult{
			RuleID:     diagnostic.Code,
			Level:      sarifLevel(diagnostic.Severity),
			Message:    sarifMessage{Text: diagnostic.Message},
			Properties: diagnostic,
		}
		if diagnostic.Path != "" {
			location := sarifPhysicalLocation{
				ArtifactLocation: sarifArtifactLocation{URI: artifactURI(diagnostic.Path)},
			}
			if diagnostic.Line > 0 {
				region := sarifRegion{StartLine: diagnostic.Line}
				if diagnostic.Column > 0 {
					region.StartColumn = diagnostic.Column
				}
				location.Region = &region
			}
			result.Locations = []sarifLocation{{PhysicalLocation: location}}
		}
		results = append(results, result)
	}

	properties := sarifProperties{
		Profile:                 report.Profile,
		ValidationSchemaVersion: report.SchemaVersion,
		Summary:                 report.Summary,
		Specification:           report.Specification,
	}
	log := sarifLog{
		Version: sarifVersion,
		Schema:  sarifSchemaURI,
		Runs: []sarifRun{{
			Tool: sarifTool{Driver: sarifDriver{
				Name:           "goskill",
				InformationURI: "https://github.com/tdeshazo/goskill",
				Rules:          rules,
				Properties:     properties,
			}},
			Artifacts:  artifacts,
			Results:    results,
			Properties: properties,
		}},
	}
	data, err := json.MarshalIndent(log, "", "  ")
	if err != nil {
		return "", err
	}
	return string(append(data, '\n')), nil
}

func sarifArtifacts(files []validationFileReport) []sarifArtifact {
	uris := make([]string, 0, len(files))
	seen := map[string]bool{}
	for _, file := range files {
		uri := artifactURI(file.Path)
		if !seen[uri] {
			seen[uri] = true
			uris = append(uris, uri)
		}
	}
	sort.Strings(uris)
	artifacts := make([]sarifArtifact, 0, len(uris))
	for _, uri := range uris {
		artifacts = append(artifacts, sarifArtifact{Location: sarifArtifactLocation{URI: uri}})
	}
	return artifacts
}

func artifactURI(path string) string {
	if strings.HasPrefix(path, `\\`) || strings.HasPrefix(path, "//") {
		clean := strings.TrimLeft(strings.ReplaceAll(path, `\`, "/"), "/")
		host, rest, found := strings.Cut(clean, "/")
		if found && host != "" {
			return (&url.URL{Scheme: "file", Host: host, Path: "/" + rest}).String()
		}
	}
	if regexp.MustCompile(`^[A-Za-z]:[\\/]`).MatchString(path) {
		clean := "/" + strings.ReplaceAll(path, `\`, "/")
		return (&url.URL{Scheme: "file", Path: clean}).String()
	}
	if parsedURL, err := url.Parse(path); err == nil && parsedURL.Scheme != "" && (parsedURL.Host != "" || parsedURL.Scheme == "file") {
		return parsedURL.String()
	}
	if absolute, err := filepath.Abs(path); err == nil {
		path = absolute
	}
	clean := filepath.ToSlash(path)
	if !strings.HasPrefix(clean, "/") {
		clean = "/" + clean
	}
	return (&url.URL{Scheme: "file", Path: clean}).String()
}

func sarifLevel(severity skills.Severity) string {
	if severity == skills.SeverityError {
		return "error"
	}
	return "warning"
}
