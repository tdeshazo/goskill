package commands

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tdeshazo/goskill/internal/search"
	"github.com/tdeshazo/goskill/internal/skills"
	"github.com/tdeshazo/goskill/internal/terminal"
)

// Run executes the CLI through Cobra. A new command tree is built for each run
// so tests and embedded callers do not share flag state.
func (a App) Run(args []string) error {
	root := a.rootCommand()
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if arg == "--usage-spec" {
			a.writeOut(generateUsageSpec(root))
			return nil
		}
	}
	if err := validateFindProviderValue(args); err != nil {
		return err
	}
	args = expandVariadicFlags(args)
	if args == nil {
		args = []string{}
	}
	root.SetArgs(args)
	return root.Execute()
}

func validateFindProviderValue(args []string) error {
	if len(args) == 0 {
		return nil
	}
	switch args[0] {
	case "find", "search", "f", "s":
	default:
		return nil
	}

	for i := 1; i < len(args); i++ {
		if args[i] == "--" {
			return nil
		}
		if args[i] != "--provider" {
			continue
		}
		if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
			return errors.New("--provider requires a value")
		}
		i++
	}
	return nil
}

// These entry points remain available for callers that invoke a single
// command directly. They use the same Cobra path as Run.
func (a App) Spec(args []string) error     { return a.Run(append([]string{"spec"}, args...)) }
func (a App) Rules(args []string) error    { return a.Run(append([]string{"rules"}, args...)) }
func (a App) Explain(args []string) error  { return a.Run(append([]string{"explain"}, args...)) }
func (a App) Agent(args []string) error    { return a.Run(append([]string{"agent"}, args...)) }
func (a App) List(args []string) error     { return a.Run(append([]string{"list"}, args...)) }
func (a App) Find(args []string) error     { return a.Run(append([]string{"find"}, args...)) }
func (a App) Validate(args []string) error { return a.Run(append([]string{"validate"}, args...)) }
func (a App) Sync(args []string) error     { return a.Run(append([]string{"sync"}, args...)) }
func (a App) Check(args []string, doUpdate bool) error {
	if doUpdate {
		return a.Run(append([]string{"update"}, args...))
	}
	return a.Run(append([]string{"check"}, args...))
}

func (a App) rootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "goskill",
		Short:         "Manage agent skills",
		Version:       a.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		Run: func(*cobra.Command, []string) {
			a.banner()
		},
	}
	root.SetOut(a.Stdout)
	root.SetErr(a.Stderr)
	versionOutput := renderVersionOutput(a.Version)
	if !writerIsTerminal(a.Stdout) {
		versionOutput = terminal.StripEscapes(versionOutput)
	}
	root.SetVersionTemplate(versionOutput)
	root.SetHelpFunc(func(cmd *cobra.Command, _ []string) {
		switch cmd.CommandPath() {
		case "goskill":
			a.help()
		case "goskill find":
			a.writeOut(renderFindHelp())
		case "goskill validate":
			a.writeOut(renderValidateHelp())
		case "goskill spec":
			a.writeOut(renderSpecHelp())
		case "goskill rules":
			a.writeOut(renderRulesHelp())
		case "goskill explain":
			a.writeOut(renderExplainHelp())
		case "goskill use":
			a.writeOut(useHelp())
		case "goskill agent":
			a.writeOut(agentHelp())
		default:
			_ = cmd.Usage()
		}
	})
	root.PersistentPreRun = func(cmd *cobra.Command, _ []string) {
		if cmd == root {
			return
		}
		top := cmd
		for top.Parent() != root {
			top = top.Parent()
		}
		if top.Name() == "completion" || strings.HasPrefix(top.Name(), "__complete") {
			return
		}
		if top.Name() == "validate" {
			versionInfo, _ := cmd.Flags().GetBool("version-info")
			if versionInfo {
				return
			}
		}
		if !skipUpdateCheckForCommand(top.Name(), nil) {
			a.warnIfNewerRelease(top.Name())
		}
	}
	root.AddCommand(
		a.addCommand(), a.useCommand(), a.agentCommand(), a.listCommand(),
		a.removeCommand(), a.findCommand(), a.validateCommand(),
		a.specCommand(), a.rulesCommand(), a.explainCommand(),
		a.initCommand(), a.installCommand(), a.syncCommand(),
		a.checkCommand(false), a.checkCommand(true),
	)
	return root
}

