package skills

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const (
	// PortabilityCorpusSchemaVersion is the supported major schema version for
	// checked-in portability evidence.
	PortabilityCorpusSchemaVersion = "1"
	// PortabilityReplayClient is the client implementation replayed by the Go
	// test suite. Other clients remain recorded evidence until an offline
	// adapter is added for them.
	PortabilityReplayClient = "goskill"
)

// ErrInvalidPortabilityCorpus identifies malformed or incomplete corpus data.
var ErrInvalidPortabilityCorpus = errors.New("skills: invalid portability corpus")

// PortabilityCorpus is a versioned, offline record of client outcomes for
// portable Agent Skills fixtures. Unknown JSON fields are intentionally
// ignored so newer minor schema additions can be read by older tooling.
type PortabilityCorpus struct {
	SchemaVersion string              `json:"schema_version"`
	Name          string              `json:"name"`
	Clients       []PortabilityClient `json:"clients"`
	Cases         []PortabilityCase   `json:"cases"`
}

// PortabilityClient identifies an external implementation revision or the
// locally replayed goskill implementation.
type PortabilityClient struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Implementation string `json:"implementation"`
	Revision       string `json:"revision"`
	// LocalReplay marks the goskill implementation whose expectation is
	// certified by the offline replay test, rather than external evidence.
	LocalReplay bool `json:"local_replay"`
}

// PortabilityCase records expected outcomes for one fixture and profile.
type PortabilityCase struct {
	ID          string                            `json:"id"`
	Description string                            `json:"description"`
	Fixture     string                            `json:"fixture"`
	Profile     Profile                           `json:"profile"`
	Expected    map[string]PortabilityExpectation `json:"expected"`
	Evidence    []PortabilityEvidence             `json:"evidence"`
}

// PortabilityExpectation records whether a client accepted a fixture and the
// stable diagnostics it emitted when it did not.
type PortabilityExpectation struct {
	Valid       bool                            `json:"valid"`
	Diagnostics []PortabilityExpectedDiagnostic `json:"diagnostics"`
}

// PortabilityExpectedDiagnostic is the stable portion of a diagnostic that
// portability evidence records across client implementations.
type PortabilityExpectedDiagnostic struct {
	Code     string   `json:"code"`
	Severity Severity `json:"severity"`
}

// PortabilityEvidence describes how an external client outcome was obtained.
// ArtifactSHA256 is the deterministic FolderHash of the referenced fixture.
type PortabilityEvidence struct {
	Client         string `json:"client"`
	Source         string `json:"source"`
	Revision       string `json:"revision"`
	Method         string `json:"method"`
	ArtifactSHA256 string `json:"artifact_sha256"`
}

// PortabilityReplayResult contains the recorded and locally replayed outcome
// for one case. Match is false when the implementation has drifted from the
// checked-in expectation.
type PortabilityReplayResult struct {
	CaseID   string                 `json:"case_id"`
	ClientID string                 `json:"client_id"`
	Expected PortabilityExpectation `json:"expected"`
	Actual   PortabilityExpectation `json:"actual"`
	Match    bool                   `json:"match"`
}

// PortabilityCorpusError reports all deterministic schema or fixture errors.
type PortabilityCorpusError struct {
	Problems []string
}

func (e *PortabilityCorpusError) Error() string {
	if len(e.Problems) == 0 {
		return ErrInvalidPortabilityCorpus.Error()
	}
	return ErrInvalidPortabilityCorpus.Error() + ": " + strings.Join(e.Problems, "; ")
}

func (e *PortabilityCorpusError) Unwrap() error {
	return ErrInvalidPortabilityCorpus
}

// PortabilityReplayError reports deterministic mismatches found while
// replaying the goskill expectations in a valid corpus.
type PortabilityReplayError struct {
	Mismatches []string
}

func (e *PortabilityReplayError) Error() string {
	if len(e.Mismatches) == 0 {
		return "portability replay did not match"
	}
	return "portability replay mismatch: " + strings.Join(e.Mismatches, "; ")
}

