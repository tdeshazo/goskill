package commands

import (
	"encoding/json"
	"strings"

	"github.com/tdeshazo/goskill/internal/search"
)

type findJSONResponse struct {
	Results   []search.SearchResult    `json:"results"`
	Providers []findJSONProviderStatus `json:"providers"`
}

type findJSONProviderStatus struct {
	Provider string `json:"provider"`
	Error    string `json:"error,omitempty"`
}

// renderFindJSON intentionally bypasses the terminal renderer: JSON is a
// machine-readable contract and must never contain ANSI control sequences.
func renderFindJSON(results []search.SearchResult, statuses []search.ProviderStatus) (string, error) {
	providerStatuses := make([]findJSONProviderStatus, 0, len(statuses))
	for _, status := range statuses {
		value := findJSONProviderStatus{Provider: status.Provider}
		if status.Err != nil {
			value.Error = status.Err.Error()
		}
		providerStatuses = append(providerStatuses, value)
	}
	encoded, err := json.MarshalIndent(findJSONResponse{
		Results:   results,
		Providers: providerStatuses,
	}, "", "  ")
	if err != nil {
		return "", err
	}
	return string(encoded) + "\n", nil
}

// writeProviderStatusIfMaterial avoids turning ordinary partial outages into
// noisy output while still explaining an empty search that may be incomplete.
func (a App) writeProviderStatusIfMaterial(statuses []search.ProviderStatus) {
	failed := make([]string, 0, len(statuses))
	for _, status := range statuses {
		if status.Err != nil && strings.TrimSpace(status.Provider) != "" {
			failed = append(failed, status.Provider)
		}
	}
	if len(failed) == 0 {
		return
	}
	a.writeErr(renderWarning("Search incomplete", "Unavailable providers: "+strings.Join(failed, ", ")+"."))
}
