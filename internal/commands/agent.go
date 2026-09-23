package commands

import (
	"fmt"
	"strings"

	"github.com/tdeshazo/goskill/internal/agents"
)

func (a App) agentList() error {
	registry, err := a.agentRegistry()
	if err != nil {
		return err
	}
	a.writeOut(renderAgentList(registry))
	return nil
}

func (a App) agentShow(id string) error {
	registry, err := a.agentRegistry()
	if err != nil {
		return err
	}
	entry, ok := registry.Get(agents.Type(id))
	if !ok {
		return fmt.Errorf("unknown agent %q; run goskill agent list", id)
	}
	a.writeOut(renderAgentShow(entry, registry, a.Cwd))
	return nil
}

func (a App) agentValidate(file string) error {
	configs, err := agents.LoadFile(file)
	if err != nil {
		return err
	}
	names := make([]string, len(configs))
	for i, config := range configs {
		names[i] = string(config.Name)
	}
	a.writeOut(renderSuccess("Agent configuration valid", strings.Join(names, ", ")))
	return nil
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
