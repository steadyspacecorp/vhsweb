package encoder

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestTimeline(t *testing.T) {
	opts := Options{
		Speed: 2,
		Cuts:  []Cut{{StartMs: 1000, EndMs: 2000}},
		Events: []SoundEvent{
			{AtMs: 500, Kind: SoundKey},  // before the cut
			{AtMs: 1500, Kind: SoundKey}, // inside the cut -> dropped
			{AtMs: 3000, Kind: SoundKey}, // after: (3000-1000)/2 = 1000
		},
	}
	got := timeline(opts)
	if len(got) != 2 {
		t.Fatalf("got %d events, want 2: %+v", len(got), got)
	}
	if got[0].AtMs != 250 { // (500-0)/2
		t.Errorf("event 0 at %d, want 250", got[0].AtMs)
	}
	if got[1].AtMs != 1000 {
		t.Errorf("event 1 at %d, want 1000", got[1].AtMs)
	}
}

func TestCutSelect(t *testing.T) {
	if s := cutSelect(nil); s != "" {
		t.Errorf("empty cuts: got %q", s)
	}
	got := cutSelect([]Cut{{StartMs: 500, EndMs: 1500}, {StartMs: 2000, EndMs: 2000}})
	want := "not(between(t,0.5,1.5))" // zero-length cut skipped
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestParseColor(t *testing.T) {
	ff, r, g, b := parseColor("#FF8000")
	if ff != "0xFF8000" || r != 255 || g != 128 || b != 0 {
		t.Errorf("hex: got %q %d,%d,%d", ff, r, g, b)
	}
	if ff, _, _, _ := parseColor("red"); ff != "red" {
		t.Errorf("named: got %q", ff)
	}
}

// makeSource renders a small synthetic WebM to stand in for a Playwright
// recording, so the ffmpeg graphs can be exercised without a browser.
func makeSource(t *testing.T, dir string) string {
	t.Helper()
	src := filepath.Join(dir, "src.webm")
	cmd := exec.Command("ffmpeg", "-y", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc=size=320x180:rate=15:duration=2",
		"-c:v", "libvpx-vp9", "-b:v", "0", "-crf", "40", src)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("making source: %v\n%s", err, out)
	}
	return src
}

func TestEncodeGraphs(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not on PATH")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not on PATH")
	}
	dir := t.TempDir()
	src := makeSource(t, dir)

	cases := []struct {
		name string
		dst  string
		opts Options
	}{
		{"plain mp4", "plain.mp4", Options{Framerate: 15}},
		{"framed mp4", "framed.mp4", Options{Framerate: 15, Frame: Frame{
			Padding: 8, Margin: 16, MarginFill: "#1E1E1E", WindowBar: "Colorful", BorderRadius: 20,
		}}},
		{"framed right bar", "right.mp4", Options{Framerate: 15, Frame: Frame{
			Margin: 10, MarginFill: "#FFFFFF", WindowBar: "ColorfulRight", BorderRadius: 12,
		}}},
		{"cuts + speed", "cut.mp4", Options{Framerate: 15, Speed: 2,
			Cuts: []Cut{{StartMs: 500, EndMs: 1000}}}},
		{"sound", "sound.mp4", Options{Framerate: 15, Events: []SoundEvent{
			{AtMs: 200, Kind: SoundClick}, {AtMs: 800, Kind: SoundKey},
		}}},
		{"webm framed", "out.webm", Options{Framerate: 15, Frame: Frame{Margin: 8, MarginFill: "#000000"}}},
		{"gif", "out.gif", Options{Framerate: 15}},
		{"gif loop offset", "offset.gif", Options{Framerate: 15, LoopOffset: 0.25}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dst := filepath.Join(dir, c.dst)
			if err := Encode(src, dst, c.opts); err != nil {
				t.Fatalf("Encode: %v", err)
			}
			fi, err := os.Stat(dst)
			if err != nil {
				t.Fatalf("output missing: %v", err)
			}
			if fi.Size() == 0 {
				t.Fatalf("output is empty")
			}
		})
	}
}
