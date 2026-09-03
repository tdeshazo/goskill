package commands

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tdeshazo/goskill/internal/agents"
	"github.com/tdeshazo/goskill/internal/catalog"
	"github.com/tdeshazo/goskill/internal/github"
	"github.com/tdeshazo/goskill/internal/installer"
	"github.com/tdeshazo/goskill/internal/lockfile"
	"github.com/tdeshazo/goskill/internal/remoteskill"
	"github.com/tdeshazo/goskill/internal/search"
	"github.com/tdeshazo/goskill/internal/skills"
	"github.com/tdeshazo/goskill/internal/source"
	"github.com/tdeshazo/goskill/internal/wellknown"
)

const (
	findEndpointTimeout = 10 * time.Second
	// Find tries the rich and compatibility endpoints sequentially.
	findTimeout = 2 * findEndpointTimeout
)

type App struct {
	Version string
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	Cwd     string
}

func New(version string) App {
	cwd, _ := os.Getwd()
	return App{Version: version, Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, Cwd: cwd}
}

type AddOptions struct {
	Global    bool
	Agent     []string
	Yes       bool
	Skill     []string
	List      bool
	All       bool
	FullDepth bool
	Copy      bool
}

type RemoveOptions struct {
	Global bool
	Agent  []string
	Yes    bool
	All    bool
}

type FindOptions struct {
	Deep      bool
	Refresh   bool
	JSON      bool
	Verified  bool
	Providers bool
	Provider  string
	Sort      search.SortMode
}

func (a App) Run(args []string) error {
	if len(args) == 0 {
		a.banner()
		return nil
	}
	cmd, rest := args[0], args[1:]
	a.warnIfNewerRelease(cmd)
	switch cmd {
	case "--help", "-h", "help":
		a.help()
	case "--version", "-v":
		a.writeOut(renderVersionOutput(a.Version))
	case "add", "a":
		src, opts, err := parseAdd(rest)
		if err != nil {
			return err
		}
		return a.Add(src, opts)
	case "use":
		src, opts, err := parseUse(rest)
		if err != nil {
			return err
		}
		return a.Use(src, opts)
	case "list", "ls":
		return a.List(rest)
	case "remove", "rm", "r":
		names, opts, err := parseRemove(rest)
		if err != nil {
			return err
		}
		return a.Remove(names, opts)
	case "find", "search", "f", "s":
		return a.Find(rest)
	case "validate":
		return a.Validate(rest)
	case "init":
		return a.Init(rest)
	case "install", "i", "experimental_install":
		return a.InstallFromLock(rest)
	case "experimental_sync":
		return a.Sync(rest)
	case "check":
		return a.Check(rest, false)
	case "update", "upgrade":
		return a.Check(rest, true)
	default:
		return fmt.Errorf("unknown command: %s", cmd)
	}
	return nil
}

func (a App) Add(srcArgs []string, opts AddOptions) error {
	if len(srcArgs) == 0 {
		return errors.New("missing source")
	}
	if opts.All {
		opts.Skill = []string{"*"}
		opts.Agent = []string{"*"}
		opts.Yes = true
	}
	mode := installer.Symlink
	if opts.Copy {
		mode = installer.Copy
	}
	targets, err := a.resolveAgents(opts.Agent)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		targets = agents.DefaultTargets(a.Cwd)
	}
	var installed []string
	for _, rawSource := range srcArgs {
		parsed, err := source.Parse(rawSource)
		if err != nil {
			return err
		}
		resolved, cleanup, err := a.resolveSkillsForAdd(parsed, opts)
		if cleanup != nil {
			defer cleanup()
		}
		if err != nil {
			return err
		}
		if opts.List {
			a.writeOut(renderSkillDiscoveryList(resolved.skills, "Discovered skills"))
			continue
		}
		selected, err := a.selectSkills(resolved.skills, skillSelectorSourceLabel(parsed, rawSource, a.Cwd), opts, targets, mode)
		if err != nil {
			return err
		}
		sourceInstalled, err := a.installSelectedSkills(selected, addInstallContext{
			rawSource: rawSource,
			parsed:    parsed,
			resolved:  resolved,
			targets:   targets,
			global:    opts.Global,
			mode:      mode,
		})
		if err != nil {
			return err
		}
		installed = append(installed, sourceInstalled...)
	}
	if len(installed) > 0 {
		a.writeOut(renderSuccess("Installed skills", fmt.Sprintf("%d skill%s installed", len(installed), skillPlural(len(installed))), selectorSummaryStyle.Render(strings.Join(installed, ", "))))
	}
	return nil
}

type addInstallContext struct {
	rawSource string
	parsed    source.Parsed
	resolved  resolvedSkills
	targets   []agents.Type
	global    bool
	mode      installer.Mode
}

