package commands

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/tdeshazo/goskill/internal/agents"
	"github.com/tdeshazo/goskill/internal/installer"
	"github.com/tdeshazo/goskill/internal/skills"
	"github.com/tdeshazo/goskill/internal/source"
)

const skillSelectorMaxVisible = 8

var errInteractiveUnavailable = errors.New("interactive selection unavailable")

var (
	selectorActiveStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	selectorCancelStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	selectorCursorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	selectorSelected     = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	selectorInactive     = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	selectorBarStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	selectorTitleStyle   = lipgloss.NewStyle().Bold(true)
	selectorGroupStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("63")).Bold(true)
	selectorHintStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	selectorSearchStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	selectorQueryCursor  = lipgloss.NewStyle().Reverse(true)
	selectorPathStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	selectorSuccessStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	selectorWarningStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	selectorSummaryStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	selectorDimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
)

type skillSelectionModel struct {
	source    string
	skills    []skills.Skill
	selected  map[int]bool
	query     string
	cursor    int
	width     int
	cancelled bool
	done      bool
	install   selectorInstallContext
}

type selectorInstallContext struct {
	targets []agents.Type
	global  bool
	cwd     string
	mode    installer.Mode
}

func (a App) canUseInteractiveSelector() bool {
	stdin, ok := a.Stdin.(*os.File)
	if !ok || stdin != os.Stdin || !isTerminalFile(stdin) {
		return false
	}
	stdout, ok := a.Stdout.(*os.File)
	return ok && stdout == os.Stdout && isTerminalFile(stdout)
}

func (a App) selectSkillsInteractive(discovered []skills.Skill, source string, opts AddOptions, targets []agents.Type, mode installer.Mode) ([]skills.Skill, error) {
	model := newSkillSelectionModel(discovered, source, selectorInstallContext{
		targets: targets,
		global:  opts.Global,
		cwd:     a.Cwd,
		mode:    mode,
	})
	result, err := tea.NewProgram(model).Run()
	if err != nil {
		return nil, err
	}
	finalModel, ok := result.(skillSelectionModel)
	if !ok {
		if pointerModel, ok := result.(*skillSelectionModel); ok {
			finalModel = *pointerModel
		} else {
			return nil, errInteractiveUnavailable
		}
	}
	if finalModel.cancelled {
		return nil, nil
	}
	selected := finalModel.selectedSkills()
	if len(selected) == 0 {
		return nil, fmt.Errorf("no skills selected")
	}
	return selected, nil
}

func newSkillSelectionModel(discovered []skills.Skill, source string, contexts ...selectorInstallContext) skillSelectionModel {
	context := selectorInstallContext{cwd: ".", mode: installer.Symlink}
	if len(contexts) > 0 {
		context = contexts[0]
		if context.cwd == "" {
			context.cwd = "."
		}
		if context.mode == "" {
			context.mode = installer.Symlink
		}
	}

	sorted := sortedSkillsByGroup(discovered)

	return skillSelectionModel{
		source:   source,
		skills:   sorted,
		selected: map[int]bool{},
		width:    88,
		install:  context,
	}
}

func sortedSkillsByGroup(discovered []skills.Skill) []skills.Skill {
	sorted := append([]skills.Skill(nil), discovered...)
	sort.Slice(sorted, func(i, j int) bool {
		ai := skillGroup(sorted[i])
		aj := skillGroup(sorted[j])
		if ai == aj {
			return sorted[i].Name < sorted[j].Name
		}
		return ai < aj
	})
	return sorted
}

func (m skillSelectionModel) Init() tea.Cmd {
	return nil
}

func (m skillSelectionModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
			if len(m.query) > 0 {
				m.query = m.query[:len(m.query)-1]
				m.cursor = 0
			}
		default:
			text := msg.Key().Text
			if text != "" {
				m.query += text
				m.cursor = 0
			}
		}
	}
	return m, nil
}

func (m skillSelectionModel) View() tea.View {
	if m.cancelled {
		return tea.NewView(selectorCancelStyle.Render("■") + "  " + selectorTitleStyle.Render("Select skills to install") + "\n" +
			fmt.Sprintf("%s  %s\n", selectorBar(), selectorHintStyle.Render("Cancelled")))
	}
	if m.done {
		return tea.NewView(m.renderSubmitted())
	}
	return tea.NewView(m.renderActive())
}

