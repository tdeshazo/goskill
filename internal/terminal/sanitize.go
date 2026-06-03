package terminal

import (
	"regexp"
	"strings"
)

var (
	csiRE     = regexp.MustCompile(`\x1b\[[\x30-\x3f]*[\x20-\x2f]*[\x40-\x7e]`)
	oscRE     = regexp.MustCompile(`\x1b\][\s\S]*?(?:\x07|\x1b\\)`)
	dcsRE     = regexp.MustCompile(`\x1b[P^_][\s\S]*?(?:\x1b\\)`)
	simpleRE  = regexp.MustCompile(`\x1b[\x20-\x7e]`)
	c1RE      = regexp.MustCompile(`[\x80-\x9f]`)
	controlRE = regexp.MustCompile(`[\x00-\x06\x07\x08\x0b\x0c\x0d-\x1a\x1c-\x1f\x7f]`)
)

func StripEscapes(s string) string {
	s = oscRE.ReplaceAllString(s, "")
	s = dcsRE.ReplaceAllString(s, "")
	s = csiRE.ReplaceAllString(s, "")
	s = simpleRE.ReplaceAllString(s, "")
	s = c1RE.ReplaceAllString(s, "")
	return controlRE.ReplaceAllString(s, "")
}

func Metadata(s string) string {
	s = StripEscapes(s)
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}
