package commands

import (
	"fmt"
	"strings"

	"github.com/tdeshazo/goskill/internal/search"
	"github.com/tdeshazo/goskill/internal/skills"
)

type statusKind string

const (
	statusInfo    statusKind = "info"
	statusSuccess statusKind = "success"
	statusWarning statusKind = "warning"
	statusError   statusKind = "error"
)

type validationResult struct {
	Path   string
	Issues []skills.ValidationIssue
}

func renderStatus(title string, lines []string, kind statusKind) string {
	style := selectorActiveStyle
	switch kind {
	case statusSuccess:
		style = selectorSuccessStyle
	case statusWarning:
		style = selectorWarningStyle
	case statusError:
		style = selectorCancelStyle
	}

	out := []string{
		style.Render("◆") + "  " + selectorTitleStyle.Render(title),
	}
	for _, line := range lines {
		out = append(out, fmt.Sprintf("%s  %s", selectorBar(), line))
	}
	out = append(out, selectorBarStyle.Render("└"))
	return strings.Join(out, "\n") + "\n"
}

func renderSuccess(title string, lines ...string) string {
	return renderStatus(title, lines, statusSuccess)
}

func renderInfo(title string, lines ...string) string {
	return renderStatus(title, lines, statusInfo)
}

func renderWarning(title string, lines ...string) string {
	return renderStatus(title, lines, statusWarning)
}

func renderError(err error) string {
	return renderStatus("Error", []string{err.Error()}, statusError)
}

func RenderError(err error) string {
	return renderError(err)
}

func renderVersionOutput(version string) string {
	return renderInfo("goskill", selectorSuccessStyle.Bold(true).Render(version))
}

func renderBanner() string {
	lines := []string{
		selectorHintStyle.Render("The open agent skills ecosystem"),
		selectorBar(),
		fmt.Sprintf("%s %s", selectorSuccessStyle.Render("add"), "Install skills from a source"),
		fmt.Sprintf("%s %s", selectorSuccessStyle.Render("use"), "Use one skill without installing"),
		fmt.Sprintf("%s %s", selectorSuccessStyle.Render("list"), "List installed skills"),
		fmt.Sprintf("%s %s", selectorSuccessStyle.Render("remove"), "Remove installed skills"),
		fmt.Sprintf("%s %s", selectorSuccessStyle.Render("find"), "Search the skills API"),
		fmt.Sprintf("%s %s", selectorSuccessStyle.Render("validate"), "Validate SKILL.md files"),
		fmt.Sprintf("%s %s", selectorSuccessStyle.Render("check"), "Check locked skills for updates"),
		fmt.Sprintf("%s %s", selectorSuccessStyle.Render("update"), "Update locked skills"),
		fmt.Sprintf("%s %s", selectorSuccessStyle.Render("init"), "Create a SKILL.md template"),
	}
	return renderInfo("skills", lines...)
}

func renderHelp() string {
	commands := "add, use, list, remove, find, validate, check, update, init, " +
		"install, experimental_sync"
	return renderInfo("Usage",
		selectorTitleStyle.Render("skills <command> [options]"),
		fmt.Sprintf("%s %s", selectorSuccessStyle.Render("commands:"), commands),
		fmt.Sprintf("%s %s", selectorSuccessStyle.Render("agents:"), "claude-code, codex, cursor"),
	)
}

func renderFindHelp() string {
	return renderInfo("Find skills",
		selectorTitleStyle.Render("goskill find [options] <query>"),
		selectorHintStyle.Render("Search enabled registries, then deduplicate, filter, and rank results."),
		selectorBar(),
		"--deep                 Include bounded GitHub long-tail discovery",
		"--refresh              Refresh static catalogs before searching",
		"--verified             Keep verified or positively audited results",
		"--provider <name>      Keep results contributed by one provider",
		"--sort <mode>          relevance (default), popular, or newest",
		"--json                 Write normalized ANSI-free JSON",
		"--providers            List provider capabilities and availability",
	)
}

func renderSkillDiscoveryList(list []skills.Skill, title string) string {
	lines := []string{
		selectorActiveStyle.Render("◆") + "  " + selectorTitleStyle.Render(title),
		selectorBar(),
		selectorHintStyle.Render(fmt.Sprintf("%d skill%s found", len(list), skillPlural(len(list)))),
		selectorBar(),
	}
	lastGroup := ""
	for _, skill := range sortedSkillsByGroup(list) {
		group := skillGroup(skill)
		if group != lastGroup {
			if lastGroup != "" {
				lines = append(lines, selectorBar())
			}
			lines = append(lines, selectorGroupLine(titleCase(group), 88))
			lastGroup = group
		}
		lines = append(lines, fmt.Sprintf("%s %s %s", selectorBar(), selectorSelected.Render("●"), selectorTitleStyle.Render(skill.Name)))
		if skill.Description != "" {
			lines = append(lines, fmt.Sprintf("%s   %s", selectorBar(), selectorHintStyle.Render(skill.Description)))
		}
	}
	lines = append(lines, selectorBarStyle.Render("└"))
	return strings.Join(lines, "\n") + "\n"
}

