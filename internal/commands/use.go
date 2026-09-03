package commands

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/tdeshazo/goskill/internal/agents"
	"github.com/tdeshazo/goskill/internal/installer"
	"github.com/tdeshazo/goskill/internal/skills"
	"github.com/tdeshazo/goskill/internal/source"
)

type UseOptions struct {
	Skill     string
	Agent     []string
	FullDepth bool
	Help      bool
}

type materializedUseSkill struct {
	Root               string
	Dir                string
	Content            string
	HasSupportingFiles bool
}

type commandExitError struct {
	code int
}

func (e commandExitError) Error() string {
	return fmt.Sprintf("agent exited with status %d", e.code)
}

func ExitCode(err error) (int, bool) {
	var exitErr commandExitError
	if !errors.As(err, &exitErr) {
		return 0, false
	}
	return exitErr.code, true
}

func (a App) Use(rawSource string, opts UseOptions) error {
	if opts.Help {
		fmt.Fprint(a.Stdout, useHelp())
		return nil
	}
	if rawSource == "" {
		return errors.New("missing source\n\n" + useHelp())
	}
	if err := validateUseAgent(opts.Agent); err != nil {
		return err
	}

	parsed, sourceSelector, err := parseUseSource(rawSource)
	if err != nil {
		return err
	}
	selector, err := resolveUseSelector(first(sourceSelector, parsed.SkillFilter), opts.Skill)
	if err != nil {
		return err
	}

	resolveOptions := AddOptions{FullDepth: opts.FullDepth}
	if selector != "" {
		resolveOptions.Skill = []string{selector}
	}
	resolved, cleanup, err := a.resolveSkills(parsed, resolveOptions)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		return err
	}
	selected, err := selectUseSkill(resolved.skills, selector, rawSource)
	if err != nil {
		return err
	}
	materialized, err := materializeUseSkill(selected)
	if err != nil {
		return err
	}
	if len(opts.Agent) > 0 || !materialized.HasSupportingFiles {
		defer os.RemoveAll(materialized.Root)
	}
	prompt := buildUsePrompt(materialized)

	if len(opts.Agent) == 0 {
		_, err = fmt.Fprint(a.Stdout, prompt)
		return err
	}
	return a.launchUseAgent(agents.Type(opts.Agent[0]), prompt)
}

func parseUseSource(rawSource string) (source.Parsed, string, error) {
	sourceText, selector := splitUseSourceSelector(rawSource)
	parsed, err := source.Parse(sourceText)
	if err != nil {
		return source.Parsed{}, "", err
	}
	return parsed, selector, nil
}

func splitUseSourceSelector(rawSource string) (string, string) {
	input := strings.TrimSpace(rawSource)
	if strings.Contains(input, "#") {
		return input, ""
	}
	at := strings.LastIndex(input, "@")
	if at < 0 || at == len(input)-1 {
		return input, ""
	}
	lastSlash := strings.LastIndexAny(input, "/\\")
	if strings.Contains(input, "://") {
		authorityStart := strings.Index(input, "://") + len("://")
		pathStart := strings.Index(input[authorityStart:], "/")
		if pathStart < 0 {
			return input, ""
		}
		lastSlash = authorityStart + pathStart
		if slash := strings.LastIndex(input[lastSlash+1:], "/"); slash >= 0 {
			lastSlash += slash + 1
		}
	}
	if at <= lastSlash {
		return input, ""
	}
	return input[:at], input[at+1:]
}

func parseUse(args []string) (string, UseOptions, error) {
	opts := UseOptions{Agent: []string{}}
	sources := []string{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "-h", "--help":
			opts.Help = true
		case "--full-depth":
			opts.FullDepth = true
		case "-s", "--skill":
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return "", UseOptions{}, fmt.Errorf("%s requires a skill name", arg)
			}
			if opts.Skill != "" {
				return "", UseOptions{}, errors.New("only one --skill value can be provided")
			}
			i++
			opts.Skill = args[i]
		case "-a", "--agent":
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return "", UseOptions{}, fmt.Errorf("%s requires an agent name", arg)
			}
			i++
			opts.Agent = append(opts.Agent, args[i])
		default:
			if strings.HasPrefix(arg, "-") {
				return "", UseOptions{}, fmt.Errorf("unknown option: %s", arg)
			}
			sources = append(sources, arg)
		}
	}
	if opts.Help {
		return "", opts, nil
	}
	if len(sources) == 0 {
		return "", opts, nil
	}
	if len(sources) > 1 {
		return "", UseOptions{}, fmt.Errorf("expected one source, received %d: %s", len(sources), strings.Join(sources, ", "))
	}
	return sources[0], opts, nil
}

func validateUseAgent(names []string) error {
	if len(names) == 0 {
		return nil
	}
	if len(names) > 1 {
		return errors.New("goskill use --agent accepts exactly one agent")
	}
	name := names[0]
	if name == "*" {
		return errors.New("goskill use --agent does not support '*'; specify exactly one agent")
	}
	if !agents.IsValid(name) {
		return fmt.Errorf("invalid agent: %s (valid: claude-code, codex, cursor)", name)
	}
	if name == string(agents.Cursor) {
		return errors.New("running Cursor is not supported yet; supported agents for goskill use: claude-code, codex")
	}
	return nil
}

func resolveUseSelector(sourceSelector, optionSelector string) (string, error) {
	if sourceSelector != "" && optionSelector != "" && !strings.EqualFold(sourceSelector, optionSelector) {
		return "", fmt.Errorf(
			"conflicting skill selectors: source selects %q but --skill selects %q; provide one selector",
			sourceSelector,
			optionSelector,
		)
	}
	if optionSelector != "" {
		return optionSelector, nil
	}
	return sourceSelector, nil
}