func (a App) installSelectedSkills(selected []skills.Skill, ctx addInstallContext) ([]string, error) {
	installed := make([]string, 0, len(selected))
	for _, skill := range selected {
		if err := a.installSkillForTargets(skill, ctx); err != nil {
			return nil, err
		}
		installed = append(installed, skill.Name)
		a.recordInstalledSkill(skill, ctx)
	}
	return installed, nil
}

func (a App) installSkillForTargets(skill skills.Skill, ctx addInstallContext) error {
	for _, agent := range ctx.targets {
		result := installer.InstallSkill(skill, agent, ctx.global, a.Cwd, ctx.mode)
		if !result.Success {
			return fmt.Errorf("failed to install %s for %s: %w", skill.Name, agent, result.Err)
		}
	}
	return nil
}

func (a App) recordInstalledSkill(skill skills.Skill, ctx addInstallContext) {
	if ctx.global {
		_ = lockfile.AddGlobal(skill.Name, lockfile.GlobalEntry{
			Source:          ctx.resolved.sourceID,
			SourceType:      string(ctx.parsed.Type),
			SourceURL:       ctx.rawSource,
			Ref:             ctx.parsed.Ref,
			SkillPath:       ctx.resolved.skillPath(skill),
			SkillFolderHash: ctx.resolved.folderHash(skill),
		})
		return
	}

	_ = lockfile.AddLocal(a.Cwd, skill.Name, lockfile.LocalEntry{
		Source:       ctx.resolved.sourceID,
		Ref:          ctx.parsed.Ref,
		SourceType:   string(ctx.parsed.Type),
		SkillPath:    ctx.resolved.skillPath(skill),
		ComputedHash: a.installedSkillHash(skill, ctx.resolved),
	})
}

func (a App) installedSkillHash(skill skills.Skill, resolved resolvedSkills) string {
	if hash := resolved.folderHash(skill); hash != "" {
		return hash
	}
	installDir := installer.CanonicalPath(skill.Name, false, a.Cwd)
	hash, _ := skills.FolderHash(installDir)
	return hash
}

func (a App) selectSkills(discovered []skills.Skill, source string, opts AddOptions, targets []agents.Type, mode installer.Mode) ([]skills.Skill, error) {
	if len(discovered) == 0 {
		return nil, fmt.Errorf("no skills found")
	}
	if len(opts.Skill) > 0 {
		selected := skills.Filter(discovered, opts.Skill)
		if len(selected) == 0 {
			return nil, fmt.Errorf("no matching skills found for: %s", strings.Join(opts.Skill, ", "))
		}
		return selected, nil
	}
	if len(discovered) == 1 || opts.Yes {
		return discovered, nil
	}
	if a.Stdin == nil {
		return nil, fmt.Errorf("multiple skills found; specify one or more with --skill")
	}
	if a.canUseInteractiveSelector() {
		selected, err := a.selectSkillsInteractive(discovered, source, opts, targets, mode)
		if err == nil {
			return selected, nil
		}
		if !errors.Is(err, errInteractiveUnavailable) {
			return nil, err
		}
	}

	a.writeOut(renderSkillSelectionPrompt(discovered))

	scanner := bufio.NewScanner(a.Stdin)
	if !scanner.Scan() {
		return nil, fmt.Errorf("multiple skills found; specify one or more with --skill")
	}
	selection := strings.TrimSpace(scanner.Text())
	if selection == "" {
		return nil, fmt.Errorf("no skills selected")
	}
	selected, err := parseSkillSelection(discovered, selection)
	if err != nil {
		return nil, err
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("no skills selected")
	}
	return selected, nil
}

func parseSkillSelection(discovered []skills.Skill, input string) ([]skills.Skill, error) {
	input = strings.TrimSpace(input)
	if input == "*" || strings.EqualFold(input, "all") {
		return discovered, nil
	}
	byName := map[string]skills.Skill{}
	for _, skill := range discovered {
		byName[strings.ToLower(skill.Name)] = skill
		byName[strings.ToLower(skills.SanitizeName(skill.Name))] = skill
	}
	var selected []skills.Skill
	seen := map[string]bool{}
	for _, token := range strings.FieldsFunc(input, func(r rune) bool { return r == ',' || r == '\n' || r == '\t' }) {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		if n, ok := parsePositiveInt(token); ok {
			if n < 1 || n > len(discovered) {
				return nil, fmt.Errorf("skill selection %d is out of range", n)
			}
			skill := discovered[n-1]
			if !seen[skill.Name] {
				selected = append(selected, skill)
				seen[skill.Name] = true
			}
			continue
		}
		skill, ok := byName[strings.ToLower(token)]
		if !ok {
			return nil, fmt.Errorf("unknown skill selection: %s", token)
		}
		if !seen[skill.Name] {
			selected = append(selected, skill)
			seen[skill.Name] = true
		}
	}
	return selected, nil
}