func (a App) addCommand() *cobra.Command {
	var opts AddOptions
	cmd := &cobra.Command{
		Use:     "add <source>...",
		Aliases: []string{"a"},
		Short:   "Install skills from a source",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return a.Add(args, opts)
		},
	}
	f := cmd.Flags()
	f.BoolVarP(&opts.Global, "global", "g", false, "Install globally")
	f.StringArrayVarP(&opts.Agent, "agent", "a", nil, "Target agent (repeatable)")
	markUsageFlagVariadic(f.Lookup("agent"))
	f.BoolVarP(&opts.Yes, "yes", "y", false, "Accept prompts")
	f.StringArrayVarP(&opts.Skill, "skill", "s", nil, "Select skill (repeatable)")
	markUsageFlagVariadic(f.Lookup("skill"))
	f.BoolVarP(&opts.List, "list", "l", false, "List discovered skills")
	f.BoolVar(&opts.All, "all", false, "Install all skills for all agents")
	f.BoolVar(&opts.FullDepth, "full-depth", false, "Search the full source tree")
	f.BoolVar(&opts.Copy, "copy", false, "Copy files instead of linking")
	return cmd
}

func (a App) useCommand() *cobra.Command {
	var opts UseOptions
	cmd := &cobra.Command{
		Use:   "use <source>",
		Short: "Use a skill without installing it",
		Long:  "Use a skill without installing it. A source may include an @skill selector.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				return a.Use("", opts)
			}
			return a.Use(args[0], opts)
		},
	}
	f := cmd.Flags()
	f.VarP(&onceString{value: &opts.Skill, repeatedError: "only one --skill value can be provided"}, "skill", "s", "Select one skill")
	f.StringArrayVarP(&opts.Agent, "agent", "a", nil, "Launch one configured agent")
	// `use` accepts a StringArray to share its option handling with the other
	// commands, but validates that exactly one agent is selected.
	markUsageFlagNonRepeatable(f.Lookup("agent"))
	f.BoolVar(&opts.FullDepth, "full-depth", false, "Search the full source tree")
	return cmd
}

func (a App) listCommand() *cobra.Command {
	var global, jsonOut bool
	var agents []string
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List installed skills",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var scope *bool
			if cmd.Flags().Changed("global") {
				scope = &global
			}
			return a.list(scope, agents, jsonOut)
		},
	}
	f := cmd.Flags()
	f.BoolVarP(&global, "global", "g", false, "List global skills")
	f.StringArrayVarP(&agents, "agent", "a", nil, "Filter by agent (repeatable)")
	markUsageFlagVariadic(f.Lookup("agent"))
	f.BoolVar(&jsonOut, "json", false, "Write JSON")
	return cmd
}

func (a App) removeCommand() *cobra.Command {
	var opts RemoveOptions
	var skills []string
	cmd := &cobra.Command{
		Use:     "remove [skills]...",
		Aliases: []string{"rm", "r"},
		Short:   "Remove installed skills",
		RunE: func(_ *cobra.Command, args []string) error {
			return a.Remove(append(args, skills...), opts)
		},
	}
	f := cmd.Flags()
	f.BoolVarP(&opts.Global, "global", "g", false, "Remove global skills")
	f.StringArrayVarP(&opts.Agent, "agent", "a", nil, "Target agent (repeatable)")
	markUsageFlagVariadic(f.Lookup("agent"))
	f.BoolVarP(&opts.Yes, "yes", "y", false, "Accept prompts")
	f.BoolVar(&opts.All, "all", false, "Remove all matching skills")
	f.StringArrayVarP(&skills, "skill", "s", nil, "Select skill (repeatable)")
	markUsageFlagVariadic(f.Lookup("skill"))
	return cmd
}

