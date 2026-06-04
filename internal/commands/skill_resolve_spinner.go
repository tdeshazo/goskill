package commands

import (
	"fmt"
	"io"
	"time"

	"github.com/tdeshazo/goskill/internal/source"
)

var skillResolveSpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type skillResolveResult struct {
	resolved resolvedSkills
	cleanup  func()
	err      error
}

func (a App) resolveSkillsForAdd(parsed source.Parsed, opts AddOptions) (resolvedSkills, func(), error) {
	if !a.shouldShowResolveSpinner(opts) {
		return a.resolveSkills(parsed, opts)
	}

	result := make(chan skillResolveResult, 1)
	go func() {
		resolved, cleanup, err := a.resolveSkills(parsed, opts)
		result <- skillResolveResult{resolved: resolved, cleanup: cleanup, err: err}
	}()
	return a.waitForSkillResolveWithSpinner(skillResolveSourceLabel(parsed, a.Cwd), result)
}

func (a App) shouldShowResolveSpinner(opts AddOptions) bool {
	return a.canUseInteractiveSelector() && len(opts.Skill) == 0 && !opts.Yes && !opts.List
}

func (a App) waitForSkillResolveWithSpinner(label string, result <-chan skillResolveResult) (resolvedSkills, func(), error) {
	ticker := time.NewTicker(90 * time.Millisecond)
	defer ticker.Stop()

	frame := 0
	renderSkillResolveSpinner(a.Stdout, label, frame)
	defer clearSkillResolveSpinner(a.Stdout)

	for {
		select {
		case outcome := <-result:
			return outcome.resolved, outcome.cleanup, outcome.err
		case <-ticker.C:
			frame++
			renderSkillResolveSpinner(a.Stdout, label, frame)
		}
	}
}

func renderSkillResolveSpinner(w io.Writer, label string, frame int) {
	message := fmt.Sprintf("Loading skills from %s", label)
	fmt.Fprintf(w, "\r%s  %s", selectorActiveStyle.Render(skillResolveSpinnerFrames[frame%len(skillResolveSpinnerFrames)]), selectorHintStyle.Render(message))
}

func clearSkillResolveSpinner(w io.Writer) {
	fmt.Fprint(w, "\r\033[2K")
}

func skillResolveSourceLabel(parsed source.Parsed, cwd string) string {
	if parsed.LocalPath != "" {
		return shorten(parsed.LocalPath, cwd)
	}
	if ownerRepo := source.OwnerRepo(parsed); ownerRepo != "" {
		return ownerRepo
	}
	if parsed.URL != "" {
		return parsed.URL
	}
	return string(parsed.Type)
}
