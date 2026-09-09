package skills

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPortabilityCorpusReplay(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "portability", "v1")
	corpus, err := LoadAndValidatePortabilityCorpus(filepath.Join(root, "corpus.json"), root)
	if err != nil {
		t.Fatal(err)
	}
	results, err := ReplayPortabilityCorpus(corpus, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("replay results = %d, want 2", len(results))
	}
	for _, result := range results {
		if !result.Match || result.ClientID != PortabilityReplayClient {
			t.Fatalf("replay result = %#v", result)
		}
	}
	if got := results[0].Actual.Diagnostics; len(got) != 1 || got[0].Code != RuleSkillFilename {
		t.Fatalf("lowercase replay diagnostics = %#v", got)
	}
}

func TestLoadPortabilityCorpusAllowsUnknownFields(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "corpus.json")
	data := `{
  "schema_version": "1",
  "name": "future-fields",
  "future_field": {"kept_by_newer_reader": true},
  "clients": [{
    "id": "goskill",
    "name": "goskill",
		"implementation": "example.com/goskill",
		"local_replay": true
  }],
  "cases": [{
    "id": "valid-fixture",
    "description": "valid fixture",
    "fixture": "fixture",
    "profile": "portable",
    "expected": {"goskill": {"valid": true, "diagnostics": []}},
    "evidence": []
  }]
}`
	if err := os.WriteFile(file, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPortabilityCorpus(file); err != nil {
		t.Fatal(err)
	}
}

func TestPortabilityCorpusSchemaValidation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*PortabilityCorpus)
		want   string
	}{
		{
			name: "goskill is not marked for local replay",
			mutate: func(corpus *PortabilityCorpus) {
				corpus.Clients[0].LocalReplay = false
				corpus.Clients[0].Revision = strings.Repeat("a", 40)
			},
			want: "must be marked for local replay",
		},
		{
			name: "locally replayed client declares external evidence",
			mutate: func(corpus *PortabilityCorpus) {
				corpus.Cases[0].Evidence = append(corpus.Cases[0].Evidence, PortabilityEvidence{
					Client:         "goskill",
					Source:         "https://example.com/goskill/tree/" + corpus.Clients[0].Revision,
					Revision:       corpus.Clients[0].Revision,
					Method:         "go test",
					ArtifactSHA256: strings.Repeat("b", 64),
				})
			},
			want: "must not declare external evidence",
		},
		{
			name: "mutable source branch",
			mutate: func(corpus *PortabilityCorpus) {
				corpus.Cases[0].Evidence[0].Source = "https://example.com/owner/repository/tree/main/skills-ref"
			},
			want: "not an immutable source URL bound",
		},
		{
			name: "source wrong repository",
			mutate: func(corpus *PortabilityCorpus) {
				corpus.Cases[0].Evidence[0].Source = "https://example.com/other/tree/" + corpus.Cases[0].Evidence[0].Revision
			},
			want: "not an immutable source URL bound",
		},
		{
			name: "source wrong revision",
			mutate: func(corpus *PortabilityCorpus) {
				corpus.Cases[0].Evidence[0].Source = "https://example.com/owner/repository/tree/" + strings.Repeat("c", 40) + "/skills-ref"
			},
			want: "not an immutable source URL bound",
		},
		{
			name: "unsupported version",
			mutate: func(corpus *PortabilityCorpus) {
				corpus.SchemaVersion = "2"
			},
			want: "unsupported schema_version",
		},
		{
			name: "duplicate case id",
			mutate: func(corpus *PortabilityCorpus) {
				corpus.Cases = append(corpus.Cases, corpus.Cases[0])
			},
			want: "duplicate case id",
		},
		{
			name: "duplicate client id",
			mutate: func(corpus *PortabilityCorpus) {
				corpus.Clients = append(corpus.Clients, corpus.Clients[0])
			},
			want: "duplicate client id",
		},
		{
			name: "unknown diagnostic",
			mutate: func(corpus *PortabilityCorpus) {
				corpus.Cases[0].Expected["goskill"] = PortabilityExpectation{
					Valid: false,
					Diagnostics: []PortabilityExpectedDiagnostic{
						{Code: "GP999", Severity: SeverityError},
					},
				}
			},
			want: "outside \"portable\" profile",
		},
		{
			name: "invalid diagnostic severity",
			mutate: func(corpus *PortabilityCorpus) {
				corpus.Cases[0].Expected["goskill"] = PortabilityExpectation{
					Valid: false,
					Diagnostics: []PortabilityExpectedDiagnostic{
						{Code: RuleSkillFilename, Severity: Severity("notice")},
					},
				}
			},
			want: "has severity \"notice\", want \"error\"",
		},
		{
			name: "inconsistent validity",
			mutate: func(corpus *PortabilityCorpus) {
				corpus.Cases[0].Expected["goskill"] = PortabilityExpectation{
					Valid: true,
					Diagnostics: []PortabilityExpectedDiagnostic{
						{Code: RuleSkillFilename, Severity: SeverityError},
					},
				}
			},
			want: "valid=true is inconsistent",
		},
		{
			name: "missing expected client",
			mutate: func(corpus *PortabilityCorpus) {
				delete(corpus.Cases[0].Expected, "skills-ref")
			},
			want: "expected outcomes must cover every client",
		},
		{
			name: "missing evidence",
			mutate: func(corpus *PortabilityCorpus) {
				corpus.Cases[0].Evidence = []PortabilityEvidence{}
			},
			want: "no evidence for client",
		},
		{
			name: "inadequate pin",
			mutate: func(corpus *PortabilityCorpus) {
				corpus.Cases[0].Evidence[0].Revision = "main"
			},
			want: "not pinned to client revision",
		},
		{
			name: "inadequate method",
			mutate: func(corpus *PortabilityCorpus) {
				corpus.Cases[0].Evidence[0].Method = ""
			},
			want: "no reproducible method",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			corpus := testPortabilityCorpus()
			test.mutate(&corpus)
			err := validatePortabilityCorpusSchema(corpus)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validation error = %v, want substring %q", err, test.want)
			}
			if !errors.Is(err, ErrInvalidPortabilityCorpus) {
				t.Fatalf("validation error does not unwrap to ErrInvalidPortabilityCorpus: %v", err)
			}
		})
	}
}

