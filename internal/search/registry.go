package search

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// ProviderCapabilities describe operational characteristics without coupling
// the aggregator to a provider's implementation. They make opt-in, private,
// cached, and expensive future providers visible to callers before a search.
type ProviderCapabilities struct {
	AuthRequired bool
	Visibility   string // "public" or "private"
	Availability string // "live" or "cached"
	DeepCost     string // "none", "low", "medium", or "high"
}

// ProviderDescriptor identifies a provider kind and its capabilities.
type ProviderDescriptor struct {
	Kind         string
	Capabilities ProviderCapabilities
}

// ProviderConfig is an explicitly configured optional registry. Credentials
// are intentionally represented only by CredentialEnv; raw tokens are
// rejected while loading configuration.
type ProviderConfig struct {
	Name          string `json:"name"`
	Kind          string `json:"kind"`
	Endpoint      string `json:"endpoint"`
	CredentialEnv string `json:"credential_env,omitempty"`
	AuthRequired  bool   `json:"auth_required,omitempty"`
	Visibility    string `json:"visibility,omitempty"`
	Disabled      bool   `json:"-"`
}

// ProviderCredentialResolver allows environment credentials today and cloud
// identity resolvers later without putting credentials in configuration.
type ProviderCredentialResolver interface {
	Lookup(name string) (string, bool)
}

// EnvironmentCredentialResolver reads one named credential from the process
// environment. It never returns the value in an error or status.
type EnvironmentCredentialResolver struct{}

func (EnvironmentCredentialResolver) Lookup(name string) (string, bool) {
	value := strings.TrimSpace(os.Getenv(name))
	return value, value != ""
}

// ProviderFactory constructs one configured provider. credential is supplied
// only by a resolver, never by the configuration file.
type ProviderFactory func(ProviderConfig, string) (SearchProvider, error)

// ProviderRegistration makes a new provider kind available without requiring
// any aggregator change.
type ProviderRegistration struct {
	Descriptor ProviderDescriptor
	Factory    ProviderFactory
}

// ProviderRegistry is a small registry of optional-provider constructors.
type ProviderRegistry struct {
	registrations map[string]ProviderRegistration
}

// NewProviderRegistry creates a registry from registrations. Later duplicate
// kinds replace earlier ones, which permits applications to override a kind in
// tests or enterprise builds.
func NewProviderRegistry(registrations ...ProviderRegistration) ProviderRegistry {
	registry := ProviderRegistry{registrations: map[string]ProviderRegistration{}}
	for _, registration := range registrations {
		registry.Register(registration)
	}
	return registry
}

// Register adds a provider kind. Invalid registrations are ignored so an
// optional extension can never make regular find searches fail at startup.
func (r ProviderRegistry) Register(registration ProviderRegistration) {
	kind := strings.ToLower(strings.TrimSpace(registration.Descriptor.Kind))
	if kind == "" || registration.Factory == nil {
		return
	}
	registration.Descriptor.Kind = kind
	r.registrations[kind] = registration
}

// ConfiguredProviderStatus records optional-provider configuration outcomes.
// Errors are intentionally generic and never contain credential values.
type ConfiguredProviderStatus struct {
	Name       string
	Kind       string
	Enabled    bool
	Descriptor ProviderDescriptor
	Err        error
}

// Build constructs enabled configured providers. Unknown, disabled, and
// unavailable providers are reported per provider and do not prevent other
// configured providers from participating in a federated search.
func (r ProviderRegistry) Build(configs []ProviderConfig, credentials ProviderCredentialResolver) ([]SearchProvider, []ConfiguredProviderStatus) {
	providers := make([]SearchProvider, 0, len(configs))
	statuses := make([]ConfiguredProviderStatus, 0, len(configs))
	if credentials == nil {
		credentials = EnvironmentCredentialResolver{}
	}
	for _, config := range configs {
		kind := strings.ToLower(strings.TrimSpace(config.Kind))
		name := strings.TrimSpace(config.Name)
		if name == "" {
			name = kind
		}
		registration, ok := r.registrations[kind]
		status := ConfiguredProviderStatus{
			Name:       name,
			Kind:       kind,
			Enabled:    !config.Disabled,
			Descriptor: ProviderDescriptor{Kind: kind},
		}
		if ok {
			status.Descriptor = registration.Descriptor
		}
		if config.AuthRequired {
			status.Descriptor.Capabilities.AuthRequired = true
		}
		if config.Visibility == "public" || config.Visibility == "private" {
			status.Descriptor.Capabilities.Visibility = config.Visibility
		}
		if config.Disabled {
			statuses = append(statuses, status)
			continue
		}
		if !ok {
			status.Err = errors.New("unknown optional provider kind")
			statuses = append(statuses, status)
			continue
		}

		credential := ""
		if status.Descriptor.Capabilities.AuthRequired {
			credentialName := strings.TrimSpace(config.CredentialEnv)
			if credentialName == "" {
				status.Err = errors.New("credential environment variable is not configured")
				statuses = append(statuses, status)
				continue
			}
			var found bool
			credential, found = credentials.Lookup(credentialName)
			if !found {
				status.Err = errors.New("credential is unavailable")
				statuses = append(statuses, status)
				continue
			}
		}
		provider, err := registration.Factory(config, credential)
		if err != nil {
			status.Err = errors.New("optional provider is unavailable")
			statuses = append(statuses, status)
			continue
		}
		providers = append(providers, provider)
		statuses = append(statuses, status)
	}
	return providers, statuses
}