func selectUseSkill(discovered []skills.Skill, selector, rawSource string) (skills.Skill, error) {
	if len(discovered) == 0 {
		return skills.Skill{}, errors.New("no valid skills found; skills require a SKILL.md with name and description")
	}
	if selector == "" {
		if len(discovered) == 1 {
			return discovered[0], nil
		}
		return skills.Skill{}, multipleUseSkillsError(rawSource, discovered)
	}
	selected := skills.Filter(discovered, []string{selector})
	if len(selected) == 0 {
		return skills.Skill{}, noUseSkillMatchError(selector, discovered)
	}
	if len(selected) > 1 {
		return skills.Skill{}, fmt.Errorf("skill selector %q matched multiple skills", selector)
	}
	return selected[0], nil
}

func multipleUseSkillsError(rawSource string, discovered []skills.Skill) error {
	lines := []string{"this source contains multiple skills; specify exactly one skill:"}
	for _, skill := range discovered {
		lines = append(lines, "  - "+skill.Name)
	}
	examples := fmt.Sprintf(
		"examples:\n  goskill use %s@%s\n  goskill use %s --skill %s",
		rawSource,
		discovered[0].Name,
		rawSource,
		discovered[0].Name,
	)
	lines = append(
		lines,
		"",
		examples,
	)
	return errors.New(strings.Join(lines, "\n"))
}

func noUseSkillMatchError(selector string, discovered []skills.Skill) error {
	lines := []string{fmt.Sprintf("no matching skill found for: %s", selector), "available skills:"}
	for _, skill := range discovered {
		lines = append(lines, "  - "+skill.Name)
	}
	return errors.New(strings.Join(lines, "\n"))
}

func materializeUseSkill(skill skills.Skill) (materializedUseSkill, error) {
	root, err := os.MkdirTemp("", "skill-use-")
	if err != nil {
		return materializedUseSkill{}, err
	}
	dir := filepath.Join(root, skills.SanitizeName(skill.Name))
	if !skills.PathSafe(root, dir) {
		_ = os.RemoveAll(root)
		return materializedUseSkill{}, errors.New("invalid skill name: potential path traversal detected")
	}
	if err := installer.MaterializeSkill(skill, dir); err != nil {
		_ = os.RemoveAll(root)
		return materializedUseSkill{}, err
	}
	content, err := materializedSkillContent(skill, dir)
	if err != nil {
		_ = os.RemoveAll(root)
		return materializedUseSkill{}, err
	}
	hasSupportingFiles, err := containsSupportingFiles(dir)
	if err != nil {
		_ = os.RemoveAll(root)
		return materializedUseSkill{}, err
	}
	return materializedUseSkill{
		Root:               root,
		Dir:                dir,
		Content:            content,
		HasSupportingFiles: hasSupportingFiles,
	}, nil
}

func materializedSkillContent(skill skills.Skill, dir string) (string, error) {
	if skill.RawContent != "" {
		return skill.RawContent, nil
	}
	for _, file := range skill.Files {
		if strings.EqualFold(filepath.Base(filepath.FromSlash(file.Path)), "SKILL.md") {
			return file.Contents, nil
		}
	}
	content, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return "", fmt.Errorf("reading materialized SKILL.md: %w", err)
	}
	return string(content), nil
}

func containsSupportingFiles(root string) (bool, error) {
	found := false
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root || entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if strings.EqualFold(filepath.ToSlash(rel), "SKILL.md") {
			return nil
		}
		found = true
		return fs.SkipAll
	})
	return found, err
}

func buildUsePrompt(skill materializedUseSkill) string {
	sections := []string{
		"You are being given a Skill to execute for the user's next request.",
		"Use the following SKILL.md as your instructions:",
		fmt.Sprintf("<SKILL.md>\n%s\n</SKILL.md>", skill.Content),
	}
	if skill.HasSupportingFiles {
		supportingFiles := "Supporting files for this skill were downloaded to:\n%s\n\n" +
			"When the SKILL.md references relative paths, read them from that directory."
		sections = append(
			sections,
			fmt.Sprintf(supportingFiles, skill.Dir),
		)
	}
	return strings.Join(sections, "\n\n") + "\n"
}

func (a App) launchUseAgent(agent agents.Type, prompt string) error {
	command := ""
	switch agent {
	case agents.ClaudeCode:
		command = "claude"
	case agents.Codex:
		command = "codex"
	default:
		return fmt.Errorf("running %s is not supported yet", agents.Display(agent))
	}
	cmd := exec.Command(command, prompt)
	cmd.Stdin = a.Stdin
	cmd.Stdout = a.Stdout
	cmd.Stderr = a.Stderr
	cmd.Dir = a.Cwd
	err := cmd.Run()
	if err == nil {
		return nil
	}
	if errors.Is(err, exec.ErrNotFound) {
		return fmt.Errorf("could not launch %s: command not found: %s", agents.Display(agent), command)
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code := exitErr.ExitCode()
		if code < 1 {
			code = 1
		}
		return commandExitError{code: code}
	}
	return fmt.Errorf("launching %s: %w", agents.Display(agent), err)
}

func useHelp() string {
	return `Usage: goskill use <source>[@<skill>] [options]

Generate a prompt for using one skill without installing it.

Options:
  -s, --skill <skill>   Select the skill to use
  -a, --agent <agent>   Start one supported agent interactively (claude-code, codex)
  --full-depth          Search nested directories like goskill add --full-depth
  -h, --help            Show this help message

Examples:
  goskill use vercel-labs/agent-skills@web-design-guidelines | claude
  goskill use vercel-labs/agent-skills --skill web-design-guidelines --agent claude-code
  goskill use vercel-labs/agent-skills@web-design-guidelines --agent codex
`
}