func (a App) findCommand() *cobra.Command {
	var opts FindOptions
	var sortMode string
	cmd := &cobra.Command{
		Use:     "find [options] [query]...",
		Aliases: []string{"search", "f", "s"},
		Short:   "Search skill registries",
		Long:    "Search skill registries. A query is required unless --providers is set.",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Sort = search.SortMode(strings.ToLower(strings.TrimSpace(sortMode)))
			if !opts.Sort.Valid() {
				return fmt.Errorf("invalid --sort value %q (want relevance, popular, or newest)", sortMode)
			}
			opts.Provider = strings.TrimSpace(opts.Provider)
			if cmd.Flags().Changed("provider") && opts.Provider == "" {
				return errors.New("--provider requires a value")
			}
			return a.find(args, opts)
		},
	}
	f := cmd.Flags()
	f.BoolVar(&opts.Deep, "deep", false, "Include GitHub long-tail discovery")
	f.BoolVar(&opts.Refresh, "refresh", false, "Refresh static catalogs")
	f.BoolVar(&opts.Verified, "verified", false, "Keep verified results")
	f.BoolVar(&opts.JSON, "json", false, "Write JSON")
	f.BoolVar(&opts.Providers, "providers", false, "List provider capabilities")
	f.StringVar(&opts.Provider, "provider", "", "Filter by provider")
	f.StringVar(&sortMode, "sort", "relevance", "Sort by relevance, popular, or newest")
	return cmd
}

func (a App) validateCommand() *cobra.Command {
	format, profile := "text", "spec"
	var jsonOut, sarifOut, versionInfo bool
	cmd := &cobra.Command{
		Use:   "validate [skills]...",
		Short: "Validate SKILL.md files",
		RunE: func(cmd *cobra.Command, args []string) error {
			if jsonOut && sarifOut || cmd.Flags().Changed("format") && (jsonOut || sarifOut) {
				return errors.New("validation output format options are mutually exclusive")
			}
			if jsonOut {
				format = string(validationFormatJSON)
			}
			if sarifOut {
				format = string(validationFormatSARIF)
			}
			opts := validationOptions{
				Format:      validationFormat(strings.ToLower(strings.TrimSpace(format))),
				Profile:     skills.Profile(strings.ToLower(strings.TrimSpace(profile))),
				Sources:     args,
				VersionInfo: versionInfo,
			}
			switch opts.Format {
			case validationFormatText, validationFormatJSON, validationFormatSARIF:
			default:
				return fmt.Errorf("invalid validation format %q (want text, json, or sarif)", format)
			}
			if !opts.Profile.Valid() {
				return fmt.Errorf("invalid validation profile %q (want spec, recommended, or portable)", profile)
			}
			if versionInfo {
				if cmd.Flags().Changed("format") || jsonOut || sarifOut {
					return fmt.Errorf("--version-info cannot be combined with output formats\n%s", validateUsage)
				}
				if len(args) > 0 {
					return fmt.Errorf("--version-info does not accept skill sources\n%s", validateUsage)
				}
			} else if len(args) == 0 {
				return errors.New(validateUsage)
			}
			return a.validate(opts)
		},
	}
	f := cmd.Flags()
	f.Var(&onceString{value: &format, repeatedError: "validation output format options are mutually exclusive"}, "format", "Output format: text, json, or sarif")
	f.Var(&onceString{value: &profile, repeatedError: "validation profile options are mutually exclusive"}, "profile", "Validation profile: spec, recommended, or portable")
	f.VarPF(&onceBool{value: &jsonOut, name: "json"}, "json", "", "Alias for --format json").NoOptDefVal = "true"
	f.VarPF(&onceBool{value: &sarifOut, name: "sarif"}, "sarif", "", "Alias for --format sarif").NoOptDefVal = "true"
	f.VarPF(&onceBool{value: &versionInfo, name: "version-info"}, "version-info", "", "Write offline validator metadata").NoOptDefVal = "true"
	return cmd
}

func (a App) specCommand() *cobra.Command {
	var revision bool
	cmd := &cobra.Command{
		Use:   "spec",
		Short: "Show the pinned Agent Skills specification",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 0 {
				return errors.New("usage: goskill spec [--revision]")
			}
			return nil
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			if revision {
				a.writeOut(skills.SpecRevision + "\n")
			} else {
				a.writeOut(renderSpecOutput())
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&revision, "revision", false, "Write only the pinned Git revision")
	return cmd
}

func (a App) rulesCommand() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "rules",
		Short: "List validation rules",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return a.rules(jsonOut)
		},
	}
	cmd.Flags().VarPF(&onceBool{value: &jsonOut, name: "json"}, "json", "", "Write JSON").NoOptDefVal = "true"
	return cmd
}

func (a App) explainCommand() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "explain <code>",
		Short: "Explain one validation rule",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return a.explain(args[0], jsonOut)
		},
	}
	cmd.Flags().VarPF(&onceBool{value: &jsonOut, name: "json"}, "json", "", "Write JSON").NoOptDefVal = "true"
	return cmd
}

