package skills

import "strings"

const (
	// SpecRepository identifies the immutable upstream repository that provides
	// the specification snapshot used for conformance validation.
	SpecRepository = "agentskills/agentskills"
	// SpecRepositoryURL is the canonical source repository for the pin.
	SpecRepositoryURL = "https://github.com/" + SpecRepository
	// SpecRevision is the immutable agentskills/agentskills revision that
	// defines the conformance contract implemented by this package.
	SpecRevision = "69ef37e9424c0a7ea9dd2293b559e43ec8176379"
	// SpecCanonicalURL is the canonical published Agent Skills specification.
	SpecCanonicalURL = "https://agentskills.io/specification"
	// SpecSourceURL is the exact upstream specification snapshot for the pin.
	SpecSourceURL = SpecRepositoryURL + "/blob/" + SpecRevision + "/docs/specification.mdx"
	// SpecVersioningStatus explains why the pin is a Git revision rather than a
	// semantic version: upstream publishes no formal numbered specification.
	SpecVersioningStatus = "upstream unversioned; identified by immutable Git revision"
)

const specDisplayRevisionLength = 7

// SpecDisplayRevision returns a concise, script-friendly representation of the
// immutable revision without duplicating the pinned value at call sites.
func SpecDisplayRevision() string {
	if len(SpecRevision) <= specDisplayRevisionLength {
		return SpecRevision
	}
	return SpecRevision[:specDisplayRevisionLength]
}

// SpecReference returns the upstream repository and concise pinned revision.
func SpecReference() string {
	return SpecRepository + "@" + SpecDisplayRevision()
}

// Severity classifies a validation diagnostic.
type Severity string

const (
	// SeverityError indicates a conformance or portability failure.
	SeverityError Severity = "error"
	// SeverityWarning indicates guidance that does not make a skill invalid.
	SeverityWarning Severity = "warning"
)

// Profile selects the Agent Skills validation policy.
type Profile string

const (
	// ProfileSpec enforces only normative Agent Skills requirements.
	ProfileSpec Profile = "spec"
	// ProfileRecommended adds official authoring guidance as warnings.
	ProfileRecommended Profile = "recommended"
	// ProfilePortable adds narrowly scoped cross-client interoperability errors.
	ProfilePortable Profile = "portable"
)

// Valid reports whether p is a supported validation profile.
func (p Profile) Valid() bool {
	return p == ProfileSpec || p == ProfileRecommended || p == ProfilePortable
}

// Diagnostic is a structured Agent Skills conformance finding. Line and
// Column are always 1-based; file-level findings use the deterministic 1:1
// fallback location.
type Diagnostic struct {
	Code     string   `json:"code"`
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
	Path     string   `json:"path"`
	Line     int      `json:"line"`
	Column   int      `json:"column"`
}

// Rule describes one stable Agent Skills conformance rule. Codes are explicit
// rather than derived so automation can depend on them across releases.
type Rule struct {
	Code      string   `json:"code"`
	Summary   string   `json:"summary"`
	Severity  Severity `json:"severity"`
	Profile   Profile  `json:"profile"`
	Source    string   `json:"source,omitempty"`
	Rationale string   `json:"rationale,omitempty"`
}

const (
	RuleSkillMDRequired        = "AS001"
	RuleFrontmatterRequired    = "AS002"
	RuleFrontmatterYAML        = "AS003"
	RuleTopLevelFields         = "AS004"
	RuleNameRequired           = "AS005"
	RuleNameType               = "AS006"
	RuleNameLength             = "AS007"
	RuleNameLowercase          = "AS008"
	RuleNameHyphenBoundary     = "AS009"
	RuleNameConsecutiveHyphens = "AS010"
	RuleNameCharacters         = "AS011"
	RuleNameDirectory          = "AS012"
	RuleDescriptionRequired    = "AS013"
	RuleDescriptionType        = "AS014"
	RuleDescriptionLength      = "AS015"
	RuleCompatibilityType      = "AS016"
	RuleCompatibilityLength    = "AS017"
	RuleMetadataMapping        = "AS018"
	RuleMetadataValues         = "AS019"
	RuleAllowedToolsType       = "AS020"
	RuleSkillLineCount         = "GS210"
	RuleSkillFilename          = "GP310"
)

