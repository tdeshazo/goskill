package commands

import (
	"fmt"
	"strings"

	"github.com/tdeshazo/goskill/internal/agents"
	"github.com/tdeshazo/goskill/internal/installer"
)

func renderSkillList(list []installer.Installed, cwd string) string {
	lines := []string{
		selectorActiveStyle.Render("◆") + "  " + selectorTitleStyle.Render("Installed skills"),
		selectorBar(),
	}

	if len(list) == 0 {
		lines = append(lines,
			fmt.Sprintf("%s  %s", selectorBar(), selectorHintStyle.Render("No skills found.")),
			selectorBarStyle.Render("└"),
		)
		return strings.Join(lines, "\n") + "\n"
	}

	lines = append(lines,
		fmt.Sprintf("%s  %s", selectorBar(), selectorHintStyle.Render(fmt.Sprintf("%d skill%s installed", len(list), skillPlural(len(list))))),
		selectorBar(),
	)

	lastScope := ""
	for _, item := range list {
		if item.Scope != lastScope {
			lines = append(lines, selectorGroupLine(titleCase(item.Scope), 88))
			lastScope = item.Scope
		}
		lines = append(lines, renderSkillListItem(item, cwd)...)
	}

	lines = append(lines, selectorBarStyle.Render("└"))
	return strings.Join(lines, "\n") + "\n"
}

func renderSkillListItem(item installer.Installed, cwd string) []string {
	agentsText := installedAgentsText(item.Agents)
	lines := []string{
		fmt.Sprintf("%s %s %s", selectorBar(), selectorSelected.Render("●"), selectorTitleStyle.Render(item.Name)),
	}
	if item.Description != "" {
		lines = append(lines, fmt.Sprintf("%s   %s", selectorBar(), selectorHintStyle.Render(item.Description)))
	}
	if agentsText != "" {
		lines = append(lines, fmt.Sprintf("%s   %s %s", selectorBar(), selectorSuccessStyle.Render("agents:"), agentsText))
	}
	lines = append(lines, fmt.Sprintf("%s   %s %s", selectorBar(), selectorPathStyle.Render("path:"), shorten(item.CanonicalPath, cwd)))
	return lines
}

func installedAgentsText(installed []agents.Type) string {
	names := make([]string, 0, len(installed))
	for _, agent := range installed {
		names = append(names, agents.Display(agent))
	}
	return strings.Join(names, ", ")
}
