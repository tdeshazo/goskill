package main

import (
	"fmt"
	"os"

	"github.com/tdeshazo/goskill/internal/commands"
)

var version = "0.1.0"

func main() {
	app := commands.New(version)
	if err := app.Run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
