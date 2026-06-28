// Package runner executes parsed .tape commands against a Playwright-driven
// browser and records the session to a video file.
package runner

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/steadyspacecorp/vhsweb/internal/encoder"
	"github.com/steadyspacecorp/vhsweb/internal/parser"
	"github.com/playwright-community/playwright-go"
)

// Run executes the full pipeline: launch browser, play commands, record, and
// encode the result to cfg.Output.
func Run(cfg Config, actions []parser.Command) error {
	// Preview mode opens a real window and replays the actions, but records
	// nothing — so it skips the video temp dir and the encode step below.
	var videoDir string
	if !cfg.Preview {
		var err error
		videoDir, err = os.MkdirTemp("", "vhsweb-video-*")
		if err != nil {
			return fmt.Errorf("creating temp dir: %w", err)
		}
		defer os.RemoveAll(videoDir)
	}

	pw, err := playwright.Run()
	if err != nil {
		return fmt.Errorf("starting playwright: %w", err)
	}
	defer pw.Stop()

	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(cfg.Headless),
	})
	if err != nil {
		return fmt.Errorf("launching chromium: %w", err)
	}

	// Zoom maps to deviceScaleFactor (true DPR): the viewport stays the logical
	// page size (so CSS media queries see the real width), the browser renders
	// at Width*Zoom device pixels, and RecordVideo downsamples that to the
	// logical size — supersampled, anti-aliased text with undistorted layout.
	// RecordVideo can't exceed the CSS viewport (a larger Size just gray-pads),
	// so output resolution is the logical Width x Height regardless of Zoom.
	viewport := &playwright.Size{Width: cfg.Width, Height: cfg.Height}
	devW := int(float64(cfg.Width) * cfg.Zoom)
	devH := int(float64(cfg.Height) * cfg.Zoom)
	ctxOpts := playwright.BrowserNewContextOptions{Viewport: viewport}
	if cfg.Zoom != 1 {
		ctxOpts.DeviceScaleFactor = playwright.Float(cfg.Zoom)
	}
	switch cfg.ColorScheme {
	case "dark":
		ctxOpts.ColorScheme = playwright.ColorSchemeDark
	case "light":
		ctxOpts.ColorScheme = playwright.ColorSchemeLight
	}
	// The screencast path (default) captures lossless frames at device-pixel
	// resolution via CDP. RecordVideo is the fallback and caps at the CSS
	// viewport, so it records at the logical size.
	if !cfg.Preview && cfg.Capture == "record" {
		ctxOpts.RecordVideo = &playwright.RecordVideo{
			Dir:  playwright.String(videoDir),
			Size: viewport,
		}
	}
	browserCtx, err := browser.NewContext(ctxOpts)
	if err != nil {
		return fmt.Errorf("creating context: %w", err)
	}

	if cfg.ShowCursor {
		if err := browserCtx.AddInitScript(playwright.Script{
			Content: playwright.String(cursorScript),
		}); err != nil {
			return fmt.Errorf("installing cursor: %w", err)
		}
	}

	page, err := browserCtx.NewPage()
	if err != nil {
		return fmt.Errorf("opening page: %w", err)
	}
	page.SetDefaultTimeout(float64(cfg.WaitTimeout.Milliseconds()))

	// The video starts when the page is created; events are timestamped
	// relative to this moment so sounds line up with the recording.
	start := time.Now()
	var events []encoder.SoundEvent
	var cuts []encoder.Cut
	hideAt := -1
	mouse := &mouseState{}

	var sc *screencaster
	if !cfg.Preview && cfg.Capture != "record" {
		sc, err = startScreencast(browserCtx, page, videoDir, start, devW, devH, cfg.CaptureFormat)
		if err != nil {
			_ = browserCtx.Close()
			return fmt.Errorf("starting screencast: %w", err)
		}
	}

	for _, cmd := range actions {
		if cfg.Verbose {
			fmt.Println(cmd.String())
		}
		switch cmd.Type {
		case parser.CmdHide:
			if hideAt < 0 {
				hideAt = elapsedMs(start)
			}
			continue
		case parser.CmdShow:
			if hideAt >= 0 {
				cuts = append(cuts, encoder.Cut{StartMs: hideAt, EndMs: elapsedMs(start)})
				hideAt = -1
			}
			continue
		}
		if err := execute(page, cfg, cmd, start, &events, mouse); err != nil {
			// Tear down the recording before reporting the failure.
			_ = browserCtx.Close()
			return fmt.Errorf("line %d: %s: %w", cmd.Line, cmd.Type, err)
		}
	}
	// A Hide with no matching Show drops everything to the end of capture.
	if hideAt >= 0 {
		cuts = append(cuts, encoder.Cut{StartMs: hideAt, EndMs: elapsedMs(start)})
	}

	if cfg.Preview {
		// Nothing was recorded; just tear down the browser.
		return browserCtx.Close()
	}

	var rawPath string
	if sc != nil {
		frames, totalMs, serr := sc.stop()
		if serr != nil {
			_ = browserCtx.Close()
			return fmt.Errorf("screencast capture: %w", serr)
		}
		if err := browserCtx.Close(); err != nil {
			return fmt.Errorf("closing context: %w", err)
		}
		// Drop the blank about:blank frames before the first paint, shifting the
		// sound and cut timeline to match so nothing desyncs.
		frames, trim := trimLeadingBlank(frames)
		totalMs -= trim
		events = shiftEvents(events, trim)
		cuts = shiftCuts(cuts, trim)
		rawPath, err = assembleFrames(frames, totalMs, videoDir)
		if err != nil {
			return fmt.Errorf("assembling screencast: %w", err)
		}
	} else {
		video := page.Video()
		if err := browserCtx.Close(); err != nil {
			return fmt.Errorf("closing context: %w", err)
		}
		rawPath, err = video.Path()
		if err != nil {
			return fmt.Errorf("locating recorded video: %w", err)
		}
	}

	if !cfg.Sound {
		events = nil
	}
	opts := encoder.Options{
		Framerate:  cfg.Framerate,
		Events:     events,
		Speed:      cfg.PlaybackSpeed,
		LoopOffset: cfg.LoopOffset,
		Cuts:       cuts,
		Frame: encoder.Frame{
			Padding:      cfg.Padding,
			Margin:       cfg.Margin,
			MarginFill:   cfg.MarginFill,
			WindowBar:    cfg.WindowBar,
			BorderRadius: cfg.BorderRadius,
		},
	}
	for _, dst := range cfg.outputs() {
		if err := encoder.Encode(rawPath, dst, opts); err != nil {
			return fmt.Errorf("encoding %s: %w", dst, err)
		}
	}
	return nil
}

