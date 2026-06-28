// Command vhsweb records a web page session described by a .tape script to video.
//
// Usage:
//
//	vhsweb example.tape            record the session described by example.tape
//	vhsweb -o out.gif example.tape override the tape's Output (repeatable)
//	vhsweb --preview example.tape  watch the run in a real window, record nothing
//	vhsweb validate example.tape   parse-check a tape without recording
//	vhsweb new example.tape        write a starter .tape file
//	vhsweb install                 download the Playwright browser binaries
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/steadyspacecorp/vhsweb/internal/parser"
	"github.com/steadyspacecorp/vhsweb/internal/runner"
	"github.com/playwright-community/playwright-go"
)

// version is overwritten at release time via -ldflags "-X main.version=...".
var version = "dev"

const starterTape = `# vhsweb tape — drives a web page and records it to video.
# Run with: vhsweb this-file.tape

Output demo.mp4
Set Width 1280
Set Height 720
Set Zoom 2
Set Framerate 30
Set Sound true

Goto https://playwright.dev
Sleep 1s
Click "text=Get started"
WaitFor "h1"
Sleep 1s
Scroll Down 600
Sleep 2s
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "vhsweb: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return nil
	}

	switch args[0] {
	case "new":
		if len(args) < 2 {
			return fmt.Errorf("new requires a filename: vhsweb new example.tape")
		}
		return cmdNew(args[1])
	case "install":
		return playwright.Install(&playwright.RunOptions{Browsers: []string{"chromium"}})
	case "record":
		return cmdRecordSession(args[1:])
	case "validate":
		return cmdValidate(args[1:])
	case "-h", "--help", "help":
		usage()
		return nil
	case "-v", "--version", "version":
		fmt.Printf("vhsweb %s\n", version)
		return nil
	}

	// Otherwise: record (or, with --preview, just watch) a tape file.
	preview := false
	quiet := false
	var outputs []string
	var path string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "--preview", "-p":
			preview = true
		case "--quiet", "-q":
			quiet = true
		case "--output", "-o":
			if i+1 >= len(args) {
				return fmt.Errorf("%s requires a filename", a)
			}
			i++
			outputs = append(outputs, args[i])
		default:
			if a != "-" && strings.HasPrefix(a, "-") {
				return fmt.Errorf("unknown flag %q", a)
			}
			if path != "" {
				return fmt.Errorf("unexpected argument %q", a)
			}
			path = a
		}
	}
	return cmdRecord(path, outputs, quiet, preview)
}

// openTape returns a reader for the tape at path, a display name, and the base
// directory for resolving relative Source includes. An empty path (or "-")
// reads from stdin when input is piped in (Source then resolves against cwd).
func openTape(path string) (io.ReadCloser, string, string, error) {
	if path == "" || path == "-" {
		if fi, err := os.Stdin.Stat(); err == nil && fi.Mode()&os.ModeCharDevice == 0 {
			return os.Stdin, "<stdin>", "", nil
		}
		return nil, "", "", fmt.Errorf("no tape file given (try: vhsweb help)")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, "", "", err
	}
	return f, path, filepath.Dir(path), nil
}

func cmdNew(path string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists", path)
	}
	if err := os.WriteFile(path, []byte(starterTape), 0o644); err != nil {
		return err
	}
	fmt.Printf("Wrote %s\n", path)
	return nil
}

func cmdRecord(path string, outputs []string, quiet, preview bool) error {
	r, name, baseDir, err := openTape(path)
	if err != nil {
		return err
	}
	defer r.Close()

	cfg, actions, cmds, err := loadTape(r, name, baseDir)
	if err != nil {
		return err
	}
	cfg.Outputs = outputs
	cfg.Verbose = !quiet
	cfg.Color = cfg.Verbose && colorEnabled()

	if preview {
		// Watch the run in a real window; record and encode nothing.
		cfg.Preview = true
		cfg.Headless = false
		cfg.Sound = false
		logf(quiet, "Previewing %s (%dx%d, no recording)\n", name, cfg.Width, cfg.Height)
		echoSettings(cfg, cmds)
		return runner.Run(cfg, actions)
	}

	for _, dst := range cfg.Outputs {
		logf(quiet, "Recording %s -> %s (%dx%d)\n", name, dst, cfg.Width, cfg.Height)
	}
	if len(cfg.Outputs) == 0 {
		logf(quiet, "Recording %s -> %s (%dx%d)\n", name, cfg.Output, cfg.Width, cfg.Height)
	}
	echoSettings(cfg, cmds)
	if err := runner.Run(cfg, actions); err != nil {
		return err
	}
	logf(quiet, "Done\n")
	return nil
}

// echoSettings prints the tape's Output/Set header lines before the run, so the
// full script is visible (the runner streams the action lines as they execute).
func echoSettings(cfg runner.Config, cmds []parser.Command) {
	if !cfg.Verbose {
		return
	}
	for _, c := range cmds {
		if c.Type == parser.CmdOutput || c.Type == parser.CmdSet {
			fmt.Println(runner.Colorize(c, cfg.Color))
		}
	}
}

// colorEnabled reports whether stdout is a terminal that should receive ANSI
// color. It honors the NO_COLOR convention and disables color when output is
// piped or redirected.
func colorEnabled() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// cmdRecordSession drives a real browser and writes a tape from the user's
// interactions: vhsweb record <url> [-o out.tape].
func cmdRecordSession(args []string) error {
	var url, out string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "-o", "--output":
			if i+1 >= len(args) {
				return fmt.Errorf("%s requires a filename", a)
			}
			i++
			out = args[i]
		default:
			if a != "-" && strings.HasPrefix(a, "-") {
				return fmt.Errorf("unknown flag %q", a)
			}
			if url != "" {
				return fmt.Errorf("unexpected argument %q", a)
			}
			url = a
		}
	}
	if url == "" {
		return fmt.Errorf("record requires a URL: vhsweb record https://example.com")
	}
	return runner.Record(url, out, 1280, 720)
}

// cmdValidate parses and config-checks a tape without recording anything.
func cmdValidate(args []string) error {
	var path string
	for _, a := range args {
		if a != "-" && strings.HasPrefix(a, "-") {
			return fmt.Errorf("unknown flag %q", a)
		}
		if path != "" {
			return fmt.Errorf("unexpected argument %q", a)
		}
		path = a
	}
	r, name, baseDir, err := openTape(path)
	if err != nil {
		return err
	}
	defer r.Close()

	if _, _, _, err := loadTape(r, name, baseDir); err != nil {
		return err
	}
	fmt.Printf("%s: ok\n", name)
	return nil
}

// loadTape parses a tape reader into a runner Config, the ordered actions, and
// the full parsed command list (for echoing the script).
func loadTape(r io.Reader, name, baseDir string) (runner.Config, []parser.Command, []parser.Command, error) {
	cmds, err := parser.ParseWithBase(r, baseDir)
	if err != nil {
		return runner.Config{}, nil, nil, fmt.Errorf("%s: %w", name, err)
	}
	cfg, actions, err := runner.BuildConfig(cmds)
	if err != nil {
		return runner.Config{}, nil, nil, fmt.Errorf("%s: %w", name, err)
	}
	return cfg, actions, cmds, nil
}

// logf prints a status line unless quiet is set.
func logf(quiet bool, format string, a ...any) {
	if !quiet {
		fmt.Printf(format, a...)
	}
}

func usage() {
	fmt.Print(`vhsweb — VHS for the browser. Script a web page, get a video.

Usage:
  vhsweb <file.tape>            record the session described by the tape file
  vhsweb record <url>           drive a browser by hand, write a tape (-o file)
  vhsweb validate <file.tape>   parse-check a tape without recording
  vhsweb new <file.tape>        write a starter tape file
  vhsweb install                download the Playwright Chromium browser
  vhsweb version                print the version
  vhsweb help                   show this message

Flags (record):
  -o, --output <file>   write to <file>, overriding the tape's Output
                        (repeatable: -o demo.mp4 -o demo.gif)
  -p, --preview         watch the run in a real window, record nothing
  -q, --quiet           suppress status logging

A tape may also be piped in:  vhsweb < demo.tape
`)
}
