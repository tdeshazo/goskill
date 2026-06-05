package commands

import (
	"fmt"
	"strings"

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
	return renderInfo("Usage",
		selectorTitleStyle.Render("skills <command> [options]"),
		fmt.Sprintf("%s %s", selectorSuccessStyle.Render("commands:"), "add, list, remove, find, validate, check, update, init, install, experimental_sync"),
		fmt.Sprintf("%s %s", selectorSuccessStyle.Render("agents:"), "claude-code, codex, cursor"),
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

func renderFindResults(query string, results []foundSkill) string {
	if len(results) == 0 {
		return renderInfo("Find skills", selectorHintStyle.Render("No skills found for "+query))
	}
	lines := []string{
		selectorHintStyle.Render(fmt.Sprintf("%d result%s for %s", len(results), skillPlural(len(results)), query)),
		selectorBar(),
	}
	for _, skill := range results {
		lines = append(lines,
			fmt.Sprintf("%s %s", selectorSelected.Render("●"), selectorTitleStyle.Render(skill.Name)),
			fmt.Sprintf("  %s %s", selectorPathStyle.Render("source:"), skill.Source),
			fmt.Sprintf("  %s %d", selectorSuccessStyle.Render("installs:"), skill.Installs),
		)
	}
	return renderInfo("Find skills", lines...)
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