// loadState maps a WaitFor argument to a Playwright load state, or nil if the
// argument is an ordinary selector.
func loadState(arg string) *playwright.LoadState {
	switch strings.ToLower(arg) {
	case "load":
		return playwright.LoadStateLoad
	case "domcontentloaded":
		return playwright.LoadStateDomcontentloaded
	case "networkidle":
		return playwright.LoadStateNetworkidle
	default:
		return nil
	}
}

// shiftEvents moves sound events earlier by ms, dropping any that fall before
// the new start.
func shiftEvents(events []encoder.SoundEvent, ms int) []encoder.SoundEvent {
	if ms <= 0 {
		return events
	}
	out := events[:0]
	for _, e := range events {
		if e.AtMs -= ms; e.AtMs >= 0 {
			out = append(out, e)
		}
	}
	return out
}

// shiftCuts moves Hide/Show cut spans earlier by ms, clamping to zero and
// dropping any that end before the new start.
func shiftCuts(cuts []encoder.Cut, ms int) []encoder.Cut {
	if ms <= 0 {
		return cuts
	}
	out := cuts[:0]
	for _, c := range cuts {
		c.StartMs -= ms
		c.EndMs -= ms
		if c.StartMs < 0 {
			c.StartMs = 0
		}
		if c.EndMs > 0 {
			out = append(out, c)
		}
	}
	return out
}

// elapsedMs returns milliseconds since the recording started.
func elapsedMs(start time.Time) int {
	return int(time.Since(start).Milliseconds())
}

