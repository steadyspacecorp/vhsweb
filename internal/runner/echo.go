package runner

import (
	"fmt"
	"strings"

	"github.com/steadyspacecorp/vhsweb/internal/parser"
)

// ANSI 256-color foreground codes used to highlight echoed tape lines by token
// role, mirroring how charmbracelet/vhs colors its script output.
const (
	colReset   = "\x1b[0m"
	colKeyword = "\x1b[38;5;141m" // lavender — the command keyword
	colString  = "\x1b[38;5;114m" // green — string / path / selector args
	colNumber  = "\x1b[38;5;216m" // peach — durations (e.g. 60ms, 3s)
)

// Colorize renders a command as a .tape line, syntax-highlighted by token role
// when enabled. With enabled false it returns the plain serialization, so piped
// or NO_COLOR output stays free of escape codes.
func Colorize(cmd parser.Command, enabled bool) string {
	if !enabled {
		return cmd.String()
	}
	out := colKeyword + string(cmd.Type) + colReset
	for i, a := range cmd.Args {
		tok := a
		if a == "" || strings.ContainsAny(a, " \t\"'") {
			tok = fmt.Sprintf("%q", a) // re-quote args with whitespace/quotes
		}
		if c := argColor(cmd.Type, i, a); c != "" {
			tok = c + tok + colReset
		}
		out += " " + tok
	}
	return out
}

// argColor picks a color for argument i of a command. The first arg of Set is a
// setting name and bare integers are left default (uncolored); durations are
// peach; everything else is treated as a string.
func argColor(t parser.CommandType, i int, a string) string {
	if t == parser.CmdSet && i == 0 {
		return ""
	}
	if isInteger(a) {
		return ""
	}
	if _, err := parseDuration(a); err == nil {
		return colNumber
	}
	return colString
}

// isInteger reports whether s is a bare (optionally negative) integer.
func isInteger(s string) bool {
	if s == "" || s == "-" {
		return false
	}
	for i, r := range s {
		if r == '-' && i == 0 {
			continue
		}
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
