package commands

import (
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type findSelectionModel struct {
	query     string
	results   []foundSkill
	selected  map[int]bool
	cursor    int
	filter    string
	width     int
	cancelled bool
	done      bool
}

func (a App) selectFindResultsInteractive(query string, results []foundSkill) ([]foundSkill, error) {
	model := newFindSelectionModel(query, results)
	result, err := tea.NewProgram(model).Run()
	if err != nil {
		return nil, err
	}
	finalModel, ok := result.(findSelectionModel)
	if !ok {
		if pointerModel, ok := result.(*findSelectionModel); ok {
			finalModel = *pointerModel
		} else {
			return nil, errInteractiveUnavailable
		}
	}
	if finalModel.cancelled {
		return nil, nil
	}
	selected := finalModel.selectedResults()
	if len(selected) == 0 {
		return nil, fmt.Errorf("no skills selected")
	}
	return selected, nil
}

func newFindSelectionModel(query string, results []foundSkill) findSelectionModel {
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
	return findSelectionModel{
		query:    query,
		results:  sorted,
		selected: map[int]bool{},
		width:    88,
	}
}

func (m findSelectionModel) Init() tea.Cmd {
	return nil
}

func (m findSelectionModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "ctrl+c":
			m.cancelled = true
			return m, tea.Quit
		case "up":
			m.moveCursor(-1)
		case "down":
			m.moveCursor(1)
		case "space":
			m.toggleCurrent()
		case "enter":
			if m.selectedCount() > 0 {
				m.done = true
				return m, tea.Quit
			}
		case "backspace":
			if len(m.filter) > 0 {
				m.filter = m.filter[:len(m.filter)-1]
				m.cursor = 0
			}
		default:
			text := msg.Key().Text
			if text != "" {
				m.filter += text
				m.cursor = 0
			}
		}
	}
	return m, nil
}

func (m findSelectionModel) View() tea.View {
	if m.cancelled {
		return tea.NewView(selectorCancelStyle.Render("■") + "  " + selectorTitleStyle.Render("Find skills") + "\n" +
			fmt.Sprintf("%s  %s\n", selectorBar(), selectorHintStyle.Render("Cancelled")))
	}
	if m.done {
		return tea.NewView(m.renderSubmitted())
	}
	return tea.NewView(m.renderActive())
}

func (m *findSelectionModel) moveCursor(delta int) {
	filtered := m.filtered()
	if len(filtered) == 0 {
		m.cursor = 0
		return
	}
	m.cursor = clampInt(m.cursor+delta, 0, len(filtered)-1)
}

func (m *findSelectionModel) toggleCurrent() {
	filtered := m.filtered()
	if len(filtered) == 0 {
		return
	}
	idx := filtered[m.cursor]
	if m.selected[idx] {
		delete(m.selected, idx)
		return
	}
	m.selected[idx] = true
}

func (m findSelectionModel) selectedResult() *foundSkill {
	filtered := m.filtered()
	if len(filtered) == 0 || m.cursor >= len(filtered) {
		return nil
	}
	result := m.results[filtered[m.cursor]]
	return &result
}

func (m findSelectionModel) selectedResults() []foundSkill {
	indices := m.selectedIndices()
	selected := make([]foundSkill, 0, len(indices))
	for _, idx := range indices {
		selected = append(selected, m.results[idx])
	}
	return selected
}

func (m findSelectionModel) selectedIndices() []int {
	indices := make([]int, 0, len(m.selected))
	for idx := range m.selected {
		indices = append(indices, idx)
	}
	sort.Slice(indices, func(i, j int) bool {
		a := m.results[indices[i]]
		b := m.results[indices[j]]
		if a.Source == b.Source {
			if a.Installs == b.Installs {
				return a.Name < b.Name
			}
			return a.Installs > b.Installs
		}
		return a.Source < b.Source
	})
	return indices
}

func (m findSelectionModel) selectedNames() []string {
	indices := m.selectedIndices()
	names := make([]string, 0, len(indices))
	for _, idx := range indices {
		names = append(names, m.results[idx].Name)
	}
	return names
}

func (m findSelectionModel) selectedCount() int {
	return len(m.selected)
}

func (m findSelectionModel) filtered() []int {
	q := strings.TrimSpace(strings.ToLower(m.filter))
	indices := make([]int, 0, len(m.results))
	for i, result := range m.results {
		if q == "" ||
			strings.Contains(strings.ToLower(result.Name), q) ||
			strings.Contains(strings.ToLower(result.Source), q) {
			indices = append(indices, i)
		}
	}
	return indices
}

