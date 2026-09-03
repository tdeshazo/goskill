package commands

import (
	"errors"

	"github.com/tdeshazo/goskill/internal/skills"
)

// Spec reports the immutable Agent Skills specification snapshot used by
// goskill. It intentionally performs no network access.
func (a App) Spec(args []string) error {
	switch {
	case len(args) == 0:
		a.writeOut(renderSpecOutput())
		return nil
	case len(args) == 1 && args[0] == "--revision":
		a.writeOut(skills.SpecRevision + "\n")
		return nil
	case len(args) == 1 && (args[0] == "--help" || args[0] == "-h"):
		a.writeOut(renderSpecHelp())
		return nil
	default:
		return errors.New("usage: goskill spec [--revision]")
	}
}