// execute dispatches a single action command, appending sound events to evs as
// clicks and keystrokes occur.
func execute(page playwright.Page, cfg Config, cmd parser.Command, start time.Time, evs *[]encoder.SoundEvent, mouse *mouseState) error {
	switch cmd.Type {
	case parser.CmdGoto:
		_, err := page.Goto(cmd.Args[0], playwright.PageGotoOptions{
			WaitUntil: playwright.WaitUntilStateLoad,
		})
		return err

	case parser.CmdType:
		// One key tick per character, spaced by the typing delay.
		base := elapsedMs(start)
		for i := range []rune(cmd.Args[0]) {
			*evs = append(*evs, encoder.SoundEvent{
				AtMs: base + i*int(cfg.TypingSpeed.Milliseconds()),
				Kind: encoder.SoundKey,
			})
		}
		return page.Keyboard().Type(cmd.Args[0], playwright.KeyboardTypeOptions{
			Delay: playwright.Float(float64(cfg.TypingSpeed.Milliseconds())),
		})

	case parser.CmdClick:
		if err := moveMouseToSelector(page, mouse, cmd.Args[0]); err != nil {
			return err
		}
		err := page.Click(cmd.Args[0])
		if err == nil {
			*evs = append(*evs, encoder.SoundEvent{AtMs: elapsedMs(start), Kind: encoder.SoundClick})
		}
		return err

	case parser.CmdFill:
		return page.Fill(cmd.Args[0], cmd.Args[1])

	case parser.CmdPress:
		*evs = append(*evs, encoder.SoundEvent{AtMs: elapsedMs(start), Kind: encoder.SoundKey})
		return page.Keyboard().Press(cmd.Args[0])

	case parser.CmdHover:
		if err := moveMouseToSelector(page, mouse, cmd.Args[0]); err != nil {
			return err
		}
		return page.Hover(cmd.Args[0])

	case parser.CmdWaitFor:
		// `WaitFor load|domcontentloaded|networkidle` waits on a navigation
		// load state; anything else waits for a selector to appear.
		if state := loadState(cmd.Args[0]); state != nil {
			return page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{State: state})
		}
		_, err := page.WaitForSelector(cmd.Args[0])
		return err

	case parser.CmdScroll:
		return scroll(page, mouse, cmd.Args)

	case parser.CmdScreenshot:
		_, err := page.Screenshot(playwright.PageScreenshotOptions{
			Path: playwright.String(cmd.Args[0]),
		})
		return err

	case parser.CmdSleep:
		d, err := parseDuration(cmd.Args[0])
		if err != nil {
			return err
		}
		time.Sleep(d)
		return nil

	default:
		return fmt.Errorf("unsupported command %q", cmd.Type)
	}
}

// scroll handles `Scroll <Up|Down|Left|Right> <pixels>`, animating the wheel in
// small steps so the motion reads smoothly in the recording.
func scroll(page playwright.Page, mouse *mouseState, args []string) error {
	direction := strings.ToLower(args[0])

	// Args after the direction: an optional pixel count and an optional
	// selector (Scroll Down 600 "#panel"). Either may be omitted.
	pixels := 400
	selector := ""
	for _, a := range args[1:] {
		if n, err := strconv.Atoi(a); err == nil {
			pixels = n
		} else {
			selector = a
		}
	}

	// Position the cursor over the target so the wheel scrolls that element
	// (its nearest scrollable ancestor) rather than the page.
	if selector != "" {
		if err := moveMouseToSelector(page, mouse, selector); err != nil {
			return err
		}
	}

	var dx, dy float64
	switch direction {
	case "down":
		dy = 1
	case "up":
		dy = -1
	case "right":
		dx = 1
	case "left":
		dx = -1
	default:
		return fmt.Errorf("unknown scroll direction %q", args[0])
	}

	const step = 60
	remaining := pixels
	for remaining > 0 {
		delta := step
		if remaining < step {
			delta = remaining
		}
		if err := page.Mouse().Wheel(dx*float64(delta), dy*float64(delta)); err != nil {
			return err
		}
		remaining -= delta
		time.Sleep(16 * time.Millisecond)
	}
	return nil
}
