package search

import (
	"encoding/json"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Deduplicate merges only results with a stable, source-based identity. It
// never uses a display name as an identity, because unrelated repositories can
// legitimately publish skills with the same name.
func Deduplicate(results []SearchResult) []SearchResult {
	groups := map[string][]SearchResult{}
	unique := make([]SearchResult, 0, len(results))
	for _, result := range results {
		identity := canonicalIdentity(result)
		if identity == "" {
			unique = append(unique, mergeResults([]SearchResult{result}))
			continue
		}
		groups[identity] = append(groups[identity], result)
	}

	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	merged := make([]SearchResult, 0, len(results))
	for _, key := range keys {
		merged = append(merged, mergeResults(groups[key]))
	}
	merged = append(merged, unique...)
	sort.SliceStable(merged, func(i, j int) bool {
		left := canonicalIdentity(merged[i])
		right := canonicalIdentity(merged[j])
		if left == right {
			return stableResultKey(merged[i]) < stableResultKey(merged[j])
		}
		if left == "" {
			return false
		}
		if right == "" {
			return true
		}
		return left < right
	})
	return merged
}

func canonicalIdentity(result SearchResult) string {
	repo, ref, sourcePath := githubIdentityParts(result)
	if repo != "" && ref != "" && sourcePath != "" {
		return "github:" + repo + "\x00" + ref + "\x00" + sourcePath
	}
	if hasGitHubSource(result) {
		return ""
	}
	if source := exactSourceIdentity(result); source != "" && strings.TrimSpace(result.ID) != "" {
		return "source:" + source + "#" + strings.ToLower(strings.TrimSpace(result.ID))
	}
	return ""
}

func githubIdentityParts(result SearchResult) (string, string, string) {
	canonical := parseGitHubSourceParts(result.CanonicalSource)
	if canonical.repo != "" {
		if canonical.path != "" {
			return githubIdentityWithRef(canonical, result)
		}
		if skillPath := normalizeSkillPath(result.SkillPath); skillPath != "" {
			canonical.path = skillPath
			return githubIdentityWithRef(canonical, result)
		}
	}

	for _, candidate := range resultSourcesWithoutCanonical(result) {
		parts := parseGitHubSourceParts(candidate)
		if parts.repo != "" && parts.path != "" {
			return githubIdentityWithRef(parts, result)
		}
	}
	return "", "", ""
}

func githubIdentityWithRef(parts githubSourceParts, result SearchResult) (string, string, string) {
	ref := parts.ref
	if ref == "" {
		ref, _ = result.ProviderMetadata["default_branch"].(string)
		ref = strings.TrimSpace(ref)
	}
	if ref == "" {
		return "", "", ""
	}
	return parts.repo, ref, parts.path
}

func resultSourcesWithoutCanonical(result SearchResult) []string {
	sources := []string{result.InstallURL}
	for _, key := range []string{"raw_url", "html_url", "source_url", "sourceUrl", "url"} {
		if value, ok := result.ProviderMetadata[key].(string); ok {
			sources = append(sources, value)
		}
	}
	return sources
}

type githubSourceParts struct {
	repo string
	ref  string
	path string
}

func parseGitHubSource(raw string) (string, string) {
	parts := parseGitHubSourceParts(raw)
	return parts.repo, parts.path
}

func parseGitHubSourceParts(raw string) githubSourceParts {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return githubSourceParts{}
	}
	if strings.HasPrefix(raw, "github:") {
		raw = strings.TrimPrefix(raw, "github:")
	}
	if strings.HasPrefix(raw, "git@github.com:") {
		raw = "https://github.com/" + strings.TrimPrefix(raw, "git@github.com:")
	}
	if !strings.Contains(raw, "://") {
		parts := strings.Split(strings.Trim(raw, "/"), "/")
		if len(parts) >= 2 {
			repo := normalizedRepo(parts[0], parts[1])
			if len(parts) > 2 {
				return githubSourceParts{repo: repo, path: normalizeSkillPath(strings.Join(parts[2:], "/"))}
			}
			return githubSourceParts{repo: repo}
		}
		return githubSourceParts{}
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return githubSourceParts{}
	}
	host := strings.ToLower(parsed.Hostname())
	parts := splitURLPath(parsed.Path)
	switch host {
	case "github.com", "www.github.com":
		if len(parts) < 2 {
			return githubSourceParts{}
		}
		repo := normalizedRepo(parts[0], parts[1])
		if len(parts) >= 4 && (parts[2] == "tree" || parts[2] == "blob") {
			sourcePath := ""
			if len(parts) >= 5 {
				sourcePath = normalizeSkillPath(strings.Join(parts[4:], "/"))
			}
			if parts[2] == "tree" && sourcePath != "" && !strings.EqualFold(path.Base(sourcePath), "SKILL.md") {
				sourcePath = path.Join(sourcePath, "SKILL.md")
			}
			return githubSourceParts{repo: repo, ref: strings.TrimSpace(parts[3]), path: sourcePath}
		}
		return githubSourceParts{repo: repo}
	case "raw.githubusercontent.com":
		if len(parts) < 4 {
			return githubSourceParts{}
		}
		return githubSourceParts{
			repo: normalizedRepo(parts[0], parts[1]),
			ref:  strings.TrimSpace(parts[2]),
			path: normalizeSkillPath(strings.Join(parts[3:], "/")),
		}
	default:
		return githubSourceParts{}
	}
}

