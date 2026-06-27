# vhsweb — VHS for the browser

Write a `.tape` script, get a video. The excellent [VHS](https://github.com/charmbracelet/vhs)
records terminals; **vhsweb** records web pages using the same, user-friendly patterns.
It drives a real browser with [Playwright](https://playwright.dev) and encodes the session to MP4, GIF, or
WebM with ffmpeg.

```tape
Output demo.mp4
Set Width 1600
Set Height 900

Goto https://continuouscoordination.org
Sleep 1s
Click "text=Get started"
WaitFor "h1"
Scroll Down 600
Sleep 2s
```

```sh
vhsweb demo.tape   # -> demo.mp4
```

https://github.com/user-attachments/assets/92d9bd90-884c-42ca-a90c-5cef94289470

## Installation

**Prerequisite:** [ffmpeg](https://ffmpeg.org) on your PATH (`brew install ffmpeg`).
It is used to transcode the recording and mix in sound effects.

You do **not** need Node.js or a separate Playwright install — `vhsweb install`
downloads a self-contained Playwright driver (with its own bundled Node) and the
Chromium browser for you.

### Install the binary

Download the latest prebuilt binary for your OS/arch (macOS and Linux,
amd64/arm64):

```sh
curl -fsSL https://raw.githubusercontent.com/steadyspacecorp/vhsweb/main/install.sh | sh
```

It installs to `~/.local/bin` by default (override with `BIN_DIR=/usr/local/bin`).
Or build from source:

```sh
go build -o vhsweb .
```

### First run

```sh
# One-time: download the Playwright driver + Chromium (~260 MB)
vhsweb install

# Record
vhsweb demo.tape
```

The driver and browser are cached under `~/Library/Caches/ms-playwright-go`
and `~/Library/Caches/ms-playwright` (Linux: `~/.cache/...`), shared with any
other Playwright tools, so the download happens only once per machine.

## Usage

```sh
vhsweb <file.tape>            # record the session described by the tape file
vhsweb record <url>           # drive a browser by hand, write a tape from it
vhsweb validate <file.tape>   # parse-check a tape without recording
vhsweb new <file.tape>        # write a starter tape file
vhsweb install                # download the Playwright Chromium browser
vhsweb help                   # show usage
```

Record flags:

| Flag | Notes |
| --- | --- |
| `-o, --output <file>` | Write to `<file>`, overriding the tape's `Output`. Repeatable — `-o demo.mp4 -o demo.gif` renders both in one run. |
| `-p, --preview` | Replay the tape in a visible browser, record nothing. |
| `-q, --quiet` | Suppress status logging. |

A tape can also be piped in: `vhsweb < demo.tape` (or `vhsweb -o demo.gif -`).

`--preview` replays the tape in a visible browser so you can iterate on selectors
and timing without waiting on the ffmpeg encode. It opens a real window (ignores
`Set Headless`) and writes no `Output` file.

The output format is chosen from the `Output` (or `-o`) file extension: `.mp4`,
`.gif`, or `.webm`.

## Recording a tape by hand

Don't want to hand-write selectors? `vhsweb record` opens a real browser, watches
what you do, and writes the tape for you:

```sh
vhsweb record https://example.com -o demo.tape
# click around, fill fields, scroll… then press Enter in the terminal
vhsweb demo.tape                 # render it
```

A Chromium window opens at the URL. As you interact, vhsweb captures clicks,
field edits, key presses (`Enter`, `Tab`, arrows), and scrolling, inferring
`Sleep`s from your real timing. Recording stops when you press **Enter** in the
terminal (or close the window). Omit `-o` to print the tape to stdout.

The generated tape is a starting point — eyeball the selectors before relying on
it. `#id` and `[data-testid]` selectors are stable; deep `nth-of-type` paths are
the brittle ones worth tidying by hand. Manual address-bar navigation and hovers
aren't captured.

## Tape commands

### Settings

These configure the recording and must use the `Set`/`Output` keywords. Put them
at the top of the file.

| Command | Example | Default | Notes |
| --- | --- | --- | --- |
| `Output <file>` | `Output demo.mp4` | `out.mp4` | Format from extension: mp4 / gif / webm |
| `Set Width <px>` | `Set Width 1280` | `1280` | Logical viewport width |
| `Set Height <px>` | `Set Height 720` | `720` | Logical viewport height |
| `Set Zoom <factor>` | `Set Zoom 2` | `1` | Magnify the page; output is `Width*Zoom x Height*Zoom`, crisp (see below) |
| `Set Framerate <fps>` | `Set Framerate 30` | `30` | Output framerate |
| `Set TypingSpeed <dur>` | `Set TypingSpeed 50ms` | `75ms` | Delay between keystrokes for `Type` |
| `Set WaitTimeout <dur>` | `Set WaitTimeout 30s` | `15s` | Timeout for navigation / `WaitFor` |
| `Set Headless <bool>` | `Set Headless false` | `true` | Show the browser window |
| `Set Cursor <bool>` | `Set Cursor false` | `true` | Overlay a fake mouse cursor in the video |
| `Set Sound <bool>` | `Set Sound false` | `true` | Mix click / keystroke sound effects into mp4 / webm audio |
| `Set Theme <scheme>` | `Set Theme dark` | system | Emulate `prefers-color-scheme`: `dark`, `light`, or `system` |
| `Set PlaybackSpeed <factor>` | `Set PlaybackSpeed 1.5` | `1` | Speed up (`>1`) or slow down (`<1`) the output |
| `Set LoopOffset <pct>` | `Set LoopOffset 20%` | `0%` | GIF only: rotate the loop start point forward |

Window dressing (all off by default, so output is edge-to-edge unless set):

| Command | Example | Default | Notes |
| --- | --- | --- | --- |
| `Set MarginFill <color>` | `Set MarginFill "#1E1E1E"` | `#FFFFFF` | Color of the mat behind the page and in the rounded-corner reveal (hex or named) |
| `Set Margin <px>` | `Set Margin 40` | `0` | `MarginFill` mat around the page |
| `Set Padding <px>` | `Set Padding 20` | `0` | Additional `MarginFill` mat (added to `Margin`) |
| `Set BorderRadius <px>` | `Set BorderRadius 24` | `0` | Round the page corners. Pair with `Margin`/`Padding` so the rounded corners have a mat to sit on |
| `Set WindowBar <style>` | `Set WindowBar Colorful` | none | Title bar with traffic-light dots: `Colorful`, `ColorfulRight`, `Rings`, `RingsRight` |

Because a web page fills its window edge-to-edge (unlike a terminal), `Padding`
and `Margin` both simply add `MarginFill` space around the page — they stack.
`BorderRadius` rounds the page itself and reveals `MarginFill` in the corners,
so it only shows when there's a mat (or a contrasting `MarginFill`).

Durations accept Go syntax (`500ms`, `2s`) or a bare integer (milliseconds).

### Actions

Run in order, recorded in real time.

| Command | Example | Notes |
| --- | --- | --- |
| `Goto <url>` | `Goto https://example.com` | Navigate and wait for load |
| `Click <selector>` | `Click "text=Sign in"` | Playwright selector (CSS, `text=`, etc.) |
| `Type <text>` | `Type "hello"` | Types into the focused element |
| `Fill <selector> <value>` | `Fill "#email" "a@b.co"` | Sets a field value instantly |
| `Press <key>` | `Press Enter` | A keyboard key (`Enter`, `Tab`, `ArrowDown`, ...) |
| `Hover <selector>` | `Hover ".menu"` | Move the pointer over an element |
| `Scroll <dir> [px] [selector]` | `Scroll Down 600 "#panel"` | `Up` / `Down` / `Left` / `Right`, animated. With a selector, scrolls that element (hovers it, then wheels) instead of the page |
| `WaitFor <selector>` | `WaitFor "#results"` | Wait until an element appears. Prefer a selector unique to the new view — not `body`/`html`, which always exist |
| `WaitFor <state>` | `WaitFor networkidle` | Wait on a navigation load state: `load`, `domcontentloaded`, or `networkidle` |
| `Sleep <dur>` | `Sleep 1s` | Pause the recording |
| `Hide` | `Hide` | Stop capturing — actions still run, but their frames are cut |
| `Show` | `Show` | Resume capturing after a `Hide` |
| `Screenshot <file>` | `Screenshot shot.png` | Save a still image mid-session |
| `Source <file.tape>` | `Source setup.tape` | Inline another tape (relative to this file) |

Lines starting with `#` are comments. Quote any argument containing spaces.

`Hide` / `Show` let you run setup steps — logins, navigation — without them
appearing in the final video: the elapsed time between them is removed and sound
effects and timings are shifted to match. A `Hide` with no matching `Show` cuts
everything to the end.

### Zoom & crispness

`Set Zoom 2` with `Set Width 1280` / `Set Height 720` produces a crisp
**2560×1440** video. It works by sizing the browser viewport up to the output
resolution and magnifying the page content by the zoom factor, so the browser
rasterizes text and vectors at the final pixel size (sharp, not upscaled).

Because this magnifies rather than emulating a true device-pixel-ratio, CSS
responsive `@media` breakpoints evaluate against the larger pixel width. For
most app demos that's fine; true-retina DPR would require a different capture
backend.

### Sound

With `Set Sound true` (the default), every `Click` and keystroke is timestamped
during the run, and ffmpeg mixes a short sample onto the audio track at that
moment. The samples are bundled `.mp3`s (four click + four keystroke variants,
cycled so repeats don't sound identical) embedded in the binary. Applies to
**mp4** (AAC) and **webm** (Opus). GIF has no audio track, so GIF output is
always silent.

## How it works

1. **Parse** the `.tape` file into commands (`internal/parser`).
2. **Drive** a Playwright Chromium context with video recording on, replaying
   each action against the page (`internal/runner`). A fake cursor is injected
   so pointer movement and clicks are visible, and the pointer glides to each
   `Click`/`Hover` target along an eased, slightly-arced path rather than
   teleporting.
3. **Encode** the raw WebM recording into the requested format with ffmpeg
   (`internal/encoder`).

Recording captures real wall-clock time, so `Sleep`/`TypingSpeed` in the script
map directly to the pacing in the video.

## Development

**Prerequisites:** [Go](https://go.dev) 1.26+ and [ffmpeg](https://ffmpeg.org).
The Go version is pinned in `mise.toml`; with [mise](https://mise.jdx.dev)
installed, `mise install` provisions it. Prefix Go commands with `mise exec --`
(or run `mise activate`) so the pinned toolchain is used.

```sh
mise install                       # provision the pinned Go toolchain
mise exec -- go build -o vhsweb .    # build
mise exec -- go test ./...         # run tests
mise exec -- go vet ./...          # vet
./vhsweb install                     # fetch the Playwright driver + Chromium

# smoke-test end to end (serve the demo page, then record a tape)
python3 -m http.server 8080 --directory examples &
./vhsweb examples/browsing.tape
```

### Project layout

```
main.go                 CLI entry: run / new / install
internal/parser/        .tape source -> []Command (quote-aware tokenizer)
internal/runner/        config + Playwright execution, cursor & zoom injection
internal/encoder/       ffmpeg: webm -> mp4/gif/webm, sound mixing
examples/               sample tape files
```

### Notes

- The browser is driven through
  [`playwright-community/playwright-go`](https://github.com/playwright-community/playwright-go),
  pinned to `v0.6000.0` — `v0.6100.0` ships a broken `go.mod` (it declares the
  wrong module path and fails to build).
- Click/keystroke sounds are short `.mp3` samples embedded in the binary
  (`internal/encoder/assets/`) and mixed in at encode time. The samples are from
  [vercel-labs/webreel](https://github.com/vercel-labs/webreel) (Apache-2.0); see
  `internal/encoder/assets/CREDITS.md`.

## Limitations

- Framerate is resampled by ffmpeg from Playwright's capture rate, not captured
  frame-exact. For precise framerate control, a CDP screencast backend would be
  the next step.

## Roadmap

Things we'd like to build next.

### Better `record` selectors

[`vhsweb record`](#recording-a-tape-by-hand) ships today with a best-effort
selector heuristic. Leaning on Playwright's own recorder/codegen — which prefers
`text=` / roles / test-ids over brittle `nth-of-type` paths — would make the
captured tapes far more robust. Capturing hovers, manual navigation, and
individual keystrokes (as `Type`) would round it out.

### Frame-exact capture (retina DPR)

A Chrome DevTools Protocol screencast backend would give frame-exact framerate
and true device-pixel-ratio capture, replacing today's `Set Zoom` magnify trick
(which rasterizes crisply but evaluates CSS breakpoints against the larger
width). See [Limitations](#limitations).