// DefaultProviderRegistry exposes the bounded well-known endpoint adapter.
// Future Google, GitLab, and enterprise registrations can be added here or by
// an application without modifying Aggregator.
func DefaultProviderRegistry() ProviderRegistry {
	return NewProviderRegistry(ProviderRegistration{
		Descriptor: ProviderDescriptor{
			Kind: "well-known",
			Capabilities: ProviderCapabilities{
				Visibility:   "public",
				Availability: "live",
				DeepCost:     "low",
			},
		},
		Factory: func(config ProviderConfig, credential string) (SearchProvider, error) {
			if _, err := wellKnownIndexURLs(config.Endpoint); err != nil {
				return nil, err
			}
			return NewWellKnownProvider(WellKnownProviderOptions{
				Name:          config.Name,
				Endpoint:      config.Endpoint,
				Token:         credential,
				CredentialEnv: config.CredentialEnv,
				Private:       config.AuthRequired || config.Visibility == "private",
			}), nil
		},
	})
}

// LoadConfiguredProviders loads optional provider configuration from an inline
// environment value, an explicit file path, or the user config directory. A
// missing default file simply means no optional providers are enabled.
func LoadConfiguredProviders() ([]ProviderConfig, error) {
	if inline := strings.TrimSpace(os.Getenv("GOSKILL_PROVIDER_CONFIG_JSON")); inline != "" {
		return ParseProviderConfigs([]byte(inline))
	}
	path := strings.TrimSpace(os.Getenv("GOSKILL_PROVIDER_CONFIG"))
	if path == "" {
		configDir, err := os.UserConfigDir()
		if err != nil {
			return []ProviderConfig{}, nil
		}
		path = filepath.Join(configDir, "goskill", "providers.json")
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return []ProviderConfig{}, nil
	}
	if err != nil {
		return nil, errors.New("read optional provider configuration")
	}
	return ParseProviderConfigs(data)
}

// ParseProviderConfigs validates the small JSON configuration format. Its
// strict credential check keeps accidental secrets out of config files, status
// output, and process diagnostics.
func ParseProviderConfigs(data []byte) ([]ProviderConfig, error) {
	var file struct {
		Providers []json.RawMessage `json:"providers"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, errors.New("decode optional provider configuration")
	}
	configs := make([]ProviderConfig, 0, len(file.Providers))
	for _, raw := range file.Providers {
		if containsInlineCredential(raw) {
			return nil, errors.New("optional provider configuration must not contain inline credentials")
		}
		var decoded struct {
			Name          string `json:"name"`
			Kind          string `json:"kind"`
			Endpoint      string `json:"endpoint"`
			CredentialEnv string `json:"credential_env"`
			AuthRequired  bool   `json:"auth_required"`
			Visibility    string `json:"visibility"`
			Enabled       *bool  `json:"enabled"`
		}
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return nil, errors.New("decode optional provider configuration")
		}
		if strings.TrimSpace(decoded.Name) == "" || strings.TrimSpace(decoded.Kind) == "" || strings.TrimSpace(decoded.Endpoint) == "" {
			return nil, errors.New("optional provider configuration requires name, kind, and endpoint")
		}
		if endpointContainsCredential(decoded.Endpoint) {
			return nil, errors.New("optional provider endpoint must not contain credentials")
		}
		visibility := strings.ToLower(strings.TrimSpace(decoded.Visibility))
		if visibility != "" && visibility != "public" && visibility != "private" {
			return nil, errors.New("optional provider visibility must be public or private")
		}
		disabled := decoded.Enabled != nil && !*decoded.Enabled
		configs = append(configs, ProviderConfig{
			Name:          strings.TrimSpace(decoded.Name),
			Kind:          strings.ToLower(strings.TrimSpace(decoded.Kind)),
			Endpoint:      strings.TrimSpace(decoded.Endpoint),
			CredentialEnv: strings.TrimSpace(decoded.CredentialEnv),
			AuthRequired:  decoded.AuthRequired,
			Visibility:    visibility,
			Disabled:      disabled,
		})
	}
	return configs, nil
}

func endpointContainsCredential(raw string) bool {
	endpoint, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return true
	}
	return false
}

func containsInlineCredential(raw json.RawMessage) bool {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return false
	}
	return containsSensitiveKey(value)
}

func containsSensitiveKey(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			name := strings.ToLower(strings.ReplaceAll(key, "-", "_"))
			if name != "credential_env" && sensitiveConfigKey(name) {
				return true
			}
			if containsSensitiveKey(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if containsSensitiveKey(child) {
				return true
			}
		}
	}
	return false
}

func sensitiveConfigKey(name string) bool {
	compact := strings.ReplaceAll(name, "_", "")
	for _, marker := range []string{
		"token", "secret", "password", "authorization", "credential",
		"apikey", "accesskey", "privatekey", "bearer",
	} {
		if strings.Contains(compact, marker) {
			return true
		}
	}
	return false
}

// ProviderStatusError converts an optional-provider setup failure to the same
// status shape used by federated provider searches.
func (s ConfiguredProviderStatus) ProviderStatusError() error {
	if s.Err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", s.Name, s.Err)
}
