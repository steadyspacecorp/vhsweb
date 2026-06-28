package runner

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/steadyspacecorp/vhsweb/internal/parser"
)

// Config holds recording settings derived from Output and Set commands.
type Config struct {
	Output      string        // output file path from the tape's Output command
	Outputs     []string      // CLI -o overrides; when non-empty, replaces Output
	Width       int           // logical viewport width
	Height      int           // logical viewport height
	Zoom        float64       // device pixel ratio; video is captured at Width*Zoom x Height*Zoom
	Framerate   int           // target output framerate
	TypingSpeed time.Duration // delay between keystrokes for Type
	WaitTimeout time.Duration // default timeout for navigation / WaitFor
	Headless    bool          // run the browser without a visible window
	ShowCursor  bool          // overlay a fake mouse cursor in the video
	Sound       bool          // mix click/keystroke sound effects into the audio track
	Preview     bool          // watch the run in a real window; skip recording/encoding
	Verbose     bool          // echo each action line to stdout as it runs

	PlaybackSpeed float64 // output playback speed multiplier (1 = realtime)
	LoopOffset    float64 // GIF only: fraction 0..1 to rotate the loop start by

	Capture       string // "screencast" (lossless CDP frames, default) or "record" (Playwright RecordVideo)
	CaptureFormat string // screencast frame format: "jpeg" (q100, default) or "png" (lossless)

	Padding      int    // inner mat between page content and the window edge, px
	Margin       int    // space around the window, px
	MarginFill   string // color filling the margin / padding / rounded corners
	WindowBar    string // title-bar style ("" = none; e.g. Colorful, Rings)
	BorderRadius int    // window corner radius, px

	ColorScheme string // emulated prefers-color-scheme: "dark" / "light" / "" = system default
}

// DefaultConfig returns the baseline settings before any Set commands apply.
func DefaultConfig() Config {
	return Config{
		Output:      "out.mp4",
		Width:       1280,
		Height:      720,
		Zoom:        1,
		Framerate:   30,
		TypingSpeed: 75 * time.Millisecond,
		WaitTimeout: 15 * time.Second,
		Headless:    true,
		ShowCursor:  true,
		Sound:       true,

		PlaybackSpeed: 1,
		MarginFill:    "#FFFFFF",
		Capture:       "screencast",
		CaptureFormat: "jpeg",
	}
}

// outputs returns the destinations to encode: the CLI -o overrides if any were
// given, otherwise the single Output from the tape.
func (c Config) outputs() []string {
	if len(c.Outputs) > 0 {
		return c.Outputs
	}
	return []string{c.Output}
}

// applySet mutates the config from a `Set <key> <value>` command.
func (c *Config) applySet(key, value string) error {
	switch strings.ToLower(key) {
	case "width":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("Set Width: %w", err)
		}
		c.Width = n
	case "height":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("Set Height: %w", err)
		}
		c.Height = n
	case "zoom", "scale", "devicescalefactor":
		f, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("Set Zoom: %w", err)
		}
		if f <= 0 {
			return fmt.Errorf("Set Zoom: must be > 0")
		}
		c.Zoom = f
	case "framerate", "fps":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("Set Framerate: %w", err)
		}
		c.Framerate = n
	case "typingspeed":
		d, err := parseDuration(value)
		if err != nil {
			return fmt.Errorf("Set TypingSpeed: %w", err)
		}
		c.TypingSpeed = d
	case "waittimeout", "timeout":
		d, err := parseDuration(value)
		if err != nil {
			return fmt.Errorf("Set WaitTimeout: %w", err)
		}
		c.WaitTimeout = d
	case "headless":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("Set Headless: %w", err)
		}
		c.Headless = b
	case "cursor", "showcursor":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("Set Cursor: %w", err)
		}
		c.ShowCursor = b
	case "sound", "sfx":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("Set Sound: %w", err)
		}
		c.Sound = b
	case "playbackspeed", "speed":
		f, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("Set PlaybackSpeed: %w", err)
		}
		if f <= 0 {
			return fmt.Errorf("Set PlaybackSpeed: must be > 0")
		}
		c.PlaybackSpeed = f
	case "loopoffset":
		f, err := parseFraction(value)
		if err != nil {
			return fmt.Errorf("Set LoopOffset: %w", err)
		}
		c.LoopOffset = f
	case "padding":
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 {
			return fmt.Errorf("Set Padding: want a non-negative integer, got %q", value)
		}
		c.Padding = n
	case "margin":
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 {
			return fmt.Errorf("Set Margin: want a non-negative integer, got %q", value)
		}
		c.Margin = n
	case "marginfill":
		c.MarginFill = value
	case "windowbar":
		if !validWindowBar(value) {
			return fmt.Errorf("Set WindowBar: unknown style %q (try Colorful, ColorfulRight, Rings, RingsRight)", value)
		}
		c.WindowBar = value
	case "borderradius", "radius":
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 {
			return fmt.Errorf("Set BorderRadius: want a non-negative integer, got %q", value)
		}
		c.BorderRadius = n
	case "capture":
		switch strings.ToLower(value) {
		case "screencast", "record":
			c.Capture = strings.ToLower(value)
		default:
			return fmt.Errorf("Set Capture: want screencast or record, got %q", value)
		}
	case "captureformat":
		switch strings.ToLower(value) {
		case "png":
			c.CaptureFormat = "png"
		case "jpeg", "jpg":
			c.CaptureFormat = "jpeg"
		default:
			return fmt.Errorf("Set CaptureFormat: want png or jpeg, got %q", value)
		}
	case "theme", "colorscheme", "darkmode":
		switch strings.ToLower(value) {
		case "dark", "light":
			c.ColorScheme = strings.ToLower(value)
		case "system", "auto", "":
			c.ColorScheme = ""
		default:
			return fmt.Errorf("Set Theme: want dark, light, or system, got %q", value)
		}
	default:
		return fmt.Errorf("unknown Set key %q", key)
	}
	return nil
}

// validWindowBar reports whether s names a supported window-bar style.
func validWindowBar(s string) bool {
	switch s {
	case "", "Colorful", "ColorfulRight", "Rings", "RingsRight":
		return true
	}
	return false
}

// parseFraction accepts a percentage ("20%") or a 0..1 float and returns the
// fraction, clamped to [0,1).
func parseFraction(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "%") {
		p, err := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 64)
		if err != nil {
			return 0, err
		}
		return clampFraction(p / 100), nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	return clampFraction(f), nil
}

func clampFraction(f float64) float64 {
	if f < 0 {
		return 0
	}
	if f >= 1 {
		return 0.999
	}
	return f
}

// BuildConfig extracts Output/Set commands into a Config and returns the
// remaining action commands to execute, in order.
func BuildConfig(cmds []parser.Command) (Config, []parser.Command, error) {
	cfg := DefaultConfig()
	var actions []parser.Command

	for _, cmd := range cmds {
		switch cmd.Type {
		case parser.CmdOutput:
			cfg.Output = cmd.Args[0]
		case parser.CmdSet:
			if err := cfg.applySet(cmd.Args[0], cmd.Args[1]); err != nil {
				return cfg, nil, fmt.Errorf("line %d: %w", cmd.Line, err)
			}
		default:
			actions = append(actions, cmd)
		}
	}
	return cfg, actions, nil
}

// parseDuration accepts Go-style durations ("500ms", "2s") and bare integers
// (interpreted as milliseconds), matching VHS's lenient time handling.
func parseDuration(s string) (time.Duration, error) {
	if n, err := strconv.Atoi(s); err == nil {
		return time.Duration(n) * time.Millisecond, nil
	}
	return time.ParseDuration(s)
}