func hasGitHubSource(result SearchResult) bool {
	if parseGitHubSourceParts(result.CanonicalSource).repo != "" {
		return true
	}
	for _, candidate := range resultSourcesWithoutCanonical(result) {
		if parseGitHubSourceParts(candidate).repo != "" {
			return true
		}
	}
	return false
}

func splitURLPath(value string) []string {
	parts := strings.Split(strings.Trim(value, "/"), "/")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if unescaped, err := url.PathUnescape(part); err == nil {
			part = unescaped
		}
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func normalizedRepo(owner, repository string) string {
	owner = strings.TrimSpace(owner)
	repository = strings.TrimSuffix(strings.TrimSpace(repository), ".git")
	if owner == "" || repository == "" {
		return ""
	}
	return strings.ToLower(owner + "/" + repository)
}

func normalizeSkillPath(raw string) string {
	raw = strings.Trim(strings.ReplaceAll(raw, "\\", "/"), "/")
	if raw == "" {
		return ""
	}
	cleaned := path.Clean(raw)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return ""
	}
	return cleaned
}

func exactSourceIdentity(result SearchResult) string {
	for _, source := range []string{result.CanonicalSource, result.InstallURL} {
		if normalized := normalizeURL(source); normalized != "" {
			return normalized
		}
	}
	return ""
}

func normalizeURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	parsed.Fragment = ""
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Path = strings.TrimSuffix(parsed.Path, "/")
	return parsed.String()
}