func parsePositiveInt(input string) (int, bool) {
	if input == "" {
		return 0, false
	}
	var n int
	for _, r := range input {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
	}
	return n, n > 0
}

func (a App) List(args []string) error {
	var global *bool
	var agentNames []string
	jsonOut := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-g", "--global":
			v := true
			global = &v
		case "--json":
			jsonOut = true
		case "-a", "--agent":
			for i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				agentNames = append(agentNames, args[i])
			}
		}
	}
	filter, invalid := agents.Validate(agentNames)
	if len(invalid) > 0 {
		return fmt.Errorf("invalid agents: %s", strings.Join(invalid, ", "))
	}
	list, err := installer.List(global, filter, a.Cwd)
	if err != nil {
		return err
	}
	sort.Slice(list, func(i, j int) bool {
		if orderI, orderJ := listScopeOrder(list[i].Scope), listScopeOrder(list[j].Scope); orderI != orderJ {
			return orderI < orderJ
		}
		return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name)
	})
	if jsonOut {
		type outSkill struct {
			Name   string   `json:"name"`
			Path   string   `json:"path"`
			Scope  string   `json:"scope"`
			Agents []string `json:"agents"`
		}
		var out []outSkill
		for _, item := range list {
			var names []string
			for _, agent := range item.Agents {
				names = append(names, agents.Display(agent))
			}
			out = append(out, outSkill{Name: item.Name, Path: item.CanonicalPath, Scope: item.Scope, Agents: names})
		}
		enc := json.NewEncoder(a.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}
	a.writeOut(renderSkillList(list, a.Cwd))
	return nil
}

func listScopeOrder(scope string) int {
	switch strings.ToLower(scope) {
	case "project":
		return 0
	case "global":
		return 1
	default:
		return 2
	}
}

func (a App) Remove(skillNames []string, opts RemoveOptions) error {
	targets, err := a.resolveAgents(opts.Agent)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		targets = agents.Ordered()
	}
	if opts.All || len(skillNames) == 0 {
		list, err := installer.List(&opts.Global, targets, a.Cwd)
		if err != nil {
			return err
		}
		for _, item := range list {
			skillNames = append(skillNames, item.Name)
		}
	}
	if len(skillNames) == 0 {
		a.writeOut(renderInfo("Remove skills", selectorHintStyle.Render("No skills found to remove.")))
		return nil
	}
	for _, name := range skillNames {
		if err := installer.Remove(name, targets, opts.Global, a.Cwd); err != nil {
			return err
		}
		if opts.Global {
			_ = lockfile.RemoveGlobal(name)
		} else {
			_ = lockfile.RemoveLocal(a.Cwd, name)
		}
	}
	a.writeOut(renderSuccess("Removed skills", fmt.Sprintf("%d skill%s removed", len(skillNames), skillPlural(len(skillNames)))))
	return nil
}

