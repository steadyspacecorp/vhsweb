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

	// RecordVideo writes frames at the viewport's pixel size, so to get a crisp
	// Zoom-times-larger video we size the viewport up to the target resolution
	// and magnify the page content by the same factor (see zoomScript). The
	// browser rasterizes text/vectors at the final pixel size, so the result is
	// genuinely sharp rather than upscaled.
	size := &playwright.Size{
		Width:  int(float64(cfg.Width) * cfg.Zoom),
		Height: int(float64(cfg.Height) * cfg.Zoom),
	}
	ctxOpts := playwright.BrowserNewContextOptions{Viewport: size}
	switch cfg.ColorScheme {
	case "dark":
		ctxOpts.ColorScheme = playwright.ColorSchemeDark
	case "light":
		ctxOpts.ColorScheme = playwright.ColorSchemeLight
	}
	if !cfg.Preview {
		ctxOpts.RecordVideo = &playwright.RecordVideo{
			Dir:  playwright.String(videoDir),
			Size: size,
		}
	}
	browserCtx, err := browser.NewContext(ctxOpts)
	if err != nil {
		return fmt.Errorf("creating context: %w", err)
	}

	if cfg.Zoom != 1 {
		if err := browserCtx.AddInitScript(playwright.Script{
			Content: playwright.String(zoomScript(cfg.Zoom)),
		}); err != nil {
			return fmt.Errorf("installing zoom: %w", err)
		}
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

	for _, cmd := range actions {
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

	video := page.Video()
	if err := browserCtx.Close(); err != nil {
		return fmt.Errorf("closing context: %w", err)
	}

	rawPath, err := video.Path()
	if err != nil {
		return fmt.Errorf("locating recorded video: %w", err)
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
		_, err := page.WaitForSelector(cmd.Args[0])
		return err

	case parser.CmdScroll:
		return scroll(page, cmd.Args)

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
func scroll(page playwright.Page, args []string) error {
	direction := strings.ToLower(args[0])
	pixels := 400
	if len(args) > 1 {
		n, err := strconv.Atoi(args[1])
		if err != nil {
			return fmt.Errorf("scroll amount: %w", err)
		}
		pixels = n
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