func (m findSelectionModel) renderActive() string {
	filtered := m.filtered()
	if len(filtered) == 0 {
		m.cursor = 0
	}

	lines := []string{
		selectorActiveStyle.Render("◆") + "  " + selectorTitleStyle.Render("Find skills") + " " + selectorHintStyle.Render("(space to toggle)"),
		selectorBar(),
		fmt.Sprintf("%s  %s %s %s", selectorBar(), selectorHintStyle.Render("Query:"), selectorPathStyle.Render(m.query), selectorHintStyle.Render(fmt.Sprintf("(%d results)", len(m.results)))),
		fmt.Sprintf("%s  %s %s%s", selectorBar(), selectorSearchStyle.Render("Filter:"), m.filter, selectorQueryCursor.Render(" ")),
		fmt.Sprintf("%s  %s", selectorBar(), selectorHintStyle.Render("↑↓ move, space select, enter install, esc/ctrl+c cancel")),
		selectorBar(),
	}

	visibleStart := max(0, min(m.cursor-skillSelectorMaxVisible/2, len(filtered)-skillSelectorMaxVisible))
	visibleEnd := min(len(filtered), visibleStart+skillSelectorMaxVisible)
	visible := filtered[visibleStart:visibleEnd]

	if len(filtered) == 0 {
		lines = append(lines, fmt.Sprintf("%s  %s", selectorBar(), selectorHintStyle.Render("No matches found")))
	} else {
		lastSource := ""
		for _, resultIndex := range visible {
			result := m.results[resultIndex]
			if result.Source != lastSource {
				if lastSource != "" {
					lines = append(lines, selectorBar())
				}
				lines = append(lines, selectorGroupLine(result.Source, m.width))
				lastSource = result.Source
			}
			lines = append(lines, m.renderResultLine(resultIndex, result, filtered))
		}
		hiddenBefore := visibleStart
		hiddenAfter := len(filtered) - visibleEnd
		if hiddenBefore > 0 || hiddenAfter > 0 {
			parts := []string{}
			if hiddenBefore > 0 {
				parts = append(parts, fmt.Sprintf("↑ %d more", hiddenBefore))
			}
			if hiddenAfter > 0 {
				parts = append(parts, fmt.Sprintf("↓ %d more", hiddenAfter))
			}
			lines = append(lines, fmt.Sprintf("%s  %s", selectorBar(), selectorHintStyle.Render(strings.Join(parts, "  "))))
		}
	}

	if selected := m.selectedResult(); selected != nil {
		lines = append(lines,
			selectorBar(),
			fmt.Sprintf("%s  %s %s", selectorBar(), selectorSummaryStyle.Render("Command:"), selectorHintStyle.Render(findInstallCommand(*selected))),
		)
	}
	lines = append(lines, m.renderSelectedSummaryLines()...)
	lines = append(lines, selectorBarStyle.Render("└"))
	return strings.Join(lines, "\n") + "\n"
}

func (m findSelectionModel) renderResultLine(resultIndex int, result foundSkill, filtered []int) string {
	active := len(filtered) > 0 && filtered[m.cursor] == resultIndex
	prefix := " "
	if active {
		prefix = selectorCursorStyle.Render("❯")
	}

	label := result.Name
	if active {
		label = lipgloss.NewStyle().Underline(true).Render(label)
	}
	radio := selectorInactive.Render("○")
	if m.selected[resultIndex] {
		radio = selectorSelected.Render("●")
	}
	installText := selectorHintStyle.Render(fmt.Sprintf("(%d installs)", result.Installs))
	return fmt.Sprintf("%s %s %s %s %s", selectorBar(), prefix, radio, label, installText)
}

func (m findSelectionModel) renderSelectedSummaryLines() []string {
	names := m.selectedNames()
	if len(names) == 0 {
		return []string{fmt.Sprintf("%s  %s", selectorBar(), selectorHintStyle.Render("Selected: (none)"))}
	}

	width := max(20, m.width-3)
	parts := wrapSelectedSummary(names, width)
	lines := make([]string, 0, len(parts))
	for _, part := range parts {
		lines = append(lines, fmt.Sprintf("%s  %s", selectorBar(), part))
	}
	return lines
}

func (m findSelectionModel) renderSubmitted() string {
	selected := m.selectedResults()
	if len(selected) == 0 {
		return ""
	}
	names := m.selectedNames()
	lines := []string{
		selectorSuccessStyle.Render("◇") + "  " + selectorTitleStyle.Render("Find skills"),
		fmt.Sprintf("%s  %s", selectorBar(), selectorDimStyle.Render(strings.Join(names, ", "))),
		"",
		selectorTitleStyle.Render("Installation Summary"),
	}
	for _, skill := range selected {
		lines = append(lines,
			"",
			selectorPathStyle.Render(skill.Name),
			fmt.Sprintf("  %s %s", selectorHintStyle.Render("source:"), skill.Source),
			fmt.Sprintf("  %s %s", selectorHintStyle.Render("command:"), findInstallCommand(skill)),
		)
	}
	lines = append(lines,
		"",
		selectorSuccessStyle.Render(fmt.Sprintf("Ready to install %d skill%s.", len(selected), skillPlural(len(selected)))),
	)
	return strings.Join(lines, "\n") + "\n"
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