func (a App) Find(args []string) error {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		a.writeOut(renderFindHelp())
		return nil
	}
	queryArgs, opts, err := parseFind(args)
	if err != nil {
		return err
	}
	if opts.Providers {
		if len(queryArgs) > 0 {
			return errors.New("--providers does not accept a query")
		}
		return a.FindProviders(opts)
	}
	query := strings.TrimSpace(strings.Join(queryArgs, " "))
	if query == "" {
		return errors.New("usage: goskill find [--deep] [--refresh] [--verified] [--provider <name>] [--sort relevance|popular|newest] [--json] <query>\n       goskill find --providers")
	}
	apiBase := envDefault("SKILLS_API_URL", "https://skills.sh")
	ctx, cancel := context.WithTimeout(context.Background(), findTimeout)
	defer cancel()
	providers := []search.SearchProvider{search.NewSkillsSHProviderWithOptions(search.SkillsSHProviderOptions{
		BaseURL:         apiBase,
		RichSearchURL:   os.Getenv("SKILLS_RICH_SEARCH_URL"),
		LegacySearchURL: os.Getenv("SKILLS_SEARCH_URL"),
		AuthToken:       first(os.Getenv("SKILLS_API_TOKEN"), os.Getenv("VERCEL_OIDC_TOKEN")),
		Timeout:         findEndpointTimeout,
	})}
	if !envEnabled("GOSKILL_DISABLE_SKILLMD") {
		providers = append(providers, search.NewSkillMDProviderWithOptions(search.SkillMDProviderOptions{
			BaseURL:   os.Getenv("SKILLMD_API"),
			SearchURL: os.Getenv("SKILLMD_SEARCH_URL"),
			Timeout:   findEndpointTimeout,
		}))
	}
	if opts.Deep {
		providers = append(providers, search.NewGitHubProvider())
	}
	var trueFoundry *catalog.SearchProvider
	if !envEnabled("GOSKILL_DISABLE_TRUEFOUNDRY_CATALOG") {
		trueFoundry = catalog.NewSearchProvider(
			catalog.NewTrueFoundryProvider(os.Getenv("TRUEFOUNDRY_CATALOG_URL"), nil),
			catalog.NewStore(os.Getenv("GOSKILL_CATALOG_CACHE_DIR"), catalogTTL()),
			opts.Refresh,
		)
		providers = append(providers, trueFoundry)
	}
	optionalProviders, configuredStatuses, configurationErr := configuredSearchProviders()
	providers = append(providers, optionalProviders...)
	searchQuery := search.SearchQuery{
		Text:  query,
		Limit: 10,
	}
	response, err := search.NewAggregator(providers...).Search(ctx, searchQuery)
	if err != nil {
		return err
	}
	if !response.HasSuccessfulProvider() {
		return response.FirstError()
	}
	if configurationErr != nil {
		response.Providers = append(response.Providers, search.ProviderStatus{
			Provider: "optional provider configuration",
			Err:      configurationErr,
		})
	}
	for _, status := range configuredStatuses {
		if status.Err == nil {
			continue
		}
		response.Providers = append(response.Providers, search.ProviderStatus{
			Provider: status.Name,
			Err:      status.ProviderStatusError(),
		})
	}
	if opts.Refresh && trueFoundry != nil {
		status := trueFoundry.Status()
		switch {
		case status.RefreshErr != nil && status.Stale:
			a.writeErr(renderWarning("Catalog refresh", "Using the cached TrueFoundry catalog; refresh failed."))
		case status.RefreshErr != nil:
			a.writeErr(renderWarning("Catalog refresh", "TrueFoundry catalog refresh failed."))
		case status.Refreshed:
			a.writeErr(renderInfo("Catalog refreshed", "TrueFoundry awesome-skills-registry"))
		}
	}
	results := search.FilterAndRank(response.Results, searchQuery, search.ResultFilter{
		Verified: opts.Verified,
		Provider: opts.Provider,
	}, opts.Sort)
	if opts.JSON {
		encoded, err := renderFindJSON(results, response.Providers)
		if err != nil {
			return err
		}
		a.writeOut(encoded)
		return nil
	}
	if len(results) == 0 {
		a.writeProviderStatusIfMaterial(response.Providers)
	}
	a.writeOut(renderFindResults(query, results))
	return nil
}

func parseFind(args []string) ([]string, FindOptions, error) {
	opts := FindOptions{Sort: search.SortRelevance}
	query := make([]string, 0, len(args))
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch arg {
		case "--deep":
			opts.Deep = true
			continue
		case "--refresh":
			opts.Refresh = true
			continue
		case "--verified":
			opts.Verified = true
			continue
		case "--json":
			opts.JSON = true
			continue
		case "--providers":
			opts.Providers = true
			continue
		case "--provider", "--sort":
			if index+1 >= len(args) || strings.HasPrefix(args[index+1], "--") {
				return nil, FindOptions{}, fmt.Errorf("%s requires a value", arg)
			}
			value := strings.TrimSpace(args[index+1])
			index++
			if value == "" {
				return nil, FindOptions{}, fmt.Errorf("%s requires a value", arg)
			}
			if arg == "--provider" {
				opts.Provider = value
				continue
			}
			opts.Sort = search.SortMode(strings.ToLower(value))
			if !opts.Sort.Valid() {
				return nil, FindOptions{}, fmt.Errorf("invalid --sort value %q (want relevance, popular, or newest)", value)
			}
			continue
		}
		if value, ok := strings.CutPrefix(arg, "--provider="); ok {
			if strings.TrimSpace(value) == "" {
				return nil, FindOptions{}, errors.New("--provider requires a value")
			}
			opts.Provider = strings.TrimSpace(value)
			continue
		}
		if value, ok := strings.CutPrefix(arg, "--sort="); ok {
			opts.Sort = search.SortMode(strings.ToLower(strings.TrimSpace(value)))
			if !opts.Sort.Valid() {
				return nil, FindOptions{}, fmt.Errorf("invalid --sort value %q (want relevance, popular, or newest)", value)
			}
			continue
		}
		if strings.HasPrefix(arg, "--") {
			return nil, FindOptions{}, fmt.Errorf("unknown find option %q", arg)
		}
		query = append(query, arg)
	}
	return query, opts, nil
}

func catalogTTL() time.Duration {
	raw := strings.TrimSpace(os.Getenv("GOSKILL_CATALOG_TTL"))
	if raw == "" {
		return -1
	}
	duration, err := time.ParseDuration(raw)
	if err == nil && duration >= 0 {
		return duration
	}
	return -1
}

