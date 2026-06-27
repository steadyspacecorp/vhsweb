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
	Output      string        // output file path (.mp4, .gif, or .webm)
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
	}
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
	default:
		return fmt.Errorf("unknown Set key %q", key)
	}
	return nil
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
