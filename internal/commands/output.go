package commands

import (
	"fmt"
	"io"
	"os"

	"github.com/tdeshazo/goskill/internal/terminal"
)

func (a App) writeOut(s string) {
	writeRendered(a.Stdout, s)
}

func (a App) writeErr(s string) {
	writeRendered(a.Stderr, s)
}

func writeRendered(w io.Writer, s string) {
	if w == nil {
		return
	}
	if !writerIsTerminal(w) {
		s = terminal.StripEscapes(s)
	}
	fmt.Fprint(w, s)
}

func WriteRendered(w io.Writer, s string) {
	writeRendered(w, s)
}

func writerIsTerminal(w io.Writer) bool {
	file, ok := w.(*os.File)
	return ok && isTerminalFile(file)
}