func (a App) Validate(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: skills validate <skills>")
	}
	var files []string
	for _, arg := range args {
		if strings.TrimSpace(arg) == "" {
			continue
		}
		sourceFiles, cleanup, err := a.validationSkillFiles(arg)
		if cleanup != nil {
			defer cleanup()
		}
		if err != nil {
			return err
		}
		if len(sourceFiles) == 0 {
			return fmt.Errorf("no SKILL.md files found in %s", arg)
		}
		files = append(files, sourceFiles...)
	}
	sort.Strings(files)
	files = uniqueStrings(files)
	issuesByPath := map[string][]skills.ValidationIssue{}
	pathsByName := map[string][]string{}
	for _, path := range files {
		issuesByPath[path] = append(issuesByPath[path], skills.ValidateSkillMD(path)...)
		if name, ok := skills.SkillMDName(path); ok {
			pathsByName[strings.ToLower(name)] = append(pathsByName[strings.ToLower(name)], path)
		}
	}
	for name, paths := range pathsByName {
		if len(paths) < 2 {
			continue
		}
		sort.Strings(paths)
		for _, path := range paths {
			issuesByPath[path] = append(issuesByPath[path], skills.ValidationIssue{Message: fmt.Sprintf("duplicate skill name %q", name)})
		}
	}
	var issueCount int
	var results []validationResult
	for _, path := range files {
		issues := issuesByPath[path]
		results = append(results, validationResult{Path: path, Issues: issues})
		for range issues {
			issueCount++
		}
	}
	a.writeOut(renderValidationResults(results, len(files), issueCount, a.Cwd))
	if issueCount > 0 {
		return fmt.Errorf("validation failed: %d issue(s)", issueCount)
	}
	return nil
}

func (a App) validationSkillFiles(rawSource string) ([]string, func(), error) {
	candidate := rawSource
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(a.Cwd, candidate)
	}
	if _, err := os.Stat(candidate); err == nil {
		abs, err := filepath.Abs(candidate)
		if err != nil {
			return nil, nil, err
		}
		files, err := validationSkillFilesFromPath(abs, "")
		return files, nil, err
	}
	parsed, err := source.Parse(rawSource)
	if err != nil {
		return nil, nil, err
	}
	switch parsed.Type {
	case source.Local:
		files, err := validationSkillFilesFromPath(parsed.LocalPath, parsed.Subpath)
		return files, nil, err
	case source.GitHub, source.GitLab, source.Git:
		tmp, cleanup, err := github.Clone(parsed.URL, parsed.Ref)
		if err != nil {
			return nil, cleanup, err
		}
		files, err := validationSkillFilesFromPath(tmp, parsed.Subpath)
		return files, cleanup, err
	default:
		return nil, nil, fmt.Errorf("validate does not support %s sources", parsed.Type)
	}
}

func validationSkillFilesFromPath(path, subpath string) ([]string, error) {
	if subpath != "" && !skills.SubpathSafe(path, subpath) {
		return nil, errors.New("invalid subpath resolves outside repository")
	}
	searchPath := path
	if subpath != "" {
		searchPath = filepath.Join(path, subpath)
	}
	info, err := os.Stat(searchPath)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		if filepath.Base(searchPath) == "SKILL.md" || filepath.Base(searchPath) == "skill.md" {
			return []string{searchPath}, nil
		}
		return nil, fmt.Errorf("%s is not a skill directory or SKILL.md file", searchPath)
	}
	if path, ok := skills.ValidationSkillFile(searchPath); ok {
		return []string{path}, nil
	}
	return skills.FindValidationSkillFiles(searchPath, 5), nil
}

func uniqueStrings(list []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, item := range list {
		if !seen[item] {
			out = append(out, item)
			seen[item] = true
		}
	}
	return out
}

func (a App) Init(args []string) error {
	name := filepath.Base(a.Cwd)
	hasName := false
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		name = args[0]
		hasName = true
	}
	dir := a.Cwd
	display := "SKILL.md"
	if hasName {
		dir = filepath.Join(a.Cwd, name)
		display = filepath.Join(name, "SKILL.md")
	}
	path := filepath.Join(dir, "SKILL.md")
	if _, err := os.Stat(path); err == nil {
		a.writeOut(renderWarning("Init skill", fmt.Sprintf("Skill already exists at %s", selectorPathStyle.Render(display))))
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	content := fmt.Sprintf("---\nname: %s\ndescription: A brief description of what this skill does\n---\n\n# %s\n\nInstructions for the agent to follow when this skill is activated.\n\n## When to use\n\nDescribe when this skill should be used.\n\n## Instructions\n\n1. First step\n2. Second step\n3. Additional steps as needed\n", name, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return err
	}
	a.writeOut(renderSuccess("Created skill", selectorPathStyle.Render(display)))
	return nil
}

