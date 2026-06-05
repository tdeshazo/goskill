package commands

import (
	"sort"
	"strings"
)

func sortedFoundSkillsBySource(results []foundSkill) []foundSkill {
	sorted := append([]foundSkill(nil), results...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Source == sorted[j].Source {
			if sorted[i].Installs == sorted[j].Installs {
				return sorted[i].Name < sorted[j].Name
			}
			return sorted[i].Installs > sorted[j].Installs
		}
		return sorted[i].Source < sorted[j].Source
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

func findInstallCommand(skill foundSkill) string {
	if strings.TrimSpace(skill.Source) == "" {
		return "goskill add --skill " + shellQuote(skill.Name)
	}
	return "goskill add " + shellQuote(skill.Source) + " --skill " + shellQuote(skill.Name)
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
