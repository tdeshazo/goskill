package commands

import (
	"sort"
	"strings"

	"github.com/tdeshazo/goskill/internal/search"
)

func sortedFoundSkillsBySource(results []search.SearchResult) []search.SearchResult {
	sorted := append([]search.SearchResult(nil), results...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].CanonicalSource == sorted[j].CanonicalSource {
			if sorted[i].Installs == sorted[j].Installs {
				return sorted[i].Name < sorted[j].Name
			}
			return sorted[i].Installs > sorted[j].Installs
		}
		return sorted[i].CanonicalSource < sorted[j].CanonicalSource
	})
	return sorted
}

func findSourceGroup(source string) string {
	source = strings.TrimSpace(source)
	if source == "" {
		return "Unknown source"
	}
	return source
}

func findInstallCommand(skill search.SearchResult) string {
	if strings.TrimSpace(skill.CanonicalSource) == "" {
		return "goskill add --skill " + shellQuote(skill.Name)
	}
	return "goskill add " + shellQuote(skill.CanonicalSource) + " --skill " + shellQuote(skill.Name)
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	if strings.IndexFunc(value, func(r rune) bool {
		return r <= ' ' || strings.ContainsRune("'\"$`\\|&;()<>*?![]{}~", r)
	}) < 0 {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