func (a App) InstallFromLock(args []string) error {
	lock := lockfile.ReadLocal(a.Cwd)
	if len(lock.Skills) == 0 {
		a.writeOut(renderInfo("Install skills", selectorHintStyle.Render("No project skills found in skills-lock.json")))
		return nil
	}
	for skillName, entry := range lock.Skills {
		switch entry.SourceType {
		case "node_modules":
			if err := a.Sync([]string{"-y", "--force"}); err != nil {
				return err
			}
		default:
			sourceArg := entry.Source
			if entry.Ref != "" {
				sourceArg += "#" + entry.Ref
			}
			opts := AddOptions{Skill: []string{skillName}, Agent: []string{"codex", "cursor"}, Yes: true}
			if err := a.Add([]string{sourceArg}, opts); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a App) Sync(args []string) error {
	opts := parseSync(args)
	targets, err := a.resolveAgents(opts.Agent)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		targets = []agents.Type{agents.Codex, agents.Cursor}
	}
	discovered := discoverNodeModuleSkills(a.Cwd)
	if len(discovered) == 0 {
		a.writeOut(renderInfo("Sync skills", selectorHintStyle.Render("No SKILL.md files found in node_modules.")))
		return nil
	}
	local := lockfile.ReadLocal(a.Cwd)
	var toInstall []nodeSkill
	for _, skill := range discovered {
		if !opts.Force {
			if existing, ok := local.Skills[skill.Name]; ok {
				if hash, err := skills.FolderHash(skill.Path); err == nil && hash == existing.ComputedHash {
					continue
				}
			}
		}
		toInstall = append(toInstall, skill)
	}
	for _, skill := range toInstall {
		for _, agent := range targets {
			result := installer.InstallSkill(skill.Skill, agent, false, a.Cwd, installer.Symlink)
			if !result.Success {
				return result.Err
			}
		}
		hash, _ := skills.FolderHash(skill.Path)
		_ = lockfile.AddLocal(a.Cwd, skill.Name, lockfile.LocalEntry{Source: skill.PackageName, SourceType: "node_modules", ComputedHash: hash})
	}
	a.writeOut(renderSuccess("Synced skills", fmt.Sprintf("%d skill%s synced", len(toInstall), skillPlural(len(toInstall)))))
	return nil
}

func (a App) Check(args []string, doUpdate bool) error {
	opts := parseUpdate(args)
	var checked, updates, success int
	if opts.Global || !opts.Project {
		lock := lockfile.ReadGlobal()
		for name, entry := range lock.Skills {
			if len(opts.Skills) > 0 && !containsStringFold(opts.Skills, name) {
				continue
			}
			checked++
			if entry.SourceType != "github" || entry.SkillPath == "" || entry.SkillFolderHash == "" {
				continue
			}
			tree, ok := github.FetchRepoTree(entry.Source, entry.Ref)
			if !ok {
				continue
			}
			latest := github.SkillFolderHash(tree, entry.SkillPath)
			if latest != "" && latest != entry.SkillFolderHash {
				updates++
				a.writeOut(renderWarning("Update available", fmt.Sprintf("Update available: %s", name)))
				if doUpdate {
					sourceArg := entry.Source
					if entry.Ref != "" {
						sourceArg += "#" + entry.Ref
					}
					if err := a.Add([]string{sourceArg}, AddOptions{Global: true, Skill: []string{name}, Agent: []string{"*"}, Yes: true}); err == nil {
						success++
					}
				}
			}
		}
	}
	if opts.Project || !opts.Global {
		lock := lockfile.ReadLocal(a.Cwd)
		for name, entry := range lock.Skills {
			if len(opts.Skills) > 0 && !containsStringFold(opts.Skills, name) {
				continue
			}
			checked++
			if entry.SourceType == "node_modules" {
				continue
			}
			if entry.SkillPath == "" {
				continue
			}
			sourceArg := entry.Source
			if entry.Ref != "" {
				sourceArg += "#" + entry.Ref
			}
			if doUpdate {
				if err := a.Add([]string{sourceArg}, AddOptions{Skill: []string{name}, Agent: []string{"codex", "cursor"}, Yes: true}); err == nil {
					success++
				}
			}
		}
	}
	if !doUpdate {
		a.writeOut(renderInfo("Checked skills", fmt.Sprintf("%d skill%s checked", checked, skillPlural(checked)), fmt.Sprintf("%d update%s available", updates, skillPlural(updates))))
	} else {
		a.writeOut(renderSuccess("Updated skills", fmt.Sprintf("%d skill%s updated", success, skillPlural(success))))
	}
	return nil
}

type resolvedSkills struct {
	skills     []skills.Skill
	sourceID   string
	tree       *github.RepoTree
	basePath   string
	blobHashes map[string]string
}

func (r resolvedSkills) skillPath(skill skills.Skill) string {
	if skill.RepoPath != "" {
		return skill.RepoPath
	}
	if r.basePath == "" || skill.Path == "" {
		return ""
	}
	rel, err := filepath.Rel(r.basePath, filepath.Join(skill.Path, "SKILL.md"))
	if err != nil {
		return ""
	}
	return filepath.ToSlash(rel)
}

func (r resolvedSkills) folderHash(skill skills.Skill) string {
	if skill.Hash != "" {
		return skill.Hash
	}
	if r.tree != nil {
		return github.SkillFolderHash(*r.tree, r.skillPath(skill))
	}
	return ""
}

func (a App) resolveSkills(parsed source.Parsed, opts AddOptions) (resolvedSkills, func(), error) {
	switch parsed.Type {
	case source.Local:
		list, err := skills.Discover(parsed.LocalPath, parsed.Subpath, len(opts.Skill) > 0, opts.FullDepth)
		list = skillsWithRepoPaths(list, parsed.LocalPath)
		return resolvedSkills{skills: list, sourceID: parsed.LocalPath, basePath: parsed.LocalPath}, nil, err
	case source.WellKnown:
		wk, err := wellknown.FetchAll(parsed.URL)
		if err != nil {
			return resolvedSkills{}, nil, err
		}
		var list []skills.Skill
		for _, item := range wk {
			list = append(list, item.Skill)
		}
		return resolvedSkills{skills: list, sourceID: wellknown.SourceIdentifier(parsed.URL)}, nil, nil
	case source.ConfiguredWellKnown:
		provider, err := configuredWellKnownProvider(parsed.URL)
		if err != nil {
			return resolvedSkills{}, nil, err
		}
		results, err := provider.Search(context.Background(), search.SearchQuery{Text: parsed.SkillFilter, Limit: 10})
		if err != nil {
			return resolvedSkills{}, nil, errors.New("fetch configured well-known skill")
		}
		for _, result := range results {
			if !strings.EqualFold(result.Name, parsed.SkillFilter) || result.InstallURL == "" {
				continue
			}
			skill, err := provider.FetchSkill(result)
			if err != nil {
				return resolvedSkills{}, nil, err
			}
			return resolvedSkills{skills: []skills.Skill{skill}, sourceID: "wellknown/" + parsed.URL}, nil, nil
		}
		return resolvedSkills{}, nil, errors.New("configured well-known skill was not found")
	case source.SkillURL:
		skill, err := remoteskill.Fetch(parsed.URL)
		if err != nil {
			return resolvedSkills{}, nil, err
		}
		return resolvedSkills{skills: []skills.Skill{skill}, sourceID: parsed.URL}, nil, nil
	case source.GitHub:
		ownerRepo := source.OwnerRepo(parsed)
		if ownerRepo != "" {
			if blob, ok := github.TryBlobInstall(ownerRepo, parsed.Subpath, first(parsed.SkillFilter, ""), parsed.Ref, len(opts.Skill) > 0); ok {
				tree := blob.Tree
				return resolvedSkills{skills: blob.Skills, sourceID: ownerRepo, tree: &tree}, nil, nil
			}
		}
		fallthrough
	case source.GitLab, source.Git:
		tmp, cleanup, err := github.Clone(parsed.URL, parsed.Ref)
		if err != nil {
			return resolvedSkills{}, cleanup, err
		}
		list, err := skills.Discover(tmp, parsed.Subpath, len(opts.Skill) > 0 || parsed.SkillFilter != "", opts.FullDepth)
		if parsed.SkillFilter != "" {
			list = skills.Filter(list, []string{parsed.SkillFilter})
		}
		list = skillsWithRepoPaths(list, tmp)
		sourceID := source.OwnerRepo(parsed)
		if sourceID == "" {
			sourceID = parsed.URL
		}
		return resolvedSkills{skills: list, sourceID: sourceID, basePath: tmp}, cleanup, err
	default:
		return resolvedSkills{}, nil, errors.New("unsupported source")
	}
}

func configuredWellKnownProvider(name string) (*search.WellKnownProvider, error) {
	configs, err := search.LoadConfiguredProviders()
	if err != nil {
		return nil, err
	}
	for _, config := range configs {
		if config.Disabled || config.Kind != "well-known" || !strings.EqualFold(config.Name, name) {
			continue
		}
		providers, statuses := search.DefaultProviderRegistry().Build(
			[]search.ProviderConfig{config},
			search.EnvironmentCredentialResolver{},
		)
		if len(statuses) != 1 || statuses[0].Err != nil || len(providers) != 1 {
			return nil, errors.New("configured well-known provider is unavailable")
		}
		provider, ok := providers[0].(*search.WellKnownProvider)
		if !ok {
			return nil, errors.New("configured provider is not well-known")
		}
		return provider, nil
	}
	return nil, errors.New("configured well-known provider was not found")
}

func skillsWithRepoPaths(list []skills.Skill, basePath string) []skills.Skill {
	if basePath == "" {
		return list
	}
	out := append([]skills.Skill(nil), list...)
	for i := range out {
		if out[i].RepoPath != "" || out[i].Path == "" {
			continue
		}
		rel, err := filepath.Rel(basePath, filepath.Join(out[i].Path, "SKILL.md"))
		if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
			continue
		}
		out[i].RepoPath = filepath.ToSlash(rel)
	}
	return out
}

