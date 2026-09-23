package commands

import (
	"strconv"
	"strings"

	cobra_usage "github.com/jdx/usage/integrations/cobra"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	usageSpecFlagRepeatableAnnotation = "goskill.io/usage-spec/flag-repeatable"
	usageSpecArgVariadicAnnotation    = "goskill.io/usage-spec/arg-variadic"
)

// markUsageFlagVariadic records cardinality metadata for the Usage exporter.
// The annotations do not affect pflag parsing.
func markUsageFlagVariadic(flag *pflag.Flag) {
	setUsageFlagAnnotation(flag, usageSpecFlagRepeatableAnnotation, true)
	setUsageFlagAnnotation(flag, usageSpecArgVariadicAnnotation, true)
}

// markUsageFlagNonRepeatable documents StringArray flags whose command-level
// validation accepts only one value.
func markUsageFlagNonRepeatable(flag *pflag.Flag) {
	setUsageFlagAnnotation(flag, usageSpecFlagRepeatableAnnotation, false)
	setUsageFlagAnnotation(flag, usageSpecArgVariadicAnnotation, false)
}

func setUsageFlagAnnotation(flag *pflag.Flag, key string, enabled bool) {
	if flag == nil {
		return
	}
	if flag.Annotations == nil {
		flag.Annotations = make(map[string][]string)
	}
	flag.Annotations[key] = []string{strconv.FormatBool(enabled)}
}

type usageSpecFlagKey struct {
	commandPath string
	name        string
}

type usageSpecFlagCardinality struct {
	repeatable  bool
	argVariadic bool
}

// generateUsageSpec supplements the pinned Cobra exporter with cardinality
// annotations from each Cobra flag definition. The exporter currently writes
// flag arguments but omits the pflag-level repeatability of StringArray flags.
func generateUsageSpec(root *cobra.Command) string {
	cardinality := make(map[usageSpecFlagKey]usageSpecFlagCardinality)
	collectUsageSpecCardinality(root, nil, cardinality)
	return addUsageSpecCardinality(cobra_usage.Generate(root), cardinality)
}

func collectUsageSpecCardinality(cmd *cobra.Command, commandPath []string, out map[usageSpecFlagKey]usageSpecFlagCardinality) {
	cmd.LocalFlags().VisitAll(func(flag *pflag.Flag) {
		if flag.Value.Type() != "stringArray" {
			return
		}
		repeatable := usageFlagAnnotationEnabled(flag, usageSpecFlagRepeatableAnnotation)
		argVariadic := usageFlagAnnotationEnabled(flag, usageSpecArgVariadicAnnotation)
		if !repeatable && !argVariadic {
			return
		}

		out[usageSpecFlagKey{
			commandPath: strings.Join(commandPath, " "),
			name:        usageSpecFlagName(flag),
		}] = usageSpecFlagCardinality{repeatable: repeatable, argVariadic: argVariadic}
	})

	for _, child := range cmd.Commands() {
		childPath := append(append([]string(nil), commandPath...), child.Name())
		collectUsageSpecCardinality(child, childPath, out)
	}
}

func usageFlagAnnotationEnabled(flag *pflag.Flag, key string) bool {
	if flag.Annotations == nil {
		return false
	}
	values := flag.Annotations[key]
	return len(values) > 0 && values[0] == "true"
}

func usageSpecFlagName(flag *pflag.Flag) string {
	var parts []string
	if flag.Shorthand != "" {
		parts = append(parts, "-"+flag.Shorthand)
	}
	if flag.Name != "" {
		parts = append(parts, "--"+flag.Name)
	}
	return strings.Join(parts, " ")
}

func addUsageSpecCardinality(spec string, cardinality map[usageSpecFlagKey]usageSpecFlagCardinality) string {
	trailingNewline := strings.HasSuffix(spec, "\n")
	lines := strings.Split(strings.TrimSuffix(spec, "\n"), "\n")
	var commandStack []usageSpecCommand
	commandPath := ""
	markNextArgVariadic := false

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " \t"))

		if markNextArgVariadic {
			if strings.HasPrefix(trimmed, "arg ") {
				lines[i] = addUsageSpecVar(lines[i])
			}
			markNextArgVariadic = false
		}

		if trimmed == "}" {
			commandStack = popUsageSpecCommands(commandStack, indent)
			commandPath = usageSpecCommandPath(commandStack)
			continue
		}

		if strings.HasPrefix(trimmed, "cmd ") {
			commandStack = popUsageSpecCommands(commandStack, indent)
			name, ok := usageSpecNodeValue(trimmed, "cmd")
			if !ok {
				commandPath = usageSpecCommandPath(commandStack)
				continue
			}
			if strings.HasSuffix(trimmed, " {") {
				commandStack = append(commandStack, usageSpecCommand{name: name, indent: indent})
			}
			commandPath = usageSpecCommandPath(commandStack)
			continue
		}

		if !strings.HasPrefix(trimmed, "flag ") {
			continue
		}

		name, ok := usageSpecNodeValue(trimmed, "flag")
		if !ok {
			continue
		}
		card, ok := cardinality[usageSpecFlagKey{commandPath: commandPath, name: name}]
		if !ok {
			continue
		}
		if card.repeatable {
			lines[i] = addUsageSpecVar(lines[i])
		}
		markNextArgVariadic = card.argVariadic
	}

	result := strings.Join(lines, "\n")
	if trailingNewline {
		result += "\n"
	}
	return result
}

type usageSpecCommand struct {
	name   string
	indent int
}

func popUsageSpecCommands(stack []usageSpecCommand, indent int) []usageSpecCommand {
	for len(stack) > 0 && stack[len(stack)-1].indent >= indent {
		stack = stack[:len(stack)-1]
	}
	return stack
}

func usageSpecCommandPath(stack []usageSpecCommand) string {
	parts := make([]string, len(stack))
	for i, command := range stack {
		parts[i] = command.name
	}
	return strings.Join(parts, " ")
}

func usageSpecNodeValue(line, node string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, node+" ") {
		return "", false
	}
	value := strings.TrimSpace(strings.TrimPrefix(trimmed, node))
	if value == "" {
		return "", false
	}
	if value[0] != '"' {
		end := strings.IndexAny(value, " \t{")
		if end < 0 {
			end = len(value)
		}
		return value[:end], end > 0
	}

	escaped := false
	for i := 1; i < len(value); i++ {
		if escaped {
			escaped = false
			continue
		}
		switch value[i] {
		case '\\':
			escaped = true
		case '"':
			decoded, err := strconv.Unquote(value[:i+1])
			return decoded, err == nil
		}
	}
	return "", false
}

func addUsageSpecVar(line string) string {
	if strings.Contains(line, " var=#true") {
		return line
	}
	trimmed := strings.TrimRight(line, " \t")
	if strings.HasSuffix(trimmed, " {") {
		return strings.TrimSuffix(trimmed, " {") + " var=#true {"
	}
	return trimmed + " var=#true"
}