var specRuleCatalog = []Rule{
	{Code: RuleSkillMDRequired, Summary: "SKILL.md is required and readable", Severity: SeverityError, Profile: ProfileSpec, Source: SpecSourceURL},
	{Code: RuleFrontmatterRequired, Summary: "YAML frontmatter is required", Severity: SeverityError, Profile: ProfileSpec, Source: SpecSourceURL},
	{Code: RuleFrontmatterYAML, Summary: "frontmatter must be valid YAML mapping", Severity: SeverityError, Profile: ProfileSpec, Source: SpecSourceURL},
	{Code: RuleTopLevelFields, Summary: "only specified frontmatter fields are allowed", Severity: SeverityError, Profile: ProfileSpec, Source: SpecSourceURL},
	{Code: RuleNameRequired, Summary: "name is required", Severity: SeverityError, Profile: ProfileSpec, Source: SpecSourceURL},
	{Code: RuleNameType, Summary: "name must be a non-empty string", Severity: SeverityError, Profile: ProfileSpec, Source: SpecSourceURL},
	{Code: RuleNameLength, Summary: "name must be at most 64 characters", Severity: SeverityError, Profile: ProfileSpec, Source: SpecSourceURL},
	{Code: RuleNameLowercase, Summary: "name must be lowercase", Severity: SeverityError, Profile: ProfileSpec, Source: SpecSourceURL},
	{Code: RuleNameHyphenBoundary, Summary: "name cannot start or end with a hyphen", Severity: SeverityError, Profile: ProfileSpec, Source: SpecSourceURL},
	{Code: RuleNameConsecutiveHyphens, Summary: "name cannot contain consecutive hyphens", Severity: SeverityError, Profile: ProfileSpec, Source: SpecSourceURL},
	{Code: RuleNameCharacters, Summary: "name may contain only letters, digits, and hyphens", Severity: SeverityError, Profile: ProfileSpec, Source: SpecSourceURL},
	{Code: RuleNameDirectory, Summary: "name must match its parent directory", Severity: SeverityError, Profile: ProfileSpec, Source: SpecSourceURL},
	{Code: RuleDescriptionRequired, Summary: "description is required", Severity: SeverityError, Profile: ProfileSpec, Source: SpecSourceURL},
	{Code: RuleDescriptionType, Summary: "description must be a non-empty string", Severity: SeverityError, Profile: ProfileSpec, Source: SpecSourceURL},
	{Code: RuleDescriptionLength, Summary: "description must be at most 1024 characters", Severity: SeverityError, Profile: ProfileSpec, Source: SpecSourceURL},
	{Code: RuleCompatibilityType, Summary: "compatibility must be a string", Severity: SeverityError, Profile: ProfileSpec, Source: SpecSourceURL},
	{Code: RuleCompatibilityLength, Summary: "compatibility must be between 1 and 500 characters", Severity: SeverityError, Profile: ProfileSpec, Source: SpecSourceURL},
	{Code: RuleMetadataMapping, Summary: "metadata must be a mapping", Severity: SeverityError, Profile: ProfileSpec, Source: SpecSourceURL},
	{Code: RuleMetadataValues, Summary: "metadata keys and values must be strings", Severity: SeverityError, Profile: ProfileSpec, Source: SpecSourceURL},
	{Code: RuleAllowedToolsType, Summary: "allowed-tools must be a string", Severity: SeverityError, Profile: ProfileSpec, Source: SpecSourceURL},
}

var recommendedRuleCatalog = []Rule{
	{Code: RuleSkillLineCount, Summary: "SKILL.md should be 500 lines or fewer", Severity: SeverityWarning, Profile: ProfileRecommended, Source: "https://agentskills.io/skill-creation/best-practices"},
}

var portableRuleCatalog = []Rule{
	{Code: RuleSkillFilename, Summary: "portable skills require the exact uppercase filename SKILL.md", Severity: SeverityError, Profile: ProfilePortable, Rationale: "case-sensitive clients require the exact uppercase filename"},
}

// Rules returns the complete, stable Agent Skills rule catalog.
func Rules() []Rule {
	rules := append([]Rule(nil), specRuleCatalog...)
	rules = append(rules, recommendedRuleCatalog...)
	rules = append(rules, portableRuleCatalog...)
	return rules
}

// RulesForProfile returns every rule that can be emitted by profile.
func RulesForProfile(profile Profile) []Rule {
	rules := append([]Rule(nil), specRuleCatalog...)
	if profile == ProfileRecommended || profile == ProfilePortable {
		rules = append(rules, recommendedRuleCatalog...)
	}
	if profile == ProfilePortable {
		rules = append(rules, portableRuleCatalog...)
	}
	return rules
}

// RuleForCode returns stable metadata for code when it is part of the catalog.
func RuleForCode(code string) (Rule, bool) {
	code = NormalizeRuleCode(code)
	for _, rule := range Rules() {
		if rule.Code == code {
			return rule, true
		}
	}
	return Rule{}, false
}

// NormalizeRuleCode converts user-supplied rule codes to the catalog's stable
// uppercase form. It intentionally only trims surrounding whitespace and
// changes ASCII case so malformed and non-ASCII codes remain unknown to callers.
func NormalizeRuleCode(code string) string {
	normalized := []byte(strings.TrimSpace(code))
	for i, char := range normalized {
		if char >= 'a' && char <= 'z' {
			normalized[i] = char - ('a' - 'A')
		}
	}
	return string(normalized)
}

var allowedFrontmatterFields = map[string]bool{
	"name":          true,
	"description":   true,
	"license":       true,
	"allowed-tools": true,
	"metadata":      true,
	"compatibility": true,
}
