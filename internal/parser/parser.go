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
}

// Command is a single parsed instruction from a .tape file.
type Command struct {
	Type CommandType
	Args []string
	Line int // 1-based source line, for error messages
}

// Parse reads a .tape script and returns the ordered list of commands.
func Parse(r io.Reader) ([]Command, error) {
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

		cmds = append(cmds, Command{Type: keyword, Args: args, Line: lineNo})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return cmds, nil
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