func (a App) resolveAgents(names []string) ([]agents.Type, error) {
	targets, invalid := agents.Validate(names)
	if len(invalid) > 0 {
		return nil, fmt.Errorf("invalid agents: %s (valid: claude-code, codex, cursor)", strings.Join(invalid, ", "))
	}
	return targets, nil
}

func parseAdd(args []string) ([]string, AddOptions, error) {
	var opts AddOptions
	var sources []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "-g", "--global":
			opts.Global = true
		case "-y", "--yes":
			opts.Yes = true
		case "-l", "--list":
			opts.List = true
		case "--all":
			opts.All = true
		case "--full-depth":
			opts.FullDepth = true
		case "--copy":
			opts.Copy = true
		case "-a", "--agent":
			for i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				opts.Agent = append(opts.Agent, args[i])
			}
		case "-s", "--skill":
			for i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				opts.Skill = append(opts.Skill, args[i])
			}
		default:
			sources = append(sources, arg)
		}
	}
	return sources, opts, nil
}

func parseRemove(args []string) ([]string, RemoveOptions, error) {
	var opts RemoveOptions
	var names []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-g", "--global":
			opts.Global = true
		case "-y", "--yes":
			opts.Yes = true
		case "--all":
			opts.All = true
		case "-a", "--agent":
			for i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				opts.Agent = append(opts.Agent, args[i])
			}
		case "-s", "--skill":
			for i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				names = append(names, args[i])
			}
		default:
			names = append(names, args[i])
		}
	}
	return names, opts, nil
}

