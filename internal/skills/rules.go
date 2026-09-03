package skills

// SpecRevision is the immutable agentskills/agentskills revision that defines
// the conformance contract implemented by this package.
const SpecRevision = "69ef37e9424c0a7ea9dd2293b559e43ec8176379"

// Severity classifies a validation diagnostic.
type Severity string

const (
	// SeverityError indicates a conformance failure. P0 has no warning rules.
	SeverityError Severity = "error"
)

// Diagnostic is a structured Agent Skills conformance finding. Line and
// Column are zero until source locations are available.
type Diagnostic struct {
	Code     string
	Severity Severity
	Message  string
	Path     string
	Line     int
	Column   int
}

// Rule describes one stable Agent Skills conformance rule. Codes are explicit
// rather than derived so automation can depend on them across releases.
type Rule struct {
	Code    string
	Summary string
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
)

var ruleCatalog = []Rule{
	{RuleSkillMDRequired, "SKILL.md is required and readable"},
	{RuleFrontmatterRequired, "YAML frontmatter is required"},
	{RuleFrontmatterYAML, "frontmatter must be valid YAML mapping"},
	{RuleTopLevelFields, "only specified frontmatter fields are allowed"},
	{RuleNameRequired, "name is required"},
	{RuleNameType, "name must be a non-empty string"},
	{RuleNameLength, "name must be at most 64 characters"},
	{RuleNameLowercase, "name must be lowercase"},
	{RuleNameHyphenBoundary, "name cannot start or end with a hyphen"},
	{RuleNameConsecutiveHyphens, "name cannot contain consecutive hyphens"},
	{RuleNameCharacters, "name may contain only letters, digits, and hyphens"},
	{RuleNameDirectory, "name must match its parent directory"},
	{RuleDescriptionRequired, "description is required"},
	{RuleDescriptionType, "description must be a non-empty string"},
	{RuleDescriptionLength, "description must be at most 1024 characters"},
	{RuleCompatibilityType, "compatibility must be a string"},
	{RuleCompatibilityLength, "compatibility must be between 1 and 500 characters"},
	{RuleMetadataMapping, "metadata must be a mapping"},
	{RuleMetadataValues, "metadata keys and values must be strings"},
	{RuleAllowedToolsType, "allowed-tools must be a string"},
}

// Rules returns the complete, stable Agent Skills rule catalog.
func Rules() []Rule {
	return append([]Rule(nil), ruleCatalog...)
}

var allowedFrontmatterFields = map[string]bool{
	"name":          true,
	"description":   true,
	"license":       true,
	"allowed-tools": true,
	"metadata":      true,
	"compatibility": true,
}
