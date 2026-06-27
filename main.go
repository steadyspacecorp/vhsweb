// Command vhsweb records a web page session described by a .tape script to video.
//
// Usage:
//
//	vhsweb example.tape            record the session described by example.tape
//	vhsweb --preview example.tape  watch the run in a real window, record nothing
//	vhsweb new example.tape        write a starter .tape file
//	vhsweb install                 download the Playwright browser binaries
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/steadyspacecorp/vhs-browser/internal/parser"
	"github.com/steadyspacecorp/vhs-browser/internal/runner"
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
	case "-h", "--help", "help":
		usage()
		return nil
	case "-v", "--version", "version":
		fmt.Printf("vhsweb %s\n", version)
		return nil
	}

	// Otherwise: record (or, with --preview, just watch) a tape file.
	preview := false
	var path string
	for _, a := range args {
		switch a {
		case "--preview", "-p":
			preview = true
		default:
			if strings.HasPrefix(a, "-") {
				return fmt.Errorf("unknown flag %q", a)
			}
			if path != "" {
				return fmt.Errorf("unexpected argument %q", a)
			}
			path = a
		}
	}
	if path == "" {
		return fmt.Errorf("no tape file given (try: vhsweb help)")
	}
	return cmdRecord(path, preview)
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

func cmdRecord(path string, preview bool) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	cmds, err := parser.Parse(f)
	if err != nil {
		return err
	}

	cfg, actions, err := runner.BuildConfig(cmds)
	if err != nil {
		return err
	}

	if preview {
		// Watch the run in a real window; record and encode nothing.
		cfg.Preview = true
		cfg.Headless = false
		cfg.Sound = false
		fmt.Printf("Previewing %s (%dx%d, no recording)\n", path, cfg.Width, cfg.Height)
		return runner.Run(cfg, actions)
	}

	fmt.Printf("Recording %s -> %s (%dx%d)\n", path, cfg.Output, cfg.Width, cfg.Height)
	if err := runner.Run(cfg, actions); err != nil {
		return err
	}
	fmt.Printf("Wrote %s\n", cfg.Output)
	return nil
}

func usage() {
	fmt.Print(`vhsweb — VHS for the browser. Script a web page, get a video.

Usage:
  vhsweb <file.tape>            record the session described by the tape file
  vhsweb --preview <file.tape>  watch the run in a real window, record nothing
  vhsweb new <file.tape>        write a starter tape file
  vhsweb install                download the Playwright Chromium browser
  vhsweb version                print the version
  vhsweb help                   show this message
`)
}
