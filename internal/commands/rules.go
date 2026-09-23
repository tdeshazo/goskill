package commands

import (
	"encoding/json"
	"fmt"

	"github.com/tdeshazo/goskill/internal/skills"
)

func (a App) rules(jsonOut bool) error {
	rules := skills.Rules()
	if jsonOut {
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

func (a App) explain(code string, jsonOut bool) error {
	rule, ok := skills.RuleForCode(code)
	if !ok {
		return fmt.Errorf("unknown rule code %q; run goskill rules to list available rules", skills.NormalizeRuleCode(code))
	}
	if jsonOut {
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
