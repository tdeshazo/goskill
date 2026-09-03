package commands

import (
	"fmt"
	"strings"

	"github.com/tdeshazo/goskill/internal/agents"
)

func (a App) Agent(args []string) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(a.Stdout, agentHelp())
		return nil
	}
	switch args[0] {
	case "list":
		if len(args) != 1 {
			return fmt.Errorf("usage: goskill agent list")
		}
		registry, err := a.agentRegistry()
		if err != nil {
			return err
		}
		a.writeOut(renderAgentList(registry))
		return nil
	case "show":
		if len(args) != 2 {
			return fmt.Errorf("usage: goskill agent show <id>")
		}
		registry, err := a.agentRegistry()
		if err != nil {
			return err
		}
		entry, ok := registry.Get(agents.Type(args[1]))
		if !ok {
			return fmt.Errorf("unknown agent %q; run goskill agent list", args[1])
		}
		a.writeOut(renderAgentShow(entry, registry, a.Cwd))
		return nil
	case "validate":
		if len(args) != 2 {
			return fmt.Errorf("usage: goskill agent validate <file>")
		}
		configs, err := agents.LoadFile(args[1])
		if err != nil {
			return err
		}
		names := make([]string, len(configs))
		for i, config := range configs {
			names[i] = string(config.Name)
		}
		a.writeOut(renderSuccess("Agent configuration valid", strings.Join(names, ", ")))
		return nil
	default:
		return fmt.Errorf("unknown agent command %q\n\n%s", args[0], agentHelp())
	}
}

func renderAgentList(registry *agents.Registry) string {
	lines := []string{"Configured agents:"}
	for _, entry := range registry.List() {
		lines = append(lines, fmt.Sprintf("  %-16s %-14s %s", entry.Config.Name, "("+string(entry.Origin)+")", entry.Config.DisplayName))
	}
	return strings.Join(lines, "\n") + "\n"
}

func renderAgentShow(entry agents.Entry, registry *agents.Registry, cwd string) string {
	config := entry.Config
	projectDir, _ := registry.BaseDir(config.Name, false, cwd)
	globalDir, _ := registry.BaseDir(config.Name, true, cwd)
	lines := []string{
		"Agent: " + string(config.Name),
		"Display name: " + config.DisplayName,
		"Origin: " + string(entry.Origin),
		"Project skills: " + projectDir,
		"Global skills: " + globalDir,
	}
	if config.Command != "" {
		lines = append(lines, "Command: "+config.Command)
	}
	return strings.Join(lines, "\n") + "\n"
}

func agentHelp() string {
	return `Usage: goskill agent <command>

Inspect and validate declarative agent definitions.

Commands:
  list                 List built-in and user-defined agents
  show <id>            Show one resolved agent definition
  validate <file>      Validate an agent YAML file
`
}
