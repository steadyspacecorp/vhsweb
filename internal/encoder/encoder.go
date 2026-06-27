// Package encoder transcodes the raw Playwright WebM recording into the
// user-requested output format using ffmpeg, optionally mixing in click and
// keystroke sound effects at recorded timestamps.
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

// Encode converts the WebM recording at src into dst, choosing settings from
// dst's extension (.mp4, .gif, or .webm). When events are present, a click /
// keystroke audio track is mixed into mp4 and webm output (gif is always
// silent). framerate sets the output fps.
func Encode(src, dst string, framerate int, events []SoundEvent) error {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return fmt.Errorf("ffmpeg not found on PATH: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(dst))
	if ext == "" {
		ext = ".mp4"
		dst += ".mp4"
	}
	fps := strconv.Itoa(framerate)

	switch ext {
	case ".mp4", ".webm":
		if len(events) > 0 {
			return encodeWithSound(src, dst, ext, fps, events)
		}
		return runFFmpeg(silentArgs(src, dst, ext, fps))
	case ".gif":
		return encodeGIF(src, dst, fps)
	default:
		return fmt.Errorf("unsupported output extension %q", ext)
	}
}

// silentArgs builds the ffmpeg arguments for an audio-free transcode.
func silentArgs(src, dst, ext, fps string) []string {
	if ext == ".webm" {
		return []string{
			"-y", "-i", src, "-r", fps,
			"-c:v", "libvpx-vp9", "-b:v", "0", "-crf", "30",
			dst,
		}
	}
	return []string{
		"-y", "-i", src, "-r", fps,
		"-vf", "scale=trunc(iw/2)*2:trunc(ih/2)*2",
		"-pix_fmt", "yuv420p",
		"-c:v", "libx264", "-preset", "medium",
		"-movflags", "+faststart",
		dst,
	}
}

// encodeWithSound transcodes the video and mixes the click/keystroke samples
// onto a generated audio track at their recorded timestamps.
func encodeWithSound(src, dst, ext, fps string, events []SoundEvent) error {
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
	eventInputs, filter := buildSoundMix(events, assetPath)
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
			"-r", fps, "-c:v", "libvpx-vp9", "-b:v", "0", "-crf", "30",
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
// -filter_complex graph: scale the video, then delay one sample per event and
// amix them onto the silent base track. Each kind cycles through its numbered
// variants (key-1..4, click-1..4) so repeated keystrokes don't sound identical.
// assetPath maps a sample file name to its on-disk path.
func buildSoundMix(events []SoundEvent, assetPath map[string]string) (inputs []string, filter string) {
	var b strings.Builder
	// Video chain.
	b.WriteString("[0:v]scale=trunc(iw/2)*2:trunc(ih/2)*2,format=yuv420p[vout];")

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

// encodeGIF renders a high-quality palette and applies it in a second pass.
func encodeGIF(src, dst, fps string) error {
	palette := filepath.Join(os.TempDir(), "vhsweb-palette.png")
	defer os.Remove(palette)

	gen := []string{
		"-y", "-i", src,
		"-vf", fmt.Sprintf("fps=%s,scale=trunc(iw/2)*2:-2:flags=lanczos,palettegen", fps),
		palette,
	}
	if err := runFFmpeg(gen); err != nil {
		return fmt.Errorf("generating palette: %w", err)
	}

	apply := []string{
		"-y", "-i", src, "-i", palette,
		"-lavfi", fmt.Sprintf("fps=%s,scale=trunc(iw/2)*2:-2:flags=lanczos[x];[x][1:v]paletteuse", fps),
		dst,
	}
	if err := runFFmpeg(apply); err != nil {
		return fmt.Errorf("applying palette: %w", err)
	}
	return nil
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
