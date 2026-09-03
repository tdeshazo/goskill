package commands

import (
	"errors"
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"strings"

	"github.com/tdeshazo/goskill/internal/source"
)

type validationFormat string

const (
	validationFormatText  validationFormat = "text"
	validationFormatJSON  validationFormat = "json"
	validationFormatSARIF validationFormat = "sarif"
)

type validationOptions struct {
	Format  validationFormat
	Sources []string
	Help    bool
}

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

func parseValidate(args []string) (validationOptions, error) {
	opts := validationOptions{Format: validationFormatText}
	formatSet := false
	setFormat := func(value string) error {
		if formatSet {
			return errors.New("validation output format options are mutually exclusive")
		}
		switch validationFormat(strings.ToLower(strings.TrimSpace(value))) {
		case validationFormatText, validationFormatJSON, validationFormatSARIF:
			opts.Format = validationFormat(strings.ToLower(strings.TrimSpace(value)))
			formatSet = true
			return nil
		default:
			return fmt.Errorf("invalid validation format %q (want text, json, or sarif)", value)
		}
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--help", "-h":
			if len(args) != 1 {
				return validationOptions{}, errors.New("usage: goskill validate [--format text|json|sarif] <skills>")
			}
			opts.Help = true
			return opts, nil
		case "--format":
			if i+1 >= len(args) {
				return validationOptions{}, errors.New("--format requires a value")
			}
			i++
			if err := setFormat(args[i]); err != nil {
				return validationOptions{}, err
			}
		case "--json":
			if err := setFormat(string(validationFormatJSON)); err != nil {
				return validationOptions{}, err
			}
		case "--sarif":
			if err := setFormat(string(validationFormatSARIF)); err != nil {
				return validationOptions{}, err
			}
		default:
			if value, ok := strings.CutPrefix(arg, "--format="); ok {
				if err := setFormat(value); err != nil {
					return validationOptions{}, err
				}
				continue
			}
			if strings.HasPrefix(arg, "--") {
				return validationOptions{}, fmt.Errorf("unknown validate option %q", arg)
			}
			if strings.TrimSpace(arg) != "" {
				opts.Sources = append(opts.Sources, arg)
			}
		}
	}
	if len(opts.Sources) == 0 {
		return validationOptions{}, errors.New("usage: goskill validate [--format text|json|sarif] <skills>")
	}
	return opts, nil
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
