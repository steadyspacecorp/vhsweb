package parser

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	src := `
# a comment
Output demo.mp4
Set Width 1280
Goto https://example.com
Type "hello world"
Click 'text=Sign in'
Sleep 1s
`
	cmds, err := Parse(strings.NewReader(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	want := []struct {
		typ  CommandType
		args []string
	}{
		{CmdOutput, []string{"demo.mp4"}},
		{CmdSet, []string{"Width", "1280"}},
		{CmdGoto, []string{"https://example.com"}},
		{CmdType, []string{"hello world"}},   // spaces preserved inside quotes
		{CmdClick, []string{"text=Sign in"}}, // single quotes work too
		{CmdSleep, []string{"1s"}},
	}
	if len(cmds) != len(want) {
		t.Fatalf("got %d commands, want %d", len(cmds), len(want))
	}
	for i, w := range want {
		if cmds[i].Type != w.typ {
			t.Errorf("cmd %d: type = %q, want %q", i, cmds[i].Type, w.typ)
		}
		if strings.Join(cmds[i].Args, "|") != strings.Join(w.args, "|") {
			t.Errorf("cmd %d: args = %v, want %v", i, cmds[i].Args, w.args)
		}
	}
}

func TestParseErrors(t *testing.T) {
	cases := map[string]string{
		"unknown command":  "Frobnicate foo",
		"too few args":     "Fill #email",
		"unterminated quote": `Type "oops`,
	}
	for name, src := range cases {
		if _, err := Parse(strings.NewReader(src)); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
	}
}