func TestImmutableSourceURL(t *testing.T) {
	revision := strings.Repeat("a", 40)
	tests := []struct {
		name           string
		source         string
		implementation string
		want           bool
	}{
		{
			name:           "github tree source",
			source:         "https://github.com/owner/repository/tree/" + revision + "/tool",
			implementation: "github.com/owner/repository/tool",
			want:           true,
		},
		{
			name:           "gitlab tree source",
			source:         "https://gitlab.example/group/project/-/tree/" + revision + "/tool",
			implementation: "gitlab.example/group/project/tool",
			want:           true,
		},
		{
			name:           "wrong host",
			source:         "https://other.example/owner/repository/tree/" + revision,
			implementation: "github.example/owner/repository",
			want:           false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := immutableSourceURL(test.source, test.implementation, revision); got != test.want {
				t.Fatalf("immutableSourceURL(%q) = %t, want %t", test.source, got, test.want)
			}
		})
	}
}

func TestValidatePortabilityCorpusReportsMissingFixtureAndHash(t *testing.T) {
	corpus := testPortabilityCorpus()
	err := ValidatePortabilityCorpus(corpus, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "missing fixture") {
		t.Fatalf("validation error = %v, want missing fixture", err)
	}

	root := t.TempDir()
	fixture := filepath.Join(root, "fixture")
	if err := os.MkdirAll(fixture, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture, "SKILL.md"), []byte("---\nname: fixture\ndescription: fixture\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err = ValidatePortabilityCorpus(corpus, root)
	if err == nil || !strings.Contains(err.Error(), "artifact hash") {
		t.Fatalf("validation error = %v, want artifact hash", err)
	}
}

func TestReplayPortabilityCorpusReportsMismatch(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "portability", "v1")
	corpus, err := LoadPortabilityCorpus(filepath.Join(root, "corpus.json"))
	if err != nil {
		t.Fatal(err)
	}
	corpus.Cases[0].Expected[PortabilityReplayClient] = PortabilityExpectation{
		Valid:       true,
		Diagnostics: []PortabilityExpectedDiagnostic{},
	}
	results, err := ReplayPortabilityCorpus(corpus, root)
	if err == nil {
		t.Fatal("replay unexpectedly matched a changed expectation")
	}
	var replayErr *PortabilityReplayError
	if !errors.As(err, &replayErr) || len(replayErr.Mismatches) != 1 {
		t.Fatalf("replay error = %v", err)
	}
	if len(results) != 2 || results[0].Match {
		t.Fatalf("replay results = %#v", results)
	}
}

func testPortabilityCorpus() PortabilityCorpus {
	revision := strings.Repeat("a", 40)
	artifact := strings.Repeat("b", 64)
	return PortabilityCorpus{
		SchemaVersion: PortabilityCorpusSchemaVersion,
		Name:          "test-corpus",
		Clients: []PortabilityClient{
			{ID: "goskill", Name: "goskill", Implementation: "example.com/goskill", LocalReplay: true},
			{ID: "skills-ref", Name: "skills-ref", Implementation: "example.com/owner/repository/skills-ref", Revision: revision},
		},
		Cases: []PortabilityCase{
			{
				ID:          "lowercase-fixture",
				Description: "lowercase fixture",
				Fixture:     "fixture",
				Profile:     ProfilePortable,
				Expected: map[string]PortabilityExpectation{
					"goskill":    {Valid: true, Diagnostics: []PortabilityExpectedDiagnostic{}},
					"skills-ref": {Valid: true, Diagnostics: []PortabilityExpectedDiagnostic{}},
				},
				Evidence: []PortabilityEvidence{
					{Client: "skills-ref", Source: "https://example.com/owner/repository/tree/" + revision + "/skills-ref", Revision: revision, Method: "replay fixture", ArtifactSHA256: artifact},
				},
			},
		},
	}
}
