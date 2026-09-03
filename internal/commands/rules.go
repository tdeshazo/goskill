package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/tdeshazo/goskill/internal/skills"
)

const (
	rulesUsage   = "usage: goskill rules [--json]"
	explainUsage = "usage: goskill explain [--json] <code>"
)

type ruleCommandOptions struct {
	JSON bool
	Help bool
}

// Rules lists the complete stable validation rule catalog.
func (a App) Rules(args []string) error {
	opts, err := parseRuleCommandOptions(args, rulesUsage)
	if err != nil {
		return err
	}
	if opts.Help {
		a.writeOut(renderRulesHelp())
		return nil
	}
	rules := skills.Rules()
	if opts.JSON {
		output, err := renderRulesJSON(rules)
		if err != nil {
			return fmt.Errorf("encode rules output: %w", err)
		}
		a.writeOut(output)
		return nil
	}
	a.writeOut(renderRulesOutput(rules))
	return nil
}

// Explain reports the catalog-backed details for one stable rule code.
func (a App) Explain(args []string) error {
	opts, code, err := parseExplain(args)
	if err != nil {
		return err
	}
	if opts.Help {
		a.writeOut(renderExplainHelp())
		return nil
	}
	rule, ok := skills.RuleForCode(code)
	if !ok {
		return fmt.Errorf("unknown rule code %q; run goskill rules to list available rules", skills.NormalizeRuleCode(code))
	}
	if opts.JSON {
		output, err := renderRuleJSON(rule)
		if err != nil {
			return fmt.Errorf("encode rule output: %w", err)
		}
		a.writeOut(output)
		return nil
	}
	a.writeOut(renderRuleOutput(rule))
	return nil
}

func parseRuleCommandOptions(args []string, usage string) (ruleCommandOptions, error) {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		return ruleCommandOptions{Help: true}, nil
	}
	opts := ruleCommandOptions{}
	for _, arg := range args {
		switch arg {
		case "--json":
			if opts.JSON {
				return ruleCommandOptions{}, errors.New("--json may only be specified once")
			}
			opts.JSON = true
		case "--help", "-h":
			return ruleCommandOptions{}, errors.New(usage)
		default:
			return ruleCommandOptions{}, fmt.Errorf("unknown rules option %q\n%s", arg, usage)
		}
	}
	return opts, nil
}

func parseExplain(args []string) (ruleCommandOptions, string, error) {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		return ruleCommandOptions{Help: true}, "", nil
	}
	opts := ruleCommandOptions{}
	var codes []string
	for _, arg := range args {
		switch arg {
		case "--json":
			if opts.JSON {
				return ruleCommandOptions{}, "", errors.New("--json may only be specified once")
			}
			opts.JSON = true
		case "--help", "-h":
			return ruleCommandOptions{}, "", errors.New(explainUsage)
		default:
			if strings.HasPrefix(arg, "--") {
				return ruleCommandOptions{}, "", fmt.Errorf("unknown explain option %q\n%s", arg, explainUsage)
			}
			codes = append(codes, arg)
		}
	}
	if len(codes) != 1 {
		return ruleCommandOptions{}, "", errors.New(explainUsage)
	}
	return opts, codes[0], nil
}

// JSON renderers intentionally bypass terminal styling so machine output is
// deterministic and contains no ANSI control sequences.
func renderRulesJSON(rules []skills.Rule) (string, error) {
	encoded, err := json.MarshalIndent(rules, "", "  ")
	if err != nil {
		return "", err
	}
	return string(encoded) + "\n", nil
}

func renderRuleJSON(rule skills.Rule) (string, error) {
	encoded, err := json.MarshalIndent(rule, "", "  ")
	if err != nil {
		return "", err
	}
	return string(encoded) + "\n", nil
}