func renderSkillSelectionPrompt(discovered []skills.Skill) string {
	lines := []string{
		selectorHintStyle.Render("Multiple skills found"),
		selectorBar(),
	}
	for i, skill := range discovered {
		line := fmt.Sprintf("%s %s", selectorSelected.Render(fmt.Sprintf("%d.", i+1)), selectorTitleStyle.Render(skill.Name))
		if skill.Description != "" {
			line += " " + selectorHintStyle.Render("- "+skill.Description)
		}
		lines = append(lines, line)
	}
	lines = append(lines,
		selectorBar(),
		selectorSummaryStyle.Render("Select skills to install")+" "+selectorHintStyle.Render("(numbers, names, comma-separated, or '*' for all): "),
	)
	return renderInfo("Select skills", lines...)
}

func renderFindResults(query string, results []search.SearchResult) string {
	if len(results) == 0 {
		return renderInfo("Find skills", selectorHintStyle.Render("No skills found for "+query))
	}
	lines := []string{
		selectorHintStyle.Render(fmt.Sprintf("%d result%s for %s", len(results), skillPlural(len(results)), query)),
		selectorBar(),
	}
	for _, skill := range results {
		source := findSourceGroup(skill.CanonicalSource)
		provider := providerProvenanceLabel(skill)
		identity := selectorHintStyle.Render(source)
		if provider != "" {
			identity += " " + selectorDimStyle.Render("["+provider+"]")
		}
		signals := findSignalLabel(skill)
		if signals != "" {
			signals = " " + selectorHintStyle.Render("("+signals+")")
		}
		lines = append(lines,
			fmt.Sprintf("%s %s %s%s", selectorSelected.Render("●"), selectorTitleStyle.Render(skill.Name), identity, signals),
		)
		if skill.Description != "" {
			lines = append(lines, "  "+selectorHintStyle.Render(skill.Description))
		}
		lines = append(lines,
			"  "+selectorDimStyle.Render(findInstallCommand(skill)),
		)
	}
	return renderInfo("Find skills", lines...)
}

func findSignalLabel(skill search.SearchResult) string {
	signals := []string{}
	if skill.Installs > 0 {
		signals = append(signals, fmt.Sprintf("%d install%s", skill.Installs, skillPlural(skill.Installs)))
	}
	if skill.Stars > 0 {
		signals = append(signals, fmt.Sprintf("%d stars", skill.Stars))
	}
	if skill.Rating > 0 {
		signals = append(signals, fmt.Sprintf("%.1f rating", skill.Rating))
	}
	if skill.VerificationStatus != "" {
		signals = append(signals, skill.VerificationStatus)
	}
	if skill.AuditStatus != "" {
		signals = append(signals, "audit "+skill.AuditStatus)
	}
	if !skill.UpdatedAt.IsZero() {
		signals = append(signals, "updated "+skill.UpdatedAt.UTC().Format("2006-01-02"))
	}
	return strings.Join(signals, " · ")
}

func renderFindProviderStatuses(statuses []search.ConfiguredProviderStatus) string {
	lines := make([]string, 0, len(statuses))
	for _, status := range statuses {
		capabilities := status.Descriptor.Capabilities
		auth := "no auth required"
		if capabilities.AuthRequired {
			auth = "auth required"
		}
		availability := firstNonEmpty(capabilities.Availability, "live")
		visibility := firstNonEmpty(capabilities.Visibility, "public")
		cost := firstNonEmpty(capabilities.DeepCost, "none")
		state := "enabled"
		if !status.Enabled {
			state = "disabled"
		}
		if status.Err != nil {
			state = "unavailable"
		}
		line := fmt.Sprintf("%s %s %s · %s · %s · %s · %s",
			selectorSelected.Render("●"),
			selectorTitleStyle.Render(status.Name),
			selectorHintStyle.Render(visibility),
			selectorHintStyle.Render(availability),
			selectorHintStyle.Render("deep cost "+cost),
			selectorHintStyle.Render(auth),
			selectorHintStyle.Render(state),
		)
		if status.Err != nil {
			line += " " + selectorWarningStyle.Render("("+status.Err.Error()+")")
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		lines = append(lines, selectorHintStyle.Render("No optional providers configured."))
	}
	return renderInfo("Find providers", lines...)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func providerProvenanceLabel(skill search.SearchResult) string {
	providers := append([]string(nil), skill.Providers...)
	if len(providers) == 0 && skill.Provider != "" {
		providers = []string{skill.Provider}
	}
	labels := make([]string, 0, len(providers))
	for _, provider := range providers {
		switch provider {
		case "skillmd":
			labels = append(labels, "SkillMD")
		case "github":
			labels = append(labels, "GitHub")
		case "truefoundry":
			labels = append(labels, "TrueFoundry")
		default:
			labels = append(labels, provider)
		}
	}
	return strings.Join(labels, " · ")
}

func renderValidationResults(results []validationResult, total int, issueCount int, cwd string) string {
	lines := []string{}
	for _, result := range results {
		path := shorten(result.Path, cwd)
		if len(result.Issues) == 0 {
			lines = append(lines, fmt.Sprintf("%s %s %s", selectorSuccessStyle.Render("●"), selectorTitleStyle.Render(path), selectorSuccessStyle.Render("OK")))
			continue
		}
		lines = append(lines, fmt.Sprintf("%s %s", selectorWarningStyle.Render("●"), selectorTitleStyle.Render(path)))
		for _, issue := range result.Issues {
			lines = append(lines, "  "+selectorWarningStyle.Render(issue.Message))
		}
	}
	if issueCount > 0 {
		return renderWarning("Validation failed", lines...)
	}
	lines = append(lines, selectorSuccessStyle.Render(fmt.Sprintf("Validated %d skill(s): OK", total)))
	return renderSuccess("Validation", lines...)
}
