package main

import (
	"os"

	"github.com/tdeshazo/goskill/internal/buildinfo"
	"github.com/tdeshazo/goskill/internal/commands"
)

func main() {
	app := commands.New(buildinfo.Version)
	if err := app.Run(os.Args[1:]); err != nil {
		if code, ok := commands.ExitCode(err); ok {
			os.Exit(code)
		}
		commands.WriteRendered(os.Stderr, commands.RenderError(err))
		os.Exit(1)
	}
}