// LoadPortabilityCorpus decodes and structurally validates a JSON corpus.
// Fixture paths and artifact hashes are checked by ValidatePortabilityCorpus.
func LoadPortabilityCorpus(file string) (PortabilityCorpus, error) {
	raw, err := os.ReadFile(file)
	if err != nil {
		return PortabilityCorpus{}, fmt.Errorf("load portability corpus %s: %w", file, err)
	}
	var corpus PortabilityCorpus
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&corpus); err != nil {
		return PortabilityCorpus{}, fmt.Errorf("decode portability corpus %s: %w", file, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return PortabilityCorpus{}, fmt.Errorf("decode portability corpus %s: multiple JSON values", file)
		}
		return PortabilityCorpus{}, fmt.Errorf("decode portability corpus %s: %w", file, err)
	}
	if err := validatePortabilityCorpusSchema(corpus); err != nil {
		return PortabilityCorpus{}, fmt.Errorf("validate portability corpus %s: %w", file, err)
	}
	return corpus, nil
}

// ValidatePortabilityCorpus validates the corpus schema and every fixture
// referenced by it. fixtureRoot is the directory against which case fixture
// paths are resolved.
func ValidatePortabilityCorpus(corpus PortabilityCorpus, fixtureRoot string) error {
	if err := validatePortabilityCorpusSchema(corpus); err != nil {
		return err
	}
	root, err := filepath.Abs(fixtureRoot)
	if err != nil {
		return fmt.Errorf("resolve portability fixture root: %w", err)
	}
	resolvedRoot, rootErr := filepath.EvalSymlinks(root)
	problems := make([]string, 0)
	for _, testCase := range sortedPortabilityCases(corpus.Cases) {
		fixturePath := filepath.Join(root, filepath.FromSlash(testCase.Fixture))
		if rootErr == nil {
			resolvedFixture, resolveErr := filepath.EvalSymlinks(fixturePath)
			if resolveErr == nil && !PathSafe(resolvedRoot, resolvedFixture) {
				problems = append(problems, fmt.Sprintf("case %q fixture %q: fixture resolves outside the corpus root", testCase.ID, testCase.Fixture))
				continue
			}
		}
		info, statErr := os.Stat(fixturePath)
		if statErr != nil {
			problems = append(problems, fmt.Sprintf("case %q fixture %q: missing fixture: %v", testCase.ID, testCase.Fixture, statErr))
			continue
		}
		if !info.IsDir() {
			problems = append(problems, fmt.Sprintf("case %q fixture %q: fixture is not a directory", testCase.ID, testCase.Fixture))
			continue
		}
		fixtureHash, hashErr := FolderHash(fixturePath)
		if hashErr != nil {
			problems = append(problems, fmt.Sprintf("case %q fixture %q: hash fixture: %v", testCase.ID, testCase.Fixture, hashErr))
			continue
		}
		for _, evidence := range testCase.Evidence {
			if evidence.ArtifactSHA256 != fixtureHash {
				problems = append(problems, fmt.Sprintf("case %q evidence for %q: artifact hash %q does not match fixture hash %q", testCase.ID, evidence.Client, evidence.ArtifactSHA256, fixtureHash))
			}
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return &PortabilityCorpusError{Problems: problems}
	}
	return nil
}

// LoadAndValidatePortabilityCorpus loads a JSON corpus and validates its
// referenced fixtures in one deterministic operation.
func LoadAndValidatePortabilityCorpus(file, fixtureRoot string) (PortabilityCorpus, error) {
	corpus, err := LoadPortabilityCorpus(file)
	if err != nil {
		return PortabilityCorpus{}, err
	}
	if err := ValidatePortabilityCorpus(corpus, fixtureRoot); err != nil {
		return PortabilityCorpus{}, err
	}
	return corpus, nil
}

// ReplayPortabilityCorpus validates and replays the goskill client outcomes
// without network access or external client dependencies.
func ReplayPortabilityCorpus(corpus PortabilityCorpus, fixtureRoot string) ([]PortabilityReplayResult, error) {
	if err := ValidatePortabilityCorpus(corpus, fixtureRoot); err != nil {
		return nil, err
	}
	client, ok := clientByID(corpus.Clients, PortabilityReplayClient)
	if !ok || !client.LocalReplay {
		return nil, fmt.Errorf("portability corpus has no replayable client %q", PortabilityReplayClient)
	}
	root, err := filepath.Abs(fixtureRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve portability fixture root: %w", err)
	}
	results := make([]PortabilityReplayResult, 0, len(corpus.Cases))
	mismatches := make([]string, 0)
	for _, testCase := range sortedPortabilityCases(corpus.Cases) {
		expected := testCase.Expected[PortabilityReplayClient]
		fixturePath := filepath.Join(root, filepath.FromSlash(testCase.Fixture))
		diagnostics := ValidateSkillDirectoryWithProfile(fixturePath, testCase.Profile)
		actual := expectationFromDiagnostics(diagnostics)
		match := expectationsEqual(expected, actual)
		results = append(results, PortabilityReplayResult{
			CaseID:   testCase.ID,
			ClientID: PortabilityReplayClient,
			Expected: expected,
			Actual:   actual,
			Match:    match,
		})
		if !match {
			mismatches = append(mismatches, fmt.Sprintf("case %q expected %#v, got %#v", testCase.ID, expected, actual))
		}
	}
	if len(mismatches) > 0 {
		sort.Strings(mismatches)
		return results, &PortabilityReplayError{Mismatches: mismatches}
	}
	return results, nil
}

func validatePortabilityCorpusSchema(corpus PortabilityCorpus) error {
	problems := make([]string, 0)
	if corpus.SchemaVersion != PortabilityCorpusSchemaVersion {
		problems = append(problems, fmt.Sprintf("unsupported schema_version %q (supported: %q)", corpus.SchemaVersion, PortabilityCorpusSchemaVersion))
	}
	if !stablePortabilityID(corpus.Name) {
		problems = append(problems, fmt.Sprintf("invalid corpus name %q", corpus.Name))
	}
	if len(corpus.Clients) == 0 {
		problems = append(problems, "clients must not be empty")
	}
	if len(corpus.Cases) == 0 {
		problems = append(problems, "cases must not be empty")
	}

	clients := make(map[string]PortabilityClient, len(corpus.Clients))
	for _, client := range corpus.Clients {
		if !stablePortabilityID(client.ID) {
			problems = append(problems, fmt.Sprintf("invalid client id %q", client.ID))
			continue
		}
		if _, exists := clients[client.ID]; exists {
			problems = append(problems, fmt.Sprintf("duplicate client id %q", client.ID))
			continue
		}
		clients[client.ID] = client
		if strings.TrimSpace(client.Name) == "" {
			problems = append(problems, fmt.Sprintf("client %q has empty name", client.ID))
		}
		if strings.TrimSpace(client.Implementation) == "" {
			problems = append(problems, fmt.Sprintf("client %q has empty implementation", client.ID))
		}
		if client.LocalReplay {
			if client.ID != PortabilityReplayClient {
				problems = append(problems, fmt.Sprintf("client %q is not supported for local replay", client.ID))
			}
			if client.Revision != "" {
				problems = append(problems, fmt.Sprintf("locally replayed client %q must not declare an external revision", client.ID))
			}
			continue
		}
		if !isGitRevision(client.Revision) {
			problems = append(problems, fmt.Sprintf("client %q revision is not a full lowercase git SHA", client.ID))
		}
	}
	if replayClient, exists := clients[PortabilityReplayClient]; !exists || !replayClient.LocalReplay {
		problems = append(problems, fmt.Sprintf("client %q must be marked for local replay", PortabilityReplayClient))
	}

	caseIDs := make(map[string]bool, len(corpus.Cases))
	for _, testCase := range corpus.Cases {
		if !stablePortabilityID(testCase.ID) {
			problems = append(problems, fmt.Sprintf("invalid case id %q", testCase.ID))
		} else if caseIDs[testCase.ID] {
			problems = append(problems, fmt.Sprintf("duplicate case id %q", testCase.ID))
		} else {
			caseIDs[testCase.ID] = true
		}
		if strings.TrimSpace(testCase.Description) == "" {
			problems = append(problems, fmt.Sprintf("case %q has empty description", testCase.ID))
		}
		if !safeFixturePath(testCase.Fixture) {
			problems = append(problems, fmt.Sprintf("case %q has unsafe fixture path %q", testCase.ID, testCase.Fixture))
		}
		if testCase.Profile != ProfilePortable {
			problems = append(problems, fmt.Sprintf("case %q must use portable profile", testCase.ID))
		}
		if len(testCase.Expected) != len(clients) {
			problems = append(problems, fmt.Sprintf("case %q expected outcomes must cover every client", testCase.ID))
		}
		for clientID, expected := range testCase.Expected {
			_, known := clients[clientID]
			if !known {
				problems = append(problems, fmt.Sprintf("case %q has expectation for unknown client %q", testCase.ID, clientID))
			}
			problems = append(problems, validateExpectation(testCase, clientID, expected)...)
		}
		problems = append(problems, validateEvidence(testCase, clients)...)
	}

	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	return &PortabilityCorpusError{Problems: problems}
}

func validateExpectation(testCase PortabilityCase, clientID string, expected PortabilityExpectation) []string {
	problems := make([]string, 0)
	seenCodes := make(map[string]bool, len(expected.Diagnostics))
	hasError := false
	previous := ""
	rules := make(map[string]Rule)
	for _, rule := range RulesForProfile(testCase.Profile) {
		rules[rule.Code] = rule
	}
	for _, diagnostic := range expected.Diagnostics {
		if diagnostic.Code == "" {
			problems = append(problems, fmt.Sprintf("case %q client %q has empty diagnostic code", testCase.ID, clientID))
			continue
		}
		if seenCodes[diagnostic.Code] {
			problems = append(problems, fmt.Sprintf("case %q client %q has duplicate diagnostic code %q", testCase.ID, clientID, diagnostic.Code))
		}
		seenCodes[diagnostic.Code] = true
		if previous != "" && diagnostic.Code <= previous {
			problems = append(problems, fmt.Sprintf("case %q client %q diagnostics are not sorted by code", testCase.ID, clientID))
		}
		previous = diagnostic.Code
		rule, known := rules[diagnostic.Code]
		if !known {
			problems = append(problems, fmt.Sprintf("case %q client %q has diagnostic outside %q profile: %q", testCase.ID, clientID, testCase.Profile, diagnostic.Code))
			continue
		}
		if diagnostic.Severity != rule.Severity {
			problems = append(problems, fmt.Sprintf("case %q client %q diagnostic %q has severity %q, want %q", testCase.ID, clientID, diagnostic.Code, diagnostic.Severity, rule.Severity))
		}
		if diagnostic.Severity == SeverityError {
			hasError = true
		}
	}
	if expected.Valid == hasError {
		problems = append(problems, fmt.Sprintf("case %q client %q valid=%t is inconsistent with expected diagnostics", testCase.ID, clientID, expected.Valid))
	}
	return problems
}

func validateEvidence(testCase PortabilityCase, clients map[string]PortabilityClient) []string {
	problems := make([]string, 0)
	seenClients := make(map[string]bool, len(testCase.Evidence))
	for _, evidence := range testCase.Evidence {
		client, known := clients[evidence.Client]
		if !known {
			problems = append(problems, fmt.Sprintf("case %q evidence names unknown client %q", testCase.ID, evidence.Client))
			continue
		}
		if seenClients[evidence.Client] {
			problems = append(problems, fmt.Sprintf("case %q has duplicate evidence for client %q", testCase.ID, evidence.Client))
		}
		seenClients[evidence.Client] = true
		if client.LocalReplay {
			problems = append(problems, fmt.Sprintf("case %q locally replayed client %q must not declare external evidence", testCase.ID, evidence.Client))
			continue
		}
		if evidence.Revision != client.Revision {
			problems = append(problems, fmt.Sprintf("case %q evidence for %q is not pinned to client revision", testCase.ID, evidence.Client))
		}
		if !immutableSourceURL(evidence.Source, client.Implementation, evidence.Revision) {
			problems = append(problems, fmt.Sprintf("case %q evidence for %q is not an immutable source URL bound to its implementation and revision", testCase.ID, evidence.Client))
		}
		if strings.TrimSpace(evidence.Method) == "" {
			problems = append(problems, fmt.Sprintf("case %q evidence for %q has no reproducible method", testCase.ID, evidence.Client))
		}
		if !isSHA256(evidence.ArtifactSHA256) {
			problems = append(problems, fmt.Sprintf("case %q evidence for %q has invalid artifact_sha256", testCase.ID, evidence.Client))
		}
	}
	if len(seenClients) != len(clients) {
		for clientID := range clients {
			if !clients[clientID].LocalReplay && !seenClients[clientID] {
				problems = append(problems, fmt.Sprintf("case %q has no evidence for client %q", testCase.ID, clientID))
			}
		}
	}
	return problems
}

func expectationFromDiagnostics(diagnostics []Diagnostic) PortabilityExpectation {
	expected := PortabilityExpectation{
		Valid:       true,
		Diagnostics: make([]PortabilityExpectedDiagnostic, 0, len(diagnostics)),
	}
	for _, diagnostic := range diagnostics {
		expected.Diagnostics = append(expected.Diagnostics, PortabilityExpectedDiagnostic{
			Code:     diagnostic.Code,
			Severity: diagnostic.Severity,
		})
		if diagnostic.Severity == SeverityError {
			expected.Valid = false
		}
	}
	sort.Slice(expected.Diagnostics, func(i, j int) bool {
		if expected.Diagnostics[i].Code == expected.Diagnostics[j].Code {
			return expected.Diagnostics[i].Severity < expected.Diagnostics[j].Severity
		}
		return expected.Diagnostics[i].Code < expected.Diagnostics[j].Code
	})
	return expected
}

func expectationsEqual(left, right PortabilityExpectation) bool {
	if left.Valid != right.Valid || len(left.Diagnostics) != len(right.Diagnostics) {
		return false
	}
	for i := range left.Diagnostics {
		if left.Diagnostics[i] != right.Diagnostics[i] {
			return false
		}
	}
	return true
}

func clientByID(clients []PortabilityClient, id string) (PortabilityClient, bool) {
	for _, client := range clients {
		if client.ID == id {
			return client, true
		}
	}
	return PortabilityClient{}, false
}

func stablePortabilityID(value string) bool {
	if value == "" {
		return false
	}
	for i, char := range value {
		isLetter := char >= 'a' && char <= 'z'
		isDigit := char >= '0' && char <= '9'
		if !isLetter && !isDigit && (char != '-' || i == 0 || i == len(value)-1 || value[i-1] == '-') {
			return false
		}
	}
	return true
}

func safeFixturePath(value string) bool {
	if value == "" || strings.ContainsAny(value, "\\:") || strings.HasPrefix(value, "/") {
		return false
	}
	clean := path.Clean(value)
	return clean != "." && clean != ".." && !strings.HasPrefix(clean, "../") && clean == value
}

func immutableSourceURL(value, implementation, revision string) bool {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil || parsed.RawPath != "" {
		return false
	}
	implementationHost, implementationPath, ok := splitImplementation(implementation)
	if !ok || !strings.EqualFold(parsed.Host, implementationHost) {
		return false
	}
	sourcePath := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(sourcePath) == 0 || sourcePath[0] == "" {
		return false
	}
	for _, segment := range sourcePath {
		if !validRepositoryPathSegment(segment) {
			return false
		}
	}
	for index, segment := range sourcePath {
		if segment == "tree" && index >= 2 && index+1 < len(sourcePath) && sourcePath[index+1] == revision {
			return matchesImplementationPath(implementationPath, sourcePath, index, index+2)
		}
		if segment == "-" && index >= 2 && index+2 < len(sourcePath) && sourcePath[index+1] == "tree" && sourcePath[index+2] == revision {
			return matchesImplementationPath(implementationPath, sourcePath, index, index+3)
		}
	}
	return false
}

func splitImplementation(value string) (string, []string, bool) {
	parts := strings.Split(value, "/")
	if len(parts) < 3 || !strings.Contains(parts[0], ".") {
		return "", nil, false
	}
	for _, part := range parts {
		if !validRepositoryPathSegment(part) {
			return "", nil, false
		}
	}
	return parts[0], parts[1:], true
}

func matchesImplementationPath(implementationPath, sourcePath []string, marker, suffixStart int) bool {
	boundPath := append(append([]string{}, sourcePath[:marker]...), sourcePath[suffixStart:]...)
	return strings.Join(boundPath, "/") == strings.Join(implementationPath, "/")
}

func validRepositoryPathSegment(value string) bool {
	if value == "" || value == "." || value == ".." {
		return false
	}
	for _, char := range value {
		isLetter := char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z'
		isDigit := char >= '0' && char <= '9'
		if !isLetter && !isDigit && char != '.' && char != '-' && char != '_' {
			return false
		}
	}
	return true
}

func isGitRevision(value string) bool {
	return len(value) == 40 && isLowerHex(value)
}

func isSHA256(value string) bool {
	return len(value) == sha256.Size*2 && isLowerHex(value)
}

func isLowerHex(value string) bool {
	_, err := hex.DecodeString(value)
	if err != nil {
		return false
	}
	return value == strings.ToLower(value)
}

func sortedPortabilityCases(cases []PortabilityCase) []PortabilityCase {
	ordered := append([]PortabilityCase(nil), cases...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].ID < ordered[j].ID
	})
	return ordered
}
