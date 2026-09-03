package search

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseProviderConfigsValidatesConfigurationAndCredentials(t *testing.T) {
	configs, err := ParseProviderConfigs([]byte(`{
  "providers": [
    {
      "name": "acme",
      "kind": "well-known",
      "endpoint": "https://skills.acme.example",
      "enabled": false,
      "visibility": "private",
      "auth_required": true,
      "credential_env": "ACME_TOKEN"
    }
  ]
}`))
	if err != nil {
		t.Fatal(err)
	}
	want := []ProviderConfig{{
		Name:          "acme",
		Kind:          "well-known",
		Endpoint:      "https://skills.acme.example",
		CredentialEnv: "ACME_TOKEN",
		AuthRequired:  true,
		Visibility:    "private",
		Disabled:      true,
	}}
	if !reflect.DeepEqual(configs, want) {
		t.Fatalf("configs = %#v, want %#v", configs, want)
	}
	for _, data := range [][]byte{
		[]byte(`{"providers":[{"name":"bad","kind":"well-known","endpoint":"https://example.com","token":"secret"}]}`),
		[]byte(`{"providers":[{"name":"bad","kind":"well-known","endpoint":"https://example.com","api_key":"secret"}]}`),
		[]byte(`{"providers":[{"name":"bad","kind":"well-known","endpoint":"https://example.com","access_key":"secret"}]}`),
		[]byte(`{"providers":[{"name":"bad","kind":"well-known","endpoint":"https://example.com","private_key":"secret"}]}`),
		[]byte(`{"providers":[{"name":"bad","kind":"well-known","endpoint":"https://example.com","bearer":"secret"}]}`),
		[]byte(`{"providers":[{"name":"bad","kind":"well-known","endpoint":"https://example.com/index.json?token=secret"}]}`),
		[]byte(`{"providers":[{"name":"bad","kind":"well-known","endpoint":"https://example.com","visibility":"internal"}]}`),
	} {
		if _, err := ParseProviderConfigs(data); err == nil {
			t.Fatalf("ParseProviderConfigs(%s) succeeded", data)
		}
	}
}

func TestLoadConfiguredProvidersFromExplicitFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "providers.json")
	if err := os.WriteFile(path, []byte(`{"providers":[{"name":"acme","kind":"well-known","endpoint":"https://skills.acme.example"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOSKILL_PROVIDER_CONFIG", path)
	t.Setenv("GOSKILL_PROVIDER_CONFIG_JSON", "")
	configs, err := LoadConfiguredProviders()
	if err != nil {
		t.Fatal(err)
	}
	if len(configs) != 1 || configs[0].Name != "acme" || configs[0].Disabled {
		t.Fatalf("configs = %#v", configs)
	}
}

func TestProviderRegistrySkipsDisabledAndPrivateUnavailableProviders(t *testing.T) {
	called := false
	registry := NewProviderRegistry(ProviderRegistration{
		Descriptor: ProviderDescriptor{
			Kind: "private-test",
			Capabilities: ProviderCapabilities{
				AuthRequired: true,
				Visibility:   "private",
				Availability: "live",
				DeepCost:     "low",
			},
		},
		Factory: func(config ProviderConfig, credential string) (SearchProvider, error) {
			called = true
			return testProvider{name: config.Name, search: func(context.Context, SearchQuery) ([]SearchResult, error) {
				return []SearchResult{}, nil
			}}, nil
		},
	})
	providers, statuses := registry.Build([]ProviderConfig{
		{Name: "disabled", Kind: "private-test", Endpoint: "https://disabled.example", Disabled: true},
		{Name: "private", Kind: "private-test", Endpoint: "https://private.example", CredentialEnv: "PRIVATE_TOKEN"},
		{Name: "unknown", Kind: "future-provider", Endpoint: "https://unknown.example"},
	}, staticCredentialResolver{})
	if called || len(providers) != 0 || len(statuses) != 3 {
		t.Fatalf("called=%t providers=%#v statuses=%#v", called, providers, statuses)
	}
	if statuses[0].Enabled || statuses[0].Err != nil {
		t.Fatalf("disabled status = %#v", statuses[0])
	}
	if statuses[1].Err == nil || statuses[1].Descriptor.Capabilities.Visibility != "private" {
		t.Fatalf("private status = %#v", statuses[1])
	}
	if statuses[2].Err == nil {
		t.Fatalf("unknown status = %#v", statuses[2])
	}
	if errors.Is(statuses[1].Err, context.Canceled) {
		t.Fatalf("private configuration error = %v", statuses[1].Err)
	}
}

func TestWellKnownProviderParsesConfiguredV2Endpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/agent-skills/index.json" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("authorization = %q", got)
		}
		_, _ = w.Write([]byte(`{
  "$schema": "https://schemas.agentskills.io/discovery/0.2.0/schema.json",
  "skills": [
    {"name":"web-design","description":"Build accessible web interfaces","type":"skill-md","url":"artifacts/web/SKILL.md"},
    {"name":"other","description":"Other skill","type":"skill-md","url":"artifacts/other/SKILL.md"}
  ]
}`))
	}))
	t.Cleanup(server.Close)
	provider := NewWellKnownProvider(WellKnownProviderOptions{
		Name:     "acme",
		Endpoint: server.URL,
		Token:    "test-token",
		Client:   server.Client(),
	})
	results, err := provider.Search(context.Background(), SearchQuery{Text: "web", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %#v", results)
	}
	result := results[0]
	if result.Provider != "acme" || result.Name != "web-design" || result.InstallURL != server.URL+"/.well-known/agent-skills/artifacts/web/SKILL.md" {
		t.Fatalf("result = %#v", result)
	}
	if result.ProviderMetadata["artifact_type"] != "skill-md" {
		t.Fatalf("metadata = %#v", result.ProviderMetadata)
	}
}

func TestWellKnownProviderParsesV1CompatibilityIndex(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/agent-skills/index.json":
			w.WriteHeader(http.StatusNotFound)
		case "/.well-known/skills/index.json":
			_, _ = w.Write([]byte(`{"skills":[{"name":"legacy","description":"Legacy well-known skill","files":["SKILL.md"]}]}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)
	results, err := NewWellKnownProvider(WellKnownProviderOptions{
		Name:     "legacy-registry",
		Endpoint: server.URL,
		Client:   server.Client(),
	}).Search(context.Background(), SearchQuery{Text: "legacy"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].InstallURL != server.URL || results[0].CanonicalSource != server.URL {
		t.Fatalf("results = %#v", results)
	}
}

func TestWellKnownIndexURLParsingIsBounded(t *testing.T) {
	tests := []struct {
		endpoint string
		want     []string
	}{
		{
			endpoint: "https://skills.example",
			want: []string{
				"https://skills.example/.well-known/agent-skills/index.json",
				"https://skills.example/.well-known/skills/index.json",
			},
		},
		{
			endpoint: "https://skills.example/custom/index.json",
			want:     []string{"https://skills.example/custom/index.json"},
		},
	}
	for _, test := range tests {
		got, err := wellKnownIndexURLs(test.endpoint)
		if err != nil || !reflect.DeepEqual(got, test.want) {
			t.Fatalf("wellKnownIndexURLs(%q) = %#v, %v", test.endpoint, got, err)
		}
	}
	if _, err := wellKnownIndexURLs("https://token@skills.example"); err == nil {
		t.Fatal("endpoint containing credentials was accepted")
	}
	for _, endpoint := range []string{
		"https://skills.example/index.json?token=secret",
		"https://skills.example/index.json#fragment",
	} {
		if _, err := wellKnownIndexURLs(endpoint); err == nil {
			t.Fatalf("endpoint %q was accepted", endpoint)
		}
	}
}

func TestWellKnownArtifactURLsRejectCredentialBearingReferences(t *testing.T) {
	indexURL := "https://skills.example/.well-known/agent-skills/index.json"
	for _, artifactURL := range []string{
		"https://user@skills.example/private/SKILL.md",
		"artifacts/SKILL.md?token=secret",
		"artifacts/SKILL.md#secret",
	} {
		entries, err := parseWellKnownIndex([]byte(`{"skills":[{"name":"unsafe","url":"`+artifactURL+`"}]}`), indexURL)
		if err != nil || len(entries) != 0 {
			t.Fatalf("artifact %q produced entries %#v, err %v", artifactURL, entries, err)
		}
	}
	if _, err := resolveWellKnownURL(indexURL+"?token=secret", "artifacts/SKILL.md"); err == nil {
		t.Fatal("credential-bearing resolved URL was accepted")
	}
}

func TestWellKnownProviderPreservesV1EndpointBasePathAndSkipsArchives(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/tenant/.well-known/agent-skills/index.json":
			w.WriteHeader(http.StatusNotFound)
		case "/tenant/.well-known/skills/index.json":
			_, _ = w.Write([]byte(`{"skills":[{"name":"legacy","description":"Legacy","files":["SKILL.md"]}]}`))
		case "/v2/.well-known/agent-skills/index.json":
			_, _ = w.Write([]byte(`{"skills":[{"name":"archive","type":"archive","url":"artifacts/archive.zip"},{"name":"direct","type":"skill-md","url":"artifacts/direct/SKILL.md"}]}`))
		default:
			t.Fatalf("path = %q", r.URL.Path)
		}
	}))
	defer server.Close()

	v1, err := NewWellKnownProvider(WellKnownProviderOptions{Name: "v1", Endpoint: server.URL + "/tenant", Client: server.Client()}).Search(context.Background(), SearchQuery{Text: "legacy"})
	if err != nil || len(v1) != 1 || v1[0].InstallURL != server.URL+"/tenant" || v1[0].CanonicalSource != server.URL+"/tenant" {
		t.Fatalf("v1 results = %#v, err = %v", v1, err)
	}
	v2, err := NewWellKnownProvider(WellKnownProviderOptions{Name: "v2", Endpoint: server.URL + "/v2", Client: server.Client()}).Search(context.Background(), SearchQuery{})
	if err != nil || len(v2) != 1 || v2[0].Name != "direct" {
		t.Fatalf("v2 results = %#v, err = %v", v2, err)
	}
}

func TestPrivateWellKnownArtifactRejectsOtherOriginsBeforeSendingCredentials(t *testing.T) {
	artifactServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("artifact origin must not be contacted")
	}))
	defer artifactServer.Close()
	indexServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"skills":[{"name":"private","type":"skill-md","url":"` + artifactServer.URL + `/SKILL.md"}]}`))
	}))
	defer indexServer.Close()

	provider := NewWellKnownProvider(WellKnownProviderOptions{
		Name:          "private",
		Endpoint:      indexServer.URL,
		Token:         "never-send-this-token",
		CredentialEnv: "PRIVATE_TOKEN",
		Private:       true,
		Client:        indexServer.Client(),
	})
	results, err := provider.Search(context.Background(), SearchQuery{Text: "private"})
	if err != nil || len(results) != 1 {
		t.Fatalf("results = %#v, err = %v", results, err)
	}
	if _, err := provider.FetchSkill(results[0]); err == nil {
		t.Fatal("cross-origin private artifact was accepted")
	}
}

type staticCredentialResolver struct{}

func (staticCredentialResolver) Lookup(string) (string, bool) {
	return "", false
}
