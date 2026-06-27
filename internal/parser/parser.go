// Package parser turns a .tape script into a sequence of commands.
//
// The format is line-oriented and VHS-like. Each non-blank, non-comment line
// is one command: a keyword followed by space-separated arguments, where
// quoted strings ("..." or '...') are treated as a single argument.
package parser

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// CommandType enumerates the supported .tape keywords.
type CommandType string

const (
	CmdOutput     CommandType = "Output"
	CmdSet        CommandType = "Set"
	CmdGoto       CommandType = "Goto"
	CmdType       CommandType = "Type"
	CmdClick      CommandType = "Click"
	CmdFill       CommandType = "Fill"
	CmdPress      CommandType = "Press"
	CmdHover      CommandType = "Hover"
	CmdScroll     CommandType = "Scroll"
	CmdWaitFor    CommandType = "WaitFor"
	CmdSleep      CommandType = "Sleep"
	CmdScreenshot CommandType = "Screenshot"
	CmdHide       CommandType = "Hide"
	CmdShow       CommandType = "Show"
	CmdSource     CommandType = "Source"
)

// commandArity maps each command to its minimum number of arguments.
var commandArity = map[CommandType]int{
	CmdOutput:     1,
	CmdSet:        2,
	CmdGoto:       1,
	CmdType:       1,
	CmdClick:      1,
	CmdFill:       2,
	CmdPress:      1,
	CmdHover:      1,
	CmdScroll:     1,
	CmdWaitFor:    1,
	CmdSleep:      1,
	CmdScreenshot: 1,
	CmdHide:       0,
	CmdShow:       0,
	CmdSource:     1,
}

// Command is a single parsed instruction from a .tape file.
type Command struct {
	Type CommandType
	Args []string
	Line int // 1-based source line, for error messages
}

// Parse reads a .tape script and returns the ordered list of commands. Any
// Source includes are resolved relative to the current working directory.
func Parse(r io.Reader) ([]Command, error) {
	return ParseWithBase(r, "")
}

// ParseWithBase parses a tape, resolving relative Source includes against
// baseDir (typically the directory of the tape file; empty means cwd).
func ParseWithBase(r io.Reader, baseDir string) ([]Command, error) {
	return parseStream(r, baseDir, map[string]bool{})
}

// ParseFile reads and parses the tape at path, resolving Source includes
// relative to the file's own directory.
func ParseFile(path string) ([]Command, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	seen := map[string]bool{}
	if abs, err := filepath.Abs(path); err == nil {
		seen[abs] = true
	}
	return parseStream(f, filepath.Dir(path), seen)
}

// parseStream is the shared parsing core. baseDir resolves relative Source
// paths; seen tracks already-included files to break cycles.
func parseStream(r io.Reader, baseDir string, seen map[string]bool) ([]Command, error) {
	var cmds []Command
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		tokens, err := tokenize(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNo, err)
		}
		if len(tokens) == 0 {
			continue
		}

		keyword := CommandType(tokens[0])
		arity, ok := commandArity[keyword]
		if !ok {
			return nil, fmt.Errorf("line %d: unknown command %q", lineNo, tokens[0])
		}

		args := tokens[1:]
		if len(args) < arity {
			return nil, fmt.Errorf("line %d: %s requires at least %d argument(s), got %d",
				lineNo, keyword, arity, len(args))
		}

		if keyword == CmdSource {
			included, err := source(args[0], baseDir, seen)
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", lineNo, err)
			}
			cmds = append(cmds, included...)
			continue
		}

		cmds = append(cmds, Command{Type: keyword, Args: args, Line: lineNo})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return cmds, nil
}

// source resolves and parses a Source-included tape, guarding against cycles.
func source(ref, baseDir string, seen map[string]bool) ([]Command, error) {
	path := ref
	if !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	if seen[abs] {
		return nil, fmt.Errorf("Source cycle: %s already included", ref)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("Source: %w", err)
	}
	defer f.Close()

	// Copy the seen set down each branch so siblings can include the same file.
	next := make(map[string]bool, len(seen)+1)
	for k := range seen {
		next[k] = true
	}
	next[abs] = true
	return parseStream(f, filepath.Dir(path), next)
}

// tokenize splits a line into tokens, honoring single and double quotes.
func tokenize(line string) ([]string, error) {
	var (
		tokens  []string
		current strings.Builder
		inQuote rune
		started bool
	)

	flush := func() {
		if started {
			tokens = append(tokens, current.String())
			current.Reset()
			started = false
		}
	}

	for _, r := range line {
		switch {
		case inQuote != 0:
			if r == inQuote {
				inQuote = 0
			} else {
				current.WriteRune(r)
			}
		case r == '"' || r == '\'':
			inQuote = r
			started = true // an empty "" is still a token
		case r == ' ' || r == '\t':
			flush()
		default:
			current.WriteRune(r)
			started = true
		}
	}
	if inQuote != 0 {
		return nil, fmt.Errorf("unterminated quote")
	}
	flush()
	return tokens, nil
}
