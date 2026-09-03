package commands

import (
	"path"
	"sort"
	"strings"

	"github.com/tdeshazo/goskill/internal/search"
	"github.com/tdeshazo/goskill/internal/source"
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
	if configName, ok := skill.ProviderMetadata["well_known_config"].(string); ok && strings.TrimSpace(configName) != "" {
		return "goskill add " + shellQuote("wellknown:"+strings.TrimSpace(configName)+"@"+skill.Name)
	}
	if installURL := strings.TrimSpace(skill.InstallURL); installURL != "" {
		if parsed, err := source.Parse(installURL); err == nil && parsed.Type == source.SkillURL {
			return "goskill add " + shellQuote(installURL)
		}
	}

	skillSource := strings.TrimSpace(skill.CanonicalSource)
	if skillSource == "" {
		skillSource = strings.TrimSpace(skill.InstallURL)
	}
	if skillSource == "" {
		return "goskill add --skill " + shellQuote(skill.Name)
	}
	if githubSource, omitSkill := findGitHubSkillSource(skill, skillSource); githubSource != "" {
		command := "goskill add " + shellQuote(githubSource)
		if !omitSkill {
			command += " --skill " + shellQuote(skill.Name)
		}
		return command
	}
	if parsed, err := source.Parse(skillSource); err == nil && parsed.Type == source.SkillURL {
		return "goskill add " + shellQuote(skillSource)
	}
	return "goskill add " + shellQuote(skillSource) + " --skill " + shellQuote(skill.Name)
}

// findGitHubSkillSource scopes a GitHub result to its SKILL.md directory.
// GitHub code search may find skills with the same frontmatter name in a single
// repository, so a repository-only source would otherwise select the wrong one.
// Catalog sources without a branch use goskill's owner/repo/subpath shorthand;
// that directory identifies the skill, so their display name is not used as a
// selector.
func findGitHubSkillSource(skill search.SearchResult, skillSource string) (string, bool) {
	if strings.TrimSpace(skill.SkillPath) == "" {
		return "", false
	}
	parsed, err := source.Parse(skillSource)
	if err != nil || parsed.Type != source.GitHub {
		return "", false
	}
	skillPath := strings.TrimPrefix(skill.SkillPath, "/")
	if !strings.EqualFold(path.Base(skillPath), "SKILL.md") {
		return "", false
	}
	directory := path.Dir(skillPath)
	branch, _ := skill.ProviderMetadata["source_ref"].(string)
	if branch == "" {
		branch, _ = skill.ProviderMetadata["default_branch"].(string)
	}
	if branch == "" {
		branch = parsed.Ref
	}
	ownerRepo := source.OwnerRepo(parsed)
	if ownerRepo == "" {
		return "", false
	}
	if directory == "." || directory == "/" {
		if branch == "" {
			return "", false
		}
		return sourceWebURL("https://github.com/"+ownerRepo, "tree", branch, ""), false
	}
	directory, err = source.SanitizeSubpath(directory)
	if err != nil {
		return "", false
	}
	if branch == "" {
		return ownerRepo + "/" + directory, true
	}
	return sourceWebURL("https://github.com/"+ownerRepo, "tree", branch, directory), false
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
