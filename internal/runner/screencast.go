package runner

import (
	"encoding/base64"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/playwright-community/playwright-go"
)

// scFrame is one captured screencast frame on disk and its millisecond offset
// from the start of capture.
type scFrame struct {
	path string
	atMs int
}

// screencaster streams lossless-ish JPEG frames from a page via the CDP
// Page.startScreencast API, bypassing Playwright's lossy RecordVideo. Frames are
// captured at the page's device-pixel resolution (so deviceScaleFactor yields a
// true retina capture) and written to disk as they arrive.
type screencaster struct {
	cdp   playwright.CDPSession
	dir   string
	start time.Time
	ext   string

	mu     sync.Mutex
	frames []scFrame
	n      int
	err    error
}

// startScreencast attaches a CDP session to page and begins streaming frames in
// the given format ("png" lossless, or "jpeg" quality 100) into dir. start
// anchors frame timestamps.
func startScreencast(ctx playwright.BrowserContext, page playwright.Page, dir string, start time.Time, maxW, maxH int, format string) (*screencaster, error) {
	if format != "jpeg" {
		format = "png"
	}
	ext := "png"
	if format == "jpeg" {
		ext = "jpg"
	}
	cdp, err := ctx.NewCDPSession(page)
	if err != nil {
		return nil, err
	}
	s := &screencaster{cdp: cdp, dir: dir, start: start, ext: ext}

	cdp.On("Page.screencastFrame", func(ev any) {
		params, ok := ev.(map[string]any)
		if !ok {
			return
		}
		// Ack immediately so the browser keeps sending frames.
		if sid, ok := params["sessionId"]; ok {
			_, _ = cdp.Send("Page.screencastFrameAck", map[string]any{"sessionId": sid})
		}
		data, _ := params["data"].(string)
		if data == "" {
			return
		}
		raw, derr := base64.StdEncoding.DecodeString(data)
		if derr != nil {
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		p := filepath.Join(s.dir, fmt.Sprintf("f-%06d.%s", s.n, s.ext))
		if werr := os.WriteFile(p, raw, 0o644); werr != nil {
			s.err = werr
			return
		}
		s.frames = append(s.frames, scFrame{path: p, atMs: int(time.Since(s.start).Milliseconds())})
		s.n++
	})

	params := map[string]any{
		"format":        format,
		"everyNthFrame": 1,
		"maxWidth":      maxW,
		"maxHeight":     maxH,
	}
	if format == "jpeg" {
		params["quality"] = 100
	}
	if _, err := cdp.Send("Page.startScreencast", params); err != nil {
		return nil, err
	}
	return s, nil
}

// stop ends the screencast and returns the captured frames plus the total
// capture duration (so the final frame can be held for any trailing idle time).
func (s *screencaster) stop() ([]scFrame, int, error) {
	_, _ = s.cdp.Send("Page.stopScreencast", map[string]any{})
	time.Sleep(150 * time.Millisecond) // let in-flight frames land
	_ = s.cdp.Detach()
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.frames, int(time.Since(s.start).Milliseconds()), s.err
}

// trimLeadingBlank drops leading near-uniform frames — the about:blank page
// captured before the first navigation paints — and rebases the remaining
// frames so the first kept one starts at zero. It returns the trimmed frames
// and the milliseconds removed, so the caller can shift the sound/cut timeline
// by the same amount. At least one frame is always kept.
func trimLeadingBlank(frames []scFrame) ([]scFrame, int) {
	start := 0
	for start < len(frames)-1 && isBlankFrame(frames[start].path) {
		start++
	}
	if start == 0 {
		return frames, 0
	}
	trimMs := frames[start].atMs
	out := make([]scFrame, 0, len(frames)-start)
	for _, f := range frames[start:] {
		out = append(out, scFrame{path: f.path, atMs: f.atMs - trimMs})
	}
	return out, trimMs
}

// isBlankFrame reports whether the image at path is near-uniform (a blank/solid
// loading frame), by sampling a grid and checking the luma spread.
func isBlankFrame(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return false
	}
	b := img.Bounds()
	if b.Dx() < 2 || b.Dy() < 2 {
		return false
	}
	const n = 16
	var lo, hi uint32 = 0xffff, 0
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			x := b.Min.X + (b.Dx()-1)*i/(n-1)
			y := b.Min.Y + (b.Dy()-1)*j/(n-1)
			r, g, bl, _ := img.At(x, y).RGBA()
			l := (r*299 + g*587 + bl*114) / 1000
			if l < lo {
				lo = l
			}
			if l > hi {
				hi = l
			}
		}
	}
	return hi-lo < 1500 // < ~2% of the 16-bit luma range
}

// assembleFrames concatenates captured frames into a lossless intermediate
// video, timing each frame by its real arrival so playback matches wall-clock.
// totalMs holds the last frame for any trailing idle period.
func assembleFrames(frames []scFrame, totalMs int, dir string) (string, error) {
	if len(frames) == 0 {
		return "", fmt.Errorf("no frames captured")
	}
	var b strings.Builder
	for i, f := range frames {
		fmt.Fprintf(&b, "file '%s'\n", f.path)
		next := totalMs
		if i+1 < len(frames) {
			next = frames[i+1].atMs
		}
		dur := next - f.atMs
		if dur < 1 {
			dur = 1
		}
		fmt.Fprintf(&b, "duration %.3f\n", float64(dur)/1000)
	}
	// The concat demuxer ignores the final entry's duration unless the file is
	// listed once more.
	fmt.Fprintf(&b, "file '%s'\n", frames[len(frames)-1].path)

	list := filepath.Join(dir, "frames.txt")
	if err := os.WriteFile(list, []byte(b.String()), 0o644); err != nil {
		return "", err
	}
	out := filepath.Join(dir, "screencast.mkv")
	args := []string{
		"-y", "-f", "concat", "-safe", "0", "-i", list,
		"-fps_mode", "vfr",
		"-c:v", "libx264", "-qp", "0", "-pix_fmt", "yuv444p",
		out,
	}
	cmd := exec.Command("ffmpeg", args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("assembling frames: %w\n%s", err, stderr.String())
	}
	return out, nil
}