func (m *skillSelectionModel) moveCursor(delta int) {
	filtered := m.filtered()
	if len(filtered) == 0 {
		m.cursor = 0
		return
	}
	m.cursor = clampInt(m.cursor+delta, 0, len(filtered)-1)
}

func (m *skillSelectionModel) toggleCurrent() {
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

func (m skillSelectionModel) filtered() []int {
	q := strings.TrimSpace(strings.ToLower(m.query))
	indices := make([]int, 0, len(m.skills))
	for i, skill := range m.skills {
		group := skillGroup(skill)
		if q == "" ||
			strings.Contains(strings.ToLower(skill.Name), q) ||
			strings.Contains(strings.ToLower(skill.Description), q) ||
			strings.Contains(strings.ToLower(group), q) {
			indices = append(indices, i)
		}
	}
	return indices
}

func (m skillSelectionModel) renderActive() string {
	filtered := m.filtered()
	if len(filtered) == 0 {
		m.cursor = 0
	}

	lines := []string{
		selectorActiveStyle.Render("◆") + "  " + selectorTitleStyle.Render("Select skills to install") + " " + selectorHintStyle.Render("(space to toggle)"),
		selectorBar(),
		fmt.Sprintf("%s  %s %s %s", selectorBar(), selectorHintStyle.Render("Source:"), selectorPathStyle.Render(m.source), selectorHintStyle.Render(fmt.Sprintf("(%d skills discovered)", len(m.skills)))),
		fmt.Sprintf("%s  %s %s%s", selectorBar(), selectorSearchStyle.Render("Search:"), m.query, selectorQueryCursor.Render(" ")),
		fmt.Sprintf("%s  %s", selectorBar(), selectorHintStyle.Render("↑↓ move, space select, enter confirm, esc/ctrl+c cancel")),
		selectorBar(),
	}

	visibleStart := max(0, min(m.cursor-skillSelectorMaxVisible/2, len(filtered)-skillSelectorMaxVisible))
	visibleEnd := min(len(filtered), visibleStart+skillSelectorMaxVisible)
	visible := filtered[visibleStart:visibleEnd]

	if len(filtered) == 0 {
		lines = append(lines, fmt.Sprintf("%s  %s", selectorBar(), selectorHintStyle.Render("No matches found")))
	} else {
		lastGroup := ""
		for _, skillIndex := range visible {
			skill := m.skills[skillIndex]
			group := skillGroup(skill)
			if group != lastGroup {
				if lastGroup != "" {
					lines = append(lines, selectorBar())
				}
				lines = append(lines, selectorGroupLine(titleCase(group), m.width))
				lastGroup = group
			}
			lines = append(lines, m.renderSkillLine(skillIndex, skill, filtered))
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

	lines = append(lines,
		selectorBar(),
	)
	lines = append(lines, m.renderSelectedSummaryLines()...)
	lines = append(lines, selectorBarStyle.Render("└"))
	return strings.Join(lines, "\n") + "\n"
}

func (m skillSelectionModel) renderSkillLine(skillIndex int, skill skills.Skill, filtered []int) string {
	active := len(filtered) > 0 && filtered[m.cursor] == skillIndex
	prefix := " "
	if active {
		prefix = selectorCursorStyle.Render("❯")
	}

	radio := selectorInactive.Render("○")
	if m.selected[skillIndex] {
		radio = selectorSelected.Render("●")
	}

	label := skill.Name
	if active {
		label = lipgloss.NewStyle().Underline(true).Render(label)
	}

	hint := ""
	if active && skill.Description != "" {
		width := max(20, m.width-lipgloss.Width(skill.Name)-14)
		hint = selectorHintStyle.Render(" (" + truncateDisplay(skill.Description, width) + ")")
	}
	return fmt.Sprintf("%s %s %s %s%s", selectorBar(), prefix, radio, label, hint)
}

func (m skillSelectionModel) renderSelectedSummaryLines() []string {
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

func (m skillSelectionModel) renderSubmitted() string {
	names := m.selectedNames()
	lines := []string{
		selectorSuccessStyle.Render("◇") + "  " + selectorTitleStyle.Render("Select skills to install"),
		fmt.Sprintf("%s  %s", selectorBar(), selectorDimStyle.Render(strings.Join(names, ", "))),
		"",
		selectorTitleStyle.Render("Installation Summary"),
	}

	for _, idx := range m.selectedIndices() {
		skill := m.skills[idx]
		lines = append(lines,
			"",
			selectorPathStyle.Render(skill.Name),
			fmt.Sprintf("  %s %s", selectorHintStyle.Render("scope:"), scopeLabel(m.install.global)),
			fmt.Sprintf("  %s %s", selectorHintStyle.Render("mode:"), m.install.mode),
		)
		if m.install.mode != installer.Copy {
			lines = append(lines, fmt.Sprintf("  %s %s", selectorHintStyle.Render("canonical:"), shortSelectionPath(installer.CanonicalPath(skill.Name, m.install.global, m.install.cwd))))
		}
		for _, target := range m.install.targets {
			lines = append(lines, fmt.Sprintf("  %s %s", selectorSuccessStyle.Render(agents.Display(target)+":"), shortSelectionPath(installer.InstallPath(skill.Name, target, m.install.global, m.install.cwd))))
		}
		if skill.Path != "" {
			lines = append(lines, fmt.Sprintf("  %s %s", selectorHintStyle.Render("source:"), shortSelectionPath(skill.Path)))
		}
	}

	lines = append(lines,
		"",
		selectorSuccessStyle.Render(fmt.Sprintf("Ready to install %d skill%s.", len(names), skillPlural(len(names)))),
	)
	return strings.Join(lines, "\n") + "\n"
}

func wrapSelectedSummary(names []string, width int) []string {
	label := selectorSummaryStyle.Render("Selected:")
	indent := strings.Repeat(" ", lipgloss.Width("Selected:")+1)
	lines := []string{}
	line := label

	for i, name := range names {
		token := name
		if i < len(names)-1 {
			token += ","
		}

		separator := " "
		if lipgloss.Width(line)+lipgloss.Width(separator)+lipgloss.Width(token) > width {
			lines = append(lines, line)
			line = indent + token
			continue
		}
		line += separator + token
	}

	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

func (m skillSelectionModel) selectedSkills() []skills.Skill {
	indices := m.selectedIndices()
	selected := make([]skills.Skill, 0, len(indices))
	for _, idx := range indices {
		selected = append(selected, m.skills[idx])
	}
	return selected
}

func (m skillSelectionModel) selectedIndices() []int {
	indices := make([]int, 0, len(m.selected))
	for idx := range m.selected {
		indices = append(indices, idx)
	}
	sort.Slice(indices, func(i, j int) bool {
		a := m.skills[indices[i]]
		b := m.skills[indices[j]]
		if skillGroup(a) == skillGroup(b) {
			return a.Name < b.Name
		}
		return skillGroup(a) < skillGroup(b)
	})
	return indices
}

func (m skillSelectionModel) selectedNames() []string {
	indices := m.selectedIndices()
	names := make([]string, 0, len(indices))
	for _, idx := range indices {
		names = append(names, m.skills[idx].Name)
	}
	return names
}

func (m skillSelectionModel) selectedCount() int {
	return len(m.selected)
}

func isTerminalFile(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func skillGroup(skill skills.Skill) string {
	for _, key := range []string{"plugin", "pluginName", "source"} {
		if value, ok := skill.Metadata[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	if group := skillGroupFromPath(skill.RepoPath, true); group != "" {
		return group
	}
	if group := skillGroupFromPath(skill.Path, false); group != "" {
		return group
	}
	return "skills"
}

func skillGroupFromPath(rawPath string, allowRelativeGrouping bool) string {
	if rawPath == "" {
		return ""
	}
	normalized := strings.Trim(filepath.ToSlash(rawPath), "/")
	if normalized == "" {
		return ""
	}
	parts := strings.Split(normalized, "/")
	if len(parts) == 0 {
		return ""
	}
	if strings.EqualFold(parts[len(parts)-1], "SKILL.md") {
		parts = parts[:len(parts)-1]
	}
	if len(parts) == 0 {
		return ""
	}

	if rootEnd, ok := skillRootEnd(parts); ok {
		return cleanSkillGroupParts(parts[rootEnd : len(parts)-1])
	}
	if allowRelativeGrouping && len(parts) > 1 {
		return cleanSkillGroupParts(parts[:len(parts)-1])
	}
	return ""
}

func skillRootEnd(parts []string) (int, bool) {
	roots := [][]string{
		{".agents", "skills"},
		{".claude", "skills"},
		{".codex", "skills"},
		{".cursor", "skills"},
		{"skills"},
	}
	bestEnd := -1
	for i := range parts {
		for _, root := range roots {
			if i+len(root) > len(parts) {
				continue
			}
			matches := true
			for j, want := range root {
				if parts[i+j] != want {
					matches = false
					break
				}
			}
			if matches && i+len(root) > bestEnd {
				bestEnd = i + len(root)
			}
		}
	}
	if bestEnd < 0 {
		return 0, false
	}
	return bestEnd, true
}

func cleanSkillGroupParts(parts []string) string {
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		switch part {
		case "", ".":
			continue
		case ".curated":
			cleaned = append(cleaned, "curated")
		case ".experimental":
			cleaned = append(cleaned, "experimental")
		case ".system":
			cleaned = append(cleaned, "system")
		default:
			cleaned = append(cleaned, part)
		}
	}
	if len(cleaned) == 0 {
		return "skills"
	}
	return strings.Join(cleaned, "/")
}

func selectorGroupLine(title string, width int) string {
	text := "── " + selectorGroupStyle.Render(title) + " "
	fill := strings.Repeat("─", max(4, min(30, width-lipgloss.Width(title)-10)))
	return fmt.Sprintf("%s  %s%s", selectorBar(), selectorBarStyle.Render(text), selectorBarStyle.Render(fill))
}

func titleCase(s string) string {
	segments := strings.Split(s, "/")
	for i, segment := range segments {
		segments[i] = titleWords(strings.TrimSpace(segment))
	}
	return strings.Join(segments, " / ")
}

func titleWords(s string) string {
	words := strings.FieldsFunc(s, func(r rune) bool {
		return r == '-' || r == '_' || r == ' '
	})
	for i, word := range words {
		if word == "" {
			continue
		}
		words[i] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, " ")
}

func shortSelectionPath(path string) string {
	wd, err := os.Getwd()
	absPath, absErr := filepath.Abs(path)
	if err == nil && absErr == nil {
		if rel, relErr := filepath.Rel(wd, absPath); relErr == nil && !strings.HasPrefix(rel, "..") {
			return "." + string(filepath.Separator) + rel
		}
	}
	return path
}

func skillSelectorSourceLabel(parsed source.Parsed, rawSource, cwd string) string {
	if parsed.Type == source.Local {
		return shorten(parsed.LocalPath, cwd)
	}
	if parsed.Type == source.GitHub {
		if ownerRepo := source.OwnerRepo(parsed); ownerRepo != "" {
			return sourceWebURL("https://github.com/"+ownerRepo, "tree", parsed.Ref, parsed.Subpath)
		}
	}
	if parsed.Type == source.GitLab {
		if ownerRepo := source.OwnerRepo(parsed); ownerRepo != "" {
			return sourceWebURL("https://gitlab.com/"+ownerRepo, "-/tree", parsed.Ref, parsed.Subpath)
		}
	}
	if parsed.URL != "" {
		return strings.TrimSuffix(strings.TrimSuffix(parsed.URL, ".git"), "/")
	}
	return rawSource
}

func sourceWebURL(base, treeSegment, ref, subpath string) string {
	base = strings.TrimSuffix(strings.TrimSuffix(base, ".git"), "/")
	if ref == "" {
		return base
	}
	out := base + "/" + treeSegment + "/" + ref
	if subpath != "" {
		out += "/" + strings.TrimPrefix(subpath, "/")
	}
	return out
}

func truncateDisplay(s string, width int) string {
	if lipgloss.Width(s) <= width {
		return s
	}
	if width <= 1 {
		return "…"
	}
	runes := []rune(s)
	for len(runes) > 0 && lipgloss.Width(string(runes))+1 > width {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}

func selectorBar() string {
	return selectorBarStyle.Render("│")
}

func scopeLabel(global bool) string {
	if global {
		return "global"
	}
	return "project"
}

func skillPlural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func commonPath(a, b string) string {
	if a == "" || b == "" {
		return ""
	}
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return ""
	}
	rel, err := filepath.Rel(absA, absB)
	for err == nil && strings.HasPrefix(rel, "..") {
		parent := filepath.Dir(absA)
		if parent == absA {
			return ""
		}
		absA = parent
		rel, err = filepath.Rel(absA, absB)
	}
	if err != nil {
		return ""
	}
	return absA
}

func clampInt(v, low, high int) int {
	return min(max(v, low), high)
}
