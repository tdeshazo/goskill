package main

import (
	"os"

	"github.com/tdeshazo/goskill/internal/commands"
)

var version = "0.2.4"

func main() {
	app := commands.New(version)
	if err := app.Run(os.Args[1:]); err != nil {
		if code, ok := commands.ExitCode(err); ok {
			os.Exit(code)
		}
		commands.WriteRendered(os.Stderr, commands.RenderError(err))
		os.Exit(1)
	}
}