func (a App) agentCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Inspect configured agent definitions",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			a.writeOut(agentHelp())
			return nil
		},
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List configured agents",
			Args:  cobra.NoArgs,
			RunE:  func(_ *cobra.Command, _ []string) error { return a.agentList() },
		},
		&cobra.Command{
			Use:   "show <id>",
			Short: "Show one agent",
			Args:  cobra.ExactArgs(1),
			RunE:  func(_ *cobra.Command, args []string) error { return a.agentShow(args[0]) },
		},
		&cobra.Command{
			Use:   "validate <file>",
			Short: "Validate an agent definition",
			Args:  cobra.ExactArgs(1),
			RunE:  func(_ *cobra.Command, args []string) error { return a.agentValidate(args[0]) },
		},
	)
	return cmd
}

func (a App) initCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "init [name]",
		Short: "Create a SKILL.md template",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return a.Init(args)
		},
	}
}

func (a App) installCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "install",
		Aliases: []string{"i", "experimental_install"},
		Short:   "Install skills from the project lockfile",
		Args:    cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return a.InstallFromLock(nil)
		},
	}
}

func (a App) syncCommand() *cobra.Command {
	var opts syncOptions
	cmd := &cobra.Command{
		Use:     "sync",
		Aliases: []string{"experimental_sync"},
		Short:   "Sync skills from node_modules",
		Args:    cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return a.sync(opts)
		},
	}
	f := cmd.Flags()
	f.BoolVarP(&opts.Yes, "yes", "y", false, "Accept prompts")
	f.BoolVarP(&opts.Force, "force", "f", false, "Force synchronization")
	f.StringArrayVarP(&opts.Agent, "agent", "a", nil, "Target agent (repeatable)")
	markUsageFlagVariadic(f.Lookup("agent"))
	return cmd
}

func (a App) checkCommand(update bool) *cobra.Command {
	var opts updateOptions
	name := "check"
	short := "Check locked skills for updates"
	if update {
		name = "update"
		short = "Update locked skills"
	}
	cmd := &cobra.Command{
		Use:   name + " [skills]...",
		Short: short,
		RunE: func(_ *cobra.Command, args []string) error {
			opts.Skills = args
			return a.check(opts, update)
		},
	}
	if update {
		cmd.Aliases = []string{"upgrade"}
	}
	f := cmd.Flags()
	f.BoolVarP(&opts.Global, "global", "g", false, "Include global skills")
	f.BoolVarP(&opts.Project, "project", "p", false, "Include project skills")
	f.BoolVarP(&opts.Yes, "yes", "y", false, "Accept prompts")
	return cmd
}

// The old CLI accepts multiple values after one --agent or --skill flag. Cobra
// accepts one value per occurrence, so expand that syntax before flag parsing.
func expandVariadicFlags(args []string) []string {
	if len(args) < 2 {
		return args
	}
	command := args[0]
	agent := false
	skill := false
	switch command {
	case "add", "a", "remove", "rm", "r":
		agent, skill = true, true
	case "list", "ls", "sync", "experimental_sync":
		agent = true
	default:
		return args
	}
	out := []string{command}
	for i := 1; i < len(args); i++ {
		flag := args[i]
		if flag == "--" {
			return append(out, args[i:]...)
		}
		if !(agent && (flag == "--agent" || flag == "-a") || skill && (flag == "--skill" || flag == "-s")) {
			out = append(out, flag)
			continue
		}
		if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
			out = append(out, flag)
			continue
		}
		for i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			i++
			out = append(out, flag, args[i])
		}
	}
	return out
}

type onceBool struct {
	value *bool
	name  string
	set   bool
}

func (f *onceBool) Set(raw string) error {
	if f.set {
		return fmt.Errorf("--%s may only be specified once", f.name)
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return err
	}
	*f.value = value
	f.set = true
	return nil
}

func (f *onceBool) String() string {
	return strconv.FormatBool(*f.value)
}

func (*onceBool) Type() string { return "bool" }

func (*onceBool) IsBoolFlag() bool { return true }

type onceString struct {
	value         *string
	repeatedError string
	set           bool
}

func (f *onceString) Set(value string) error {
	if f.set {
		return errors.New(f.repeatedError)
	}
	*f.value = value
	f.set = true
	return nil
}

func (f *onceString) String() string { return *f.value }

func (*onceString) Type() string { return "string" }
