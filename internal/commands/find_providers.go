package commands

import (
	"strings"

	"github.com/tdeshazo/goskill/internal/search"
)

func configuredSearchProviders() ([]search.SearchProvider, []search.ConfiguredProviderStatus, error) {
	configs, err := search.LoadConfiguredProviders()
	if err != nil {
		return []search.SearchProvider{}, []search.ConfiguredProviderStatus{}, err
	}
	providers, statuses := search.DefaultProviderRegistry().Build(configs, search.EnvironmentCredentialResolver{})
	return providers, statuses, nil
}

// FindProviders prints the capabilities and availability of built-in and
// explicitly configured optional providers without making network requests.
func (a App) FindProviders(options FindOptions) error {
	_, configured, configurationErr := configuredSearchProviders()
	statuses := builtInProviderStatuses(options)
	if configurationErr != nil {
		statuses = append(statuses, search.ConfiguredProviderStatus{
			Name:    "optional provider configuration",
			Enabled: false,
			Err:     configurationErr,
		})
	}
	statuses = append(statuses, configured...)
	a.writeOut(renderFindProviderStatuses(statuses))
	return nil
}

func builtInProviderStatuses(options FindOptions) []search.ConfiguredProviderStatus {
	return []search.ConfiguredProviderStatus{
		providerStatus("skills.sh", true, false, "public", "live", "none"),
		providerStatus("SkillMD", !envEnabled("GOSKILL_DISABLE_SKILLMD"), false, "public", "live", "none"),
		providerStatus("TrueFoundry", !envEnabled("GOSKILL_DISABLE_TRUEFOUNDRY_CATALOG"), false, "public", "cached", "none"),
		providerStatus("GitHub", options.Deep, false, "public", "live", "high"),
	}
}

func providerStatus(name string, enabled, authRequired bool, visibility, availability, cost string) search.ConfiguredProviderStatus {
	return search.ConfiguredProviderStatus{
		Name:    name,
		Kind:    strings.ToLower(name),
		Enabled: enabled,
		Descriptor: search.ProviderDescriptor{
			Kind: strings.ToLower(name),
			Capabilities: search.ProviderCapabilities{
				AuthRequired: authRequired,
				Visibility:   visibility,
				Availability: availability,
				DeepCost:     cost,
			},
		},
	}
}
