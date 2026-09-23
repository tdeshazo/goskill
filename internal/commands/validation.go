package commands

import (
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"strings"

	"github.com/tdeshazo/goskill/internal/skills"
	"github.com/tdeshazo/goskill/internal/source"
)

type validationFormat string

const (
	validationFormatText  validationFormat = "text"
	validationFormatJSON  validationFormat = "json"
	validationFormatSARIF validationFormat = "sarif"
)

type validationOptions struct {
	Format      validationFormat
	Profile     skills.Profile
	Sources     []string
	VersionInfo bool
}

const validateUsage = "usage: goskill validate [--profile spec|recommended|portable] [--format text|json|sarif] <skills>\n       goskill validate [--profile spec|recommended|portable] --version-info"

type validationFile struct {
	Path       string
	ReportPath string
}

// validationMachineOutputError preserves a nonzero conformance exit status
// without causing the executable to append a human error block after JSON or
// SARIF output.
type validationMachineOutputError struct {
	issues int
}

func (e validationMachineOutputError) Error() string {
	return fmt.Sprintf("validation failed: %d issue(s)", e.issues)
}

func (e validationMachineOutputError) ExitCode() int {
	return 1
}

func validationFilesForReport(root, sourceID string, files []string) []validationFile {
	result := make([]validationFile, 0, len(files))
	for _, file := range files {
		relative, err := filepath.Rel(root, file)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			relative = filepath.Base(file)
		}
		if relative == "." {
			result = append(result, validationFile{Path: file, ReportPath: sourceID})
			continue
		}
		result = append(result, validationFile{
			Path:       file,
			ReportPath: joinValidationReportPath(sourceID, filepath.ToSlash(relative)),
		})
	}
	return result
}

func joinValidationReportPath(sourceID, relative string) string {
	parsedURL, err := url.Parse(sourceID)
	isFileURL := err == nil && parsedURL.Scheme == "file"
	isHostedURL := err == nil && parsedURL.Scheme != "" && parsedURL.Host != ""
	if !isFileURL && !isHostedURL {
		return path.Join(sourceID, relative)
	}
	parsedURL.Path = path.Join(parsedURL.Path, relative)
	return parsedURL.String()
}

func validationSourceID(parsed source.Parsed) string {
	raw := strings.TrimSpace(parsed.URL)
	if parsedURL, err := url.Parse(raw); err == nil && (parsedURL.Host != "" || parsedURL.Scheme == "file") {
		parsedURL.User = nil
		parsedURL.Fragment = ""
		parsedURL.Path = strings.TrimSuffix(parsedURL.Path, ".git")
		parsedURL.RawQuery = validationRefQuery(parsed.Ref)
		return parsedURL.String()
	}
	if at := strings.LastIndex(raw, "@"); at >= 0 {
		raw = raw[at+1:]
	}
	if host, repository, ok := strings.Cut(raw, ":"); ok {
		parsedURL := &url.URL{
			Scheme:   "ssh",
			Host:     host,
			Path:     "/" + strings.TrimSuffix(strings.Trim(repository, "/"), ".git"),
			RawQuery: validationRefQuery(parsed.Ref),
		}
		return parsedURL.String()
	}
	identity := strings.TrimSuffix(strings.Trim(raw, "/"), ".git")
	if parsed.Ref == "" {
		return identity
	}
	return identity + "?" + validationRefQuery(parsed.Ref)
}

func validationRefQuery(ref string) string {
	if ref == "" {
		return ""
	}
	return url.Values{"ref": []string{ref}}.Encode()
}