type syncOptions struct {
	Agent []string
	Yes   bool
	Force bool
}

func parseSync(args []string) syncOptions {
	var opts syncOptions
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-y", "--yes":
			opts.Yes = true
		case "-f", "--force":
			opts.Force = true
		case "-a", "--agent":
			for i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				opts.Agent = append(opts.Agent, args[i])
			}
		}
	}
	return opts
}

type updateOptions struct {
	Global  bool
	Project bool
	Yes     bool
	Skills  []string
}

func parseUpdate(args []string) updateOptions {
	var opts updateOptions
	for _, arg := range args {
		switch arg {
		case "-g", "--global":
			opts.Global = true
		case "-p", "--project":
			opts.Project = true
		case "-y", "--yes":
			opts.Yes = true
		default:
			opts.Skills = append(opts.Skills, arg)
		}
	}
	return opts
}

type nodeSkill struct {
	skills.Skill
	PackageName string
}

func discoverNodeModuleSkills(cwd string) []nodeSkill {
	root := filepath.Join(cwd, "node_modules")
	top, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []nodeSkill
	process := func(pkgDir, pkgName string) {
		if skill, ok := skills.ParseSkillMD(filepath.Join(pkgDir, "SKILL.md"), false); ok {
			out = append(out, nodeSkill{Skill: skill, PackageName: pkgName})
			return
		}
		for _, dir := range []string{pkgDir, filepath.Join(pkgDir, "skills"), filepath.Join(pkgDir, ".agents", "skills")} {
			entries, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}
				if skill, ok := skills.ParseSkillMD(filepath.Join(dir, entry.Name(), "SKILL.md"), false); ok {
					out = append(out, nodeSkill{Skill: skill, PackageName: pkgName})
				}
			}
		}
	}
	for _, entry := range top {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		full := filepath.Join(root, entry.Name())
		if strings.HasPrefix(entry.Name(), "@") {
			scoped, _ := os.ReadDir(full)
			for _, child := range scoped {
				if child.IsDir() {
					process(filepath.Join(full, child.Name()), entry.Name()+"/"+child.Name())
				}
			}
			continue
		}
		process(full, entry.Name())
	}
	return out
}

func (a App) banner() {
	a.writeOut(renderBanner())
}

func (a App) help() {
	a.writeOut(renderHelp())
}

func shorten(path, cwd string) string {
	home, _ := os.UserHomeDir()
	if rel, err := filepath.Rel(cwd, path); err == nil && !strings.HasPrefix(rel, "..") {
		return "." + string(filepath.Separator) + rel
	}
	if rel, err := filepath.Rel(home, path); err == nil && !strings.HasPrefix(rel, "..") {
		return "~" + string(filepath.Separator) + rel
	}
	return path
}

func envDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envEnabled(key string) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

func containsStringFold(list []string, value string) bool {
	for _, item := range list {
		if strings.EqualFold(item, value) {
			return true
		}
	}
	return false
}

func first(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
