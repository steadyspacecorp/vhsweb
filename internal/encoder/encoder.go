// Package encoder transcodes the raw Playwright WebM recording into the
// user-requested output format using ffmpeg, optionally mixing in click and
// keystroke sound effects at recorded timestamps and dressing the frame with a
// window bar, rounded corners, and a margin.
package encoder

import (
	"embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// soundFS holds the bundled click/keystroke samples (see assets/CREDITS.md).
//
//go:embed assets/*.mp3
var soundFS embed.FS

// soundVariants is how many numbered samples exist per kind (key-1..4, click-1..4).
const soundVariants = 4

// Sound identifies which bundled sample to play for an event.
type Sound string

const (
	SoundKey   Sound = "key"
	SoundClick Sound = "click"
)

// SoundEvent marks a point in the recording where a tick should be heard.
type SoundEvent struct {
	AtMs int   // milliseconds from the start of the recording
	Kind Sound // key or click
}

// Cut is a span of the raw recording to remove from the output, in
// milliseconds from the start of capture (produced by Hide/Show).
type Cut struct {
	StartMs, EndMs int
}

// Frame describes optional window dressing applied to the video.
type Frame struct {
	Padding      int    // inner mat between content and window edge, px
	Margin       int    // space around the window, px
	MarginFill   string // color of margin / padding / rounded-corner reveal
	WindowBar    string // "" = none; Colorful, ColorfulRight, Rings, RingsRight
	BorderRadius int    // window corner radius, px
}

func (f Frame) active() bool {
	return f.Padding > 0 || f.Margin > 0 || f.WindowBar != "" || f.BorderRadius > 0
}

// Options collects everything that shapes the encode beyond the source/dest.
type Options struct {
	Framerate  int
	Events     []SoundEvent
	Speed      float64 // playback multiplier (1 = realtime); <=0 treated as 1
	LoopOffset float64 // GIF only: fraction 0..1 to rotate the loop start by
	Cuts       []Cut   // spans to drop (Hide/Show)
	Frame      Frame
}

func (o Options) speed() float64 {
	if o.Speed <= 0 {
		return 1
	}
	return o.Speed
}

// Encode converts the WebM recording at src into dst, choosing settings from
// dst's extension (.mp4, .gif, or .webm). When events are present, a click /
// keystroke audio track is mixed into mp4 and webm output (gif is always
// silent).
func Encode(src, dst string, opts Options) error {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return fmt.Errorf("ffmpeg not found on PATH: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(dst))
	if ext == "" {
		ext = ".mp4"
		dst += ".mp4"
	}
	fps := strconv.Itoa(opts.Framerate)

	h, err := probeHeight(src)
	if err != nil {
		return fmt.Errorf("probing video: %w", err)
	}
	chain := buildVideoChain(opts, h)

	switch ext {
	case ".mp4", ".webm":
		events := timeline(opts)
		if len(events) > 0 {
			return encodeWithSound(src, dst, ext, fps, chain, opts, events)
		}
		return runFFmpeg(silentArgs(src, dst, ext, fps, chain))
	case ".gif":
		return encodeGIF(src, dst, fps, chain, opts)
	default:
		return fmt.Errorf("unsupported output extension %q", ext)
	}
}

// buildVideoChain assembles the linear ffmpeg filter chain (comma-joined, no
// pad labels) applied to the raw video: drop Hide/Show cuts, apply playback
// speed, dress the frame, and finish on even dimensions. The caller appends the
// output pixel format. h is the source height, used to size the window bar.
func buildVideoChain(opts Options, h int) string {
	var fs []string

	if expr := cutSelect(opts.Cuts); expr != "" {
		fs = append(fs, "select='"+expr+"'", "setpts=N/FRAME_RATE/TB")
	}
	if s := opts.speed(); s != 1 {
		fs = append(fs, fmt.Sprintf("setpts=PTS/%s", trimFloat(s)))
	}

	if opts.Frame.active() {
		fs = append(fs, frameFilters(opts.Frame, h)...)
	}

	// Even dimensions last, so libx264 accepts the (possibly padded) frame.
	fs = append(fs, "scale=trunc(iw/2)*2:trunc(ih/2)*2")
	return strings.Join(fs, ",")
}

// frameFilters returns the window-dressing filters: an rgb working format, an
// optional window bar, rounded corners, then the padding and margin mats — all
// filled with the frame's MarginFill color.
//
// Order matters: the corners are rounded on the bar+content "window" *before*
// the mats are added, so the rounding cuts into real page pixels and reveals
// the fill. Rounding after the (fill-colored) padding would be invisible.
func frameFilters(f Frame, h int) []string {
	fill, r, g, b := parseColor(f.MarginFill)
	var fs []string
	fs = append(fs, "format=rgb24")

	if f.WindowBar != "" {
		fs = append(fs, windowBarFilters(f.WindowBar, h)...)
	}
	if f.BorderRadius > 0 {
		fs = append(fs, roundCorners(f.BorderRadius, r, g, b))
	}
	// Padding and margin both mat the rounded window in MarginFill.
	if mat := f.Padding + f.Margin; mat > 0 {
		fs = append(fs, fmt.Sprintf("pad=iw+%d:ih+%d:%d:%d:color=%s",
			2*mat, 2*mat, mat, mat, fill))
	}
	return fs
}

// windowBarFilters draws a title bar above the frame with three traffic-light
// dots. ColorfulRight / RingsRight place the dots on the right.
func windowBarFilters(style string, h int) []string {
	barH := h * 5 / 100
	if barH < 28 {
		barH = 28
	}
	dot := barH * 28 / 100
	if dot < 6 {
		dot = 6
	}
	dotY := (barH - dot) / 2
	step := dot * 2
	pad := barH

	fs := []string{fmt.Sprintf("pad=iw:ih+%d:0:%d:color=0xE2E2E2", barH, barH)}

	colors := []string{"0xFF5F57", "0xFEBC2E", "0x28C840"}
	right := strings.HasSuffix(style, "Right")
	for i, c := range colors {
		var x string
		if right {
			// Rightmost dot (green) sits closest to the edge.
			off := pad + dot + (2-i)*step
			x = fmt.Sprintf("iw-%d", off)
		} else {
			x = strconv.Itoa(pad + i*step)
		}
		fs = append(fs, fmt.Sprintf("drawbox=x=%s:y=%d:w=%d:h=%d:color=%s@1:t=fill",
			x, dotY, dot, dot, c))
	}
	return fs
}

// roundCorners returns a geq filter that paints the four corner cutouts with
// the given fill color, producing rounded window corners. Expressions are
// single-quoted so their commas are protected from the filtergraph parser.
func roundCorners(radius, r, g, b int) string {
	R := strconv.Itoa(radius)
	mx := "min(X,W-1-X)"
	my := "min(Y,H-1-Y)"
	dx := "(" + R + "-" + mx + ")"
	dy := "(" + R + "-" + my + ")"
	cut := fmt.Sprintf("lt(%s,%s)*lt(%s,%s)*gt(%s*%s+%s*%s,%s*%s)",
		mx, R, my, R, dx, dx, dy, dy, R, R)
	ch := func(orig string, v int) string {
		return fmt.Sprintf("'if(%s,%d,%s)'", cut, v, orig)
	}
	return fmt.Sprintf("geq=r=%s:g=%s:b=%s", ch("r(X,Y)", r), ch("g(X,Y)", g), ch("b(X,Y)", b))
}

// cutSelect returns the select-filter expression keeping frames outside every
// cut, or "" when there are no cuts.
func cutSelect(cuts []Cut) string {
	var betweens []string
	for _, c := range cuts {
		if c.EndMs <= c.StartMs {
			continue
		}
		betweens = append(betweens, fmt.Sprintf("between(t,%s,%s)",
			trimFloat(float64(c.StartMs)/1000), trimFloat(float64(c.EndMs)/1000)))
	}
	if len(betweens) == 0 {
		return ""
	}
	return "not(" + strings.Join(betweens, "+") + ")"
}

// timeline maps recorded sound events onto the output timeline, dropping any
// inside a cut and compressing the rest by removed time and playback speed.
func timeline(opts Options) []SoundEvent {
	speed := opts.speed()
	var out []SoundEvent
	for _, e := range opts.Events {
		ms := e.AtMs
		dropped := false
		removedBefore := 0
		for _, c := range opts.Cuts {
			if c.EndMs <= c.StartMs {
				continue
			}
			if ms >= c.StartMs && ms <= c.EndMs {
				dropped = true
				break
			}
			if c.EndMs <= ms {
				removedBefore += c.EndMs - c.StartMs
			}
		}
		if dropped {
			continue
		}
		at := int(float64(ms-removedBefore) / speed)
		out = append(out, SoundEvent{AtMs: at, Kind: e.Kind})
	}
	return out
}

// silentArgs builds the ffmpeg arguments for an audio-free transcode.
func silentArgs(src, dst, ext, fps, chain string) []string {
	vf := chain + ",format=yuv420p"
	if ext == ".webm" {
		return []string{
			"-y", "-i", src, "-r", fps, "-vf", vf,
			"-c:v", "libvpx-vp9", "-b:v", "0", "-crf", "24",
			dst,
		}
	}
	return []string{
		"-y", "-i", src, "-r", fps, "-vf", vf,
		"-c:v", "libx264", "-preset", "slow", "-crf", "18",
		"-movflags", "+faststart",
		dst,
	}
}

// encodeWithSound transcodes the video and mixes the click/keystroke samples
// onto a generated audio track at their (cut/speed-adjusted) timestamps.
func encodeWithSound(src, dst, ext, fps, chain string, opts Options, events []SoundEvent) error {
	tmp, err := os.MkdirTemp("", "vhsweb-sfx-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	assetPath, err := materializeAssets(tmp)
	if err != nil {
		return fmt.Errorf("writing sound assets: %w", err)
	}

	durSec, err := probeDuration(src)
	if err != nil {
		return fmt.Errorf("probing duration: %w", err)
	}

	// Inputs: 0=video, 1=silent base, then one sample input per event.
	eventInputs, filter := buildSoundMix(chain, events, assetPath)
	args := []string{
		"-y",
		"-i", src,
		"-f", "lavfi", "-t", durSec, "-i", "anullsrc=r=44100:cl=mono",
	}
	args = append(args, eventInputs...)
	args = append(args,
		"-filter_complex", filter,
		"-map", "[vout]", "-map", "[aout]",
	)
	if ext == ".webm" {
		args = append(args,
			"-r", fps, "-c:v", "libvpx-vp9", "-b:v", "0", "-crf", "24",
			"-c:a", "libopus", "-b:a", "128k")
	} else {
		args = append(args,
			"-r", fps, "-c:v", "libx264", "-preset", "medium",
			"-movflags", "+faststart",
			"-c:a", "aac", "-b:a", "128k")
	}
	args = append(args, "-shortest", dst)

	return runFFmpeg(args)
}

// buildSoundMix returns the per-event ffmpeg sample inputs and the
// -filter_complex graph: run the shared video chain, then delay one sample per
// event and amix them onto the silent base track. Each kind cycles through its
// numbered variants (key-1..4, click-1..4) so repeats don't sound identical.
func buildSoundMix(chain string, events []SoundEvent, assetPath map[string]string) (inputs []string, filter string) {
	var b strings.Builder
	fmt.Fprintf(&b, "[0:v]%s,format=yuv420p[vout];", chain)

	mixLabels := []string{"[1:a]"} // silent base
	var keyN, clickN int

	for i, e := range events {
		var name string
		if e.Kind == SoundClick {
			name = fmt.Sprintf("click-%d.mp3", clickN%soundVariants+1)
			clickN++
		} else {
			name = fmt.Sprintf("key-%d.mp3", keyN%soundVariants+1)
			keyN++
		}
		inputs = append(inputs, "-i", assetPath[name])

		at := e.AtMs
		if at < 0 {
			at = 0
		}
		// Event samples start at input index 2 (after video and silent base).
		label := fmt.Sprintf("s%d", i)
		fmt.Fprintf(&b, "[%d:a]adelay=%d:all=1,volume=%.3f[%s];", i+2, at, tickVolume(i), label)
		mixLabels = append(mixLabels, "["+label+"]")
	}

	b.WriteString(strings.Join(mixLabels, ""))
	fmt.Fprintf(&b, "amix=inputs=%d:normalize=0:duration=longest[aout]", len(mixLabels))
	return inputs, b.String()
}

// tickVolume varies the level slightly per event so repeated samples don't sound
// mechanically identical. Deterministic so output is reproducible.
func tickVolume(i int) float64 {
	frac := float64((i*1103515245+12345)%1000) / 1000.0
	return 0.55 + 0.30*frac
}

// materializeAssets writes the embedded samples into dir (ffmpeg needs real
// file inputs) and returns a map of file name to on-disk path.
func materializeAssets(dir string) (map[string]string, error) {
	entries, err := soundFS.ReadDir("assets")
	if err != nil {
		return nil, err
	}
	paths := make(map[string]string, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".mp3") {
			continue
		}
		data, err := soundFS.ReadFile("assets/" + e.Name())
		if err != nil {
			return nil, err
		}
		p := filepath.Join(dir, e.Name())
		if err := os.WriteFile(p, data, 0o644); err != nil {
			return nil, err
		}
		paths[e.Name()] = p
	}
	return paths, nil
}

// probeDuration returns the duration of src in seconds as an ffmpeg-friendly
// string, used to size the silent base track.
func probeDuration(src string) (string, error) {
	out, err := exec.Command("ffprobe", "-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=nw=1:nk=1", src).Output()
	if err != nil {
		return "", err
	}
	d := strings.TrimSpace(string(out))
	if d == "" || d == "N/A" {
		return "60", nil // safe fallback; -shortest trims to the video
	}
	return d, nil
}

// probeHeight returns the pixel height of src's video stream.
func probeHeight(src string) (int, error) {
	out, err := exec.Command("ffprobe", "-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=height",
		"-of", "default=nw=1:nk=1", src).Output()
	if err != nil {
		return 0, err
	}
	h, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil || h <= 0 {
		return 720, nil // reasonable fallback for bar sizing
	}
	return h, nil
}

// encodeGIF renders a high-quality palette and applies it in a second pass,
// honoring the shared video chain and an optional loop offset.
func encodeGIF(src, dst, fps, chain string, opts Options) error {
	palette := filepath.Join(os.TempDir(), "vhsweb-palette.png")
	defer os.Remove(palette)

	base := fmt.Sprintf("[0:v]%s,fps=%s", chain, fps)

	// Palette generation is order-independent, so it ignores the loop offset.
	gen := []string{
		"-y", "-i", src,
		"-filter_complex", base + ",palettegen",
		palette,
	}
	if err := runFFmpeg(gen); err != nil {
		return fmt.Errorf("generating palette: %w", err)
	}

	var lavfi string
	if t := loopOffsetSeconds(src, opts); t > 0 {
		// Rotate the loop: play [t:end] then [0:t] so the GIF restarts mid-way.
		ts := trimFloat(t)
		lavfi = fmt.Sprintf(
			"%s,split[la][lb];[la]trim=start=%s,setpts=PTS-STARTPTS[lc];"+
				"[lb]trim=end=%s,setpts=PTS-STARTPTS[ld];[lc][ld]concat=n=2:v=1:a=0[x];"+
				"[x][1:v]paletteuse",
			base, ts, ts)
	} else {
		lavfi = fmt.Sprintf("%s[x];[x][1:v]paletteuse", base)
	}

	apply := []string{
		"-y", "-i", src, "-i", palette,
		"-filter_complex", lavfi,
		dst,
	}
	if err := runFFmpeg(apply); err != nil {
		return fmt.Errorf("applying palette: %w", err)
	}
	return nil
}

// loopOffsetSeconds converts the configured loop-offset fraction into seconds
// on the output timeline, or 0 when no offset applies.
func loopOffsetSeconds(src string, opts Options) float64 {
	if opts.LoopOffset <= 0 {
		return 0
	}
	durStr, err := probeDuration(src)
	if err != nil {
		return 0
	}
	raw, err := strconv.ParseFloat(durStr, 64)
	if err != nil {
		return 0
	}
	removed := 0.0
	for _, c := range opts.Cuts {
		if c.EndMs > c.StartMs {
			removed += float64(c.EndMs-c.StartMs) / 1000
		}
	}
	final := (raw - removed) / opts.speed()
	if final <= 0 {
		return 0
	}
	return opts.LoopOffset * final
}

// parseColor returns an ffmpeg color string plus the 0-255 r,g,b components.
// "#RRGGBB" / "0xRRGGBB" are parsed exactly; anything else passes through to
// ffmpeg as a named color and reports white components for corner fills.
func parseColor(s string) (ff string, r, g, b int) {
	hex := strings.TrimPrefix(strings.TrimPrefix(s, "#"), "0x")
	if len(hex) == 6 {
		if v, err := strconv.ParseInt(hex, 16, 64); err == nil {
			return "0x" + strings.ToUpper(hex), int(v>>16) & 0xff, int(v>>8) & 0xff, int(v) & 0xff
		}
	}
	return s, 255, 255, 255
}

// trimFloat formats a float without a trailing ".0" or excess zeros.
func trimFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

func runFFmpeg(args []string) error {
	cmd := exec.Command("ffmpeg", args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg failed: %w\n%s", err, stderr.String())
	}
	return nil
}