func mergeResults(results []SearchResult) SearchResult {
	sorted := append([]SearchResult(nil), results...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return stableResultKey(sorted[i]) < stableResultKey(sorted[j])
	})
	githubRepo, githubRef, githubPath := githubIdentityParts(sorted[0])
	merged := cloneResult(sorted[0])
	merged.Provenance = nil
	merged.Providers = nil
	for _, result := range sorted {
		merged.Provenance = append(merged.Provenance, provenance(result))
		merged.Providers = appendUnique(merged.Providers, result.Provider)
		merged.ID = strongerText(merged.ID, result.ID)
		merged.Name = strongerText(merged.Name, result.Name)
		merged.Description = strongerText(merged.Description, result.Description)
		merged.CanonicalSource = strongerSource(merged.CanonicalSource, result.CanonicalSource)
		merged.InstallURL = strongerInstallURL(merged.InstallURL, result.InstallURL)
		merged.Author = strongerText(merged.Author, result.Author)
		merged.Authors = unionStrings(merged.Authors, result.Authors)
		merged.Repository = strongerText(merged.Repository, result.Repository)
		merged.SkillPath = strongerText(merged.SkillPath, result.SkillPath)
		merged.Tags = unionStrings(merged.Tags, result.Tags)
		merged.Category = strongerText(merged.Category, result.Category)
		merged.Version = strongerText(merged.Version, result.Version)
		merged.Stars = max(merged.Stars, result.Stars)
		merged.Installs = max(merged.Installs, result.Installs)
		merged.Rating = max(merged.Rating, result.Rating)
		merged.VerificationStatus = strongerVerification(merged.VerificationStatus, result.VerificationStatus)
		merged.AuditStatus = strongerAudit(merged.AuditStatus, result.AuditStatus)
		merged.UpdatedAt = later(merged.UpdatedAt, result.UpdatedAt)
		merged.ProviderMetadata = mergeMetadata(merged.ProviderMetadata, result.ProviderMetadata)
	}
	if githubRepo != "" && githubRef != "" && githubPath != "" {
		// strongerSource intentionally collapses equivalent GitHub URLs to
		// owner/repo. Keep the source-local path and ref so command rendering can
		// still target the same SKILL.md after the merge.
		merged.SkillPath = githubPath
		merged.ProviderMetadata["source_ref"] = githubRef
	}
	sort.SliceStable(merged.Provenance, func(i, j int) bool {
		leftOrder := providerOrder(merged.Provenance[i].Provider)
		rightOrder := providerOrder(merged.Provenance[j].Provider)
		if leftOrder == rightOrder {
			return stableProvenanceKey(merged.Provenance[i]) < stableProvenanceKey(merged.Provenance[j])
		}
		return leftOrder < rightOrder
	})
	sort.SliceStable(merged.Providers, func(i, j int) bool {
		return providerOrder(merged.Providers[i]) < providerOrder(merged.Providers[j])
	})
	if len(merged.Providers) > 0 {
		merged.Provider = merged.Providers[0]
	}
	return merged
}

func cloneResult(result SearchResult) SearchResult {
	result.Authors = append([]string(nil), result.Authors...)
	result.Tags = append([]string(nil), result.Tags...)
	result.Providers = append([]string(nil), result.Providers...)
	result.ProviderMetadata = copyMetadata(result.ProviderMetadata)
	return result
}

func provenance(result SearchResult) ProviderProvenance {
	return ProviderProvenance{
		Provider: result.Provider, ID: result.ID, Name: result.Name, Description: result.Description, CanonicalSource: result.CanonicalSource, InstallURL: result.InstallURL,
		Author: result.Author, Authors: append([]string(nil), result.Authors...), Repository: result.Repository,
		SkillPath: result.SkillPath, Tags: append([]string(nil), result.Tags...), Category: result.Category,
		Version: result.Version, Stars: result.Stars, Installs: result.Installs, Rating: result.Rating,
		VerificationStatus: result.VerificationStatus, AuditStatus: result.AuditStatus, UpdatedAt: result.UpdatedAt,
		ProviderMetadata: copyMetadata(result.ProviderMetadata),
	}
}

func stableResultKey(result SearchResult) string {
	return strings.Join([]string{
		providerOrder(result.Provider),
		result.ID,
		result.Name,
		result.Description,
		result.CanonicalSource,
		result.InstallURL,
		result.Author,
		strings.Join(result.Authors, "\x00"),
		result.Repository,
		result.SkillPath,
		strings.Join(result.Tags, "\x00"),
		result.Category,
		result.Version,
		strconv.Itoa(result.Stars),
		strconv.Itoa(result.Installs),
		strconv.FormatFloat(result.Rating, 'g', -1, 64),
		result.VerificationStatus,
		result.AuditStatus,
		result.UpdatedAt.UTC().Format(time.RFC3339Nano),
		metadataText(result.ProviderMetadata),
	}, "\x00")
}

