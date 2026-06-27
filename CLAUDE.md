# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`vhsweb` is a Go CLI that records a web-page session described by a `.tape`
script to video (MP4 / GIF / WebM). It's "VHS for the browser": it drives a real
Chromium via [playwright-go](https://github.com/playwright-community/playwright-go),
records the session, and transcodes the raw WebM with ffmpeg, optionally mixing
in click/keystroke sound effects from bundled samples.

## Commands

Go is pinned in `mise.toml` (1.26.4). Prefix Go commands with `mise exec --`
(or run `mise activate`) so the pinned toolchain is used.

```sh
mise exec -- go build -o vhsweb .       # build
mise exec -- go test ./...              # run all tests
mise exec -- go test ./internal/parser  # test a single package
mise exec -- go test ./internal/parser -run TestParse  # single test
mise exec -- go vet ./...               # vet

./vhsweb install                        # one-time: fetch Playwright driver + Chromium (~260 MB)

# smoke-test end to end (serve the demo page, then record a tape)
python3 -m http.server 8080 --directory examples &
./vhsweb examples/browsing.tape
```

**Runtime prerequisites:** `ffmpeg` and `ffprobe` on PATH (`brew install ffmpeg`).
`./vhsweb install` downloads a self-contained Playwright driver (bundled Node) and
Chromium — no system Node.js needed. Cached under `~/Library/Caches/ms-playwright-go`
and `~/Library/Caches/ms-playwright`.

## Architecture

The pipeline is **parse → run → encode**, one package each under `internal/`:

- `internal/parser` — turns a `.tape` script into `[]Command`. Line-oriented,
  VHS-like: one keyword + space-separated args per line, with a quote-aware
  tokenizer (`"..."` / `'...'` collapse to one arg). Keywords and their minimum
  arity live in `commandArity`; adding a command means adding a `CommandType`
  const and an arity entry here.
- `internal/runner` — the execution core.
  - `config.go`: `Config` + `DefaultConfig`. `BuildConfig` splits parsed commands
    into recording settings (`Output`, `Set`) vs. ordered action commands. `Set`
    keys are handled in `applySet` (several accept aliases, e.g. `zoom`/`scale`).
  - `runner.go`: `Run` launches Chromium with video recording on, replays each
    action via `execute`, then hands the raw WebM to the encoder. Click/keystroke
    timestamps are collected into `[]encoder.SoundEvent` relative to page-create
    time so sounds line up with the video.
  - `cursor.go`: JS injected as Playwright **init scripts** (survive navigations)
    — `zoomScript` (CSS `zoom` on `<html>`) and `cursorScript` (fake cursor +
    click ripple overlay reacting to native pointer events).
  - `mouse.go`: Playwright teleports the pointer to each target, so before every
    `Click`/`Hover` the runner animates the glide itself — `moveMouseTo` walks
    from the tracked `mouseState` to the element center over many `Mouse().Move`
    steps with an ease-in-out curve and a slight arc, so the on-page cursor moves
    realistically. Travel time scales with distance.
- `internal/encoder` — `Encode` picks ffmpeg settings from the output extension.
  Sound uses **bundled `.mp3` samples** embedded via `//go:embed` from
  `assets/` (four click + four key variants, from vercel-labs/webreel, Apache-2.0
  — see `assets/CREDITS.md`). `buildSoundMix` adds one sample input per event,
  `adelay`s each to its timestamp, and `amix`es them onto a silent base track;
  variants cycle per kind and `tickVolume` jitters the level so repeats differ.
  GIF is always silent (two-pass palettegen/paletteuse).

`main.go` is the CLI shell: `run` (record a `.tape`), `new` (write a starter
tape), `install` (fetch Chromium), `help`.

## Conventions & gotchas

- **Adding a tape action** touches three places: parser (`CommandType` + arity),
  `runner.execute` (the dispatch switch), and the README command tables.
- **Zoom is a magnify, not true DPR.** The viewport is sized to `Width*Zoom ×
  Height*Zoom` and page content is CSS-zoomed so the browser rasterizes at final
  pixel size (crisp). Consequence: CSS `@media` breakpoints evaluate against the
  larger width. True retina DPR would need a different capture backend.
- **playwright-go is pinned to `v0.6000.0`** in `go.mod` — `v0.6100.0` ships a
  broken `go.mod` (wrong module path, won't build). Don't bump it casually.
- **Durations** accept Go syntax (`500ms`, `2s`) or a bare integer interpreted as
  milliseconds (`parseDuration` in `config.go`).
- Module path is `github.com/steadyspacecorp/vhs-browser` but the binary/CLI is
  `vhsweb`.
- Framerate is resampled by ffmpeg from Playwright's capture rate, not captured
  frame-exact.