func stableProvenanceKey(value ProviderProvenance) string {
	return stableResultKey(SearchResult{
		Provider:           value.Provider,
		ID:                 value.ID,
		Name:               value.Name,
		Description:        value.Description,
		CanonicalSource:    value.CanonicalSource,
		InstallURL:         value.InstallURL,
		Author:             value.Author,
		Authors:            value.Authors,
		Repository:         value.Repository,
		SkillPath:          value.SkillPath,
		Tags:               value.Tags,
		Category:           value.Category,
		Version:            value.Version,
		Stars:              value.Stars,
		Installs:           value.Installs,
		Rating:             value.Rating,
		VerificationStatus: value.VerificationStatus,
		AuditStatus:        value.AuditStatus,
		UpdatedAt:          value.UpdatedAt,
		ProviderMetadata:   value.ProviderMetadata,
	})
}

func providerOrder(provider string) string {
	switch strings.ToLower(provider) {
	case "skills.sh":
		return "01:skills.sh"
	case "skillmd":
		return "02:skillmd"
	case "truefoundry":
		return "03:truefoundry"
	case "github":
		return "04:github"
	default:
		return "99:" + strings.ToLower(provider)
	}
}

func appendUnique(values []string, value string) []string {
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func unionStrings(left, right []string) []string {
	values := append([]string(nil), left...)
	for _, value := range right {
		values = appendUnique(values, value)
	}
	sort.Strings(values)
	return values
}

func strongerText(left, right string) string {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if len(right) > len(left) || (len(right) == len(left) && right < left) {
		return right
	}
	return left
}

func strongerSource(left, right string) string {
	leftRepo, _ := parseGitHubSource(left)
	rightRepo, _ := parseGitHubSource(right)
	if leftRepo != "" && rightRepo != "" {
		return leftRepo
	}
	return strongerText(left, right)
}

func strongerInstallURL(left, right string) string {
	if left == "" {
		return right
	}
	if right == "" {
		return left
	}
	leftDirect := directSkillURL(left)
	rightDirect := directSkillURL(right)
	if leftDirect != rightDirect {
		if leftDirect {
			return right
		}
		return left
	}
	return strongerText(left, right)
}

func directSkillURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	value := strings.ToLower(strings.TrimSuffix(parsed.Path, "/"))
	return strings.HasSuffix(value, "/raw") || strings.HasSuffix(value, "/skill.md")
}

func strongerVerification(left, right string) string {
	leftRank := verificationRank(left)
	rightRank := verificationRank(right)
	if rightRank > leftRank {
		return right
	}
	if leftRank > rightRank {
		return left
	}
	return strongerText(left, right)
}

func verificationRank(value string) int {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "verified", "official", "pass":
		return 3
	case "unverified", "unknown":
		return 1
	default:
		return 0
	}
}

func strongerAudit(left, right string) string {
	leftRank := auditRank(left)
	rightRank := auditRank(right)
	if rightRank > leftRank {
		return right
	}
	if leftRank > rightRank {
		return left
	}
	return strongerText(left, right)
}

func auditRank(value string) int {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "pass", "passed", "verified":
		return 3
	case "warning", "warn":
		return 2
	case "fail", "failed":
		return 1
	default:
		return 0
	}
}

func later(left, right time.Time) time.Time {
	if right.After(left) {
		return right
	}
	return left
}

func mergeMetadata(left, right map[string]any) map[string]any {
	merged := copyMetadata(left)
	for key, value := range right {
		existing, ok := merged[key]
		if !ok || metadataStrength(value) > metadataStrength(existing) || (metadataStrength(value) == metadataStrength(existing) && metadataText(value) < metadataText(existing)) {
			merged[key] = value
		}
	}
	return merged
}

func copyMetadata(metadata map[string]any) map[string]any {
	copy := make(map[string]any, len(metadata))
	for key, value := range metadata {
		copy[key] = value
	}
	return copy
}

func metadataStrength(value any) int {
	switch value := value.(type) {
	case float64:
		return int(value)
	case int:
		return value
	case string:
		return len(value)
	case bool:
		if value {
			return 1
		}
	}
	return 0
}

func metadataText(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(encoded)
}
