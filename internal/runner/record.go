package runner

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/playwright-community/playwright-go"
)

// recEvent is one interaction posted from the page recorder.
type recEvent struct {
	Type     string `json:"type"` // click, fill, press, scroll
	Selector string `json:"selector"`
	Value    string `json:"value"`
	Key      string `json:"key"`
	Y        int    `json:"y"` // scroll position
	T        int64  `json:"t"` // ms since page load
}

// recorderScript is injected into every page (and survives navigation). It
// watches for interactions, builds a best-effort selector, and posts each
// event back to Go through the exposed __vhswebRecord binding.
const recorderScript = `(() => {
  if (window.__vhswebInstalled) return;
  window.__vhswebInstalled = true;
  const esc = (s) => (window.CSS && CSS.escape) ? CSS.escape(s) : s;
  function sel(el) {
    if (!el || el.nodeType !== 1) return null;
    if (el.id) return '#' + esc(el.id);
    const tid = el.getAttribute && el.getAttribute('data-testid');
    if (tid) return '[data-testid="' + tid + '"]';
    const parts = [];
    let node = el;
    while (node && node.nodeType === 1 && node !== document.documentElement) {
      if (node.id) { parts.unshift('#' + esc(node.id)); break; }
      let part = node.tagName.toLowerCase();
      const p = node.parentElement;
      if (p) {
        const same = Array.prototype.filter.call(p.children, (c) => c.tagName === node.tagName);
        if (same.length > 1) part += ':nth-of-type(' + (same.indexOf(node) + 1) + ')';
      }
      parts.unshift(part);
      node = node.parentElement;
    }
    return parts.join(' > ');
  }
  const send = (o) => { try { window.__vhswebRecord(JSON.stringify(o)); } catch (e) {} };
  const isField = (el) => el && el.matches && el.matches('input, textarea, select');
  document.addEventListener('click', (e) => {
    if (isField(e.target)) return; // a fill is recorded on change instead
    const s = sel(e.target);
    if (s) send({ type: 'click', selector: s, t: Date.now() });
  }, true);
  document.addEventListener('change', (e) => {
    if (isField(e.target)) send({ type: 'fill', selector: sel(e.target), value: e.target.value == null ? '' : String(e.target.value), t: Date.now() });
  }, true);
  document.addEventListener('keydown', (e) => {
    const keys = ['Enter', 'Tab', 'Escape', 'ArrowDown', 'ArrowUp', 'ArrowLeft', 'ArrowRight'];
    if (keys.indexOf(e.key) !== -1) send({ type: 'press', key: e.key, t: Date.now() });
  }, true);
  let st;
  document.addEventListener('scroll', () => {
    clearTimeout(st);
    st = setTimeout(() => send({ type: 'scroll', y: Math.round(window.scrollY || 0), t: Date.now() }), 200);
  }, true);
})();`

// Record opens a real browser at url, captures the user's interactions, and
// writes a .tape describing them to out (stdout when out is "" or "-").
// Recording stops when the user presses Enter in the terminal or closes the
// browser window.
func Record(url, out string, width, height int) error {
	pw, err := playwright.Run()
	if err != nil {
		return fmt.Errorf("starting playwright: %w", err)
	}
	defer pw.Stop()

	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(false),
	})
	if err != nil {
		return fmt.Errorf("launching chromium: %w", err)
	}

	ctx, err := browser.NewContext(playwright.BrowserNewContextOptions{
		Viewport: &playwright.Size{Width: width, Height: height},
	})
	if err != nil {
		return fmt.Errorf("creating context: %w", err)
	}

	var (
		mu     sync.Mutex
		events []recEvent
	)
	if err := ctx.ExposeBinding("__vhswebRecord", func(_ *playwright.BindingSource, args ...any) any {
		if len(args) == 0 {
			return nil
		}
		s, ok := args[0].(string)
		if !ok {
			return nil
		}
		var e recEvent
		if json.Unmarshal([]byte(s), &e) == nil {
			mu.Lock()
			events = append(events, e)
			mu.Unlock()
		}
		return nil
	}); err != nil {
		return fmt.Errorf("exposing recorder binding: %w", err)
	}
	if err := ctx.AddInitScript(playwright.Script{Content: playwright.String(recorderScript)}); err != nil {
		return fmt.Errorf("installing recorder: %w", err)
	}

	page, err := ctx.NewPage()
	if err != nil {
		return fmt.Errorf("opening page: %w", err)
	}
	if _, err := page.Goto(url); err != nil {
		return fmt.Errorf("navigating to %s: %w", url, err)
	}

	// Stop on terminal Enter or browser close, whichever comes first.
	done := make(chan struct{})
	var once sync.Once
	stop := func() { once.Do(func() { close(done) }) }
	browser.OnDisconnected(func(playwright.Browser) { stop() })
	go func() {
		bufio.NewReader(os.Stdin).ReadString('\n')
		stop()
	}()
	fmt.Fprintln(os.Stderr, "Recording — interact with the page, then press Enter here to finish.")
	<-done
	_ = browser.Close()

	mu.Lock()
	tape := buildTape(url, width, height, events)
	mu.Unlock()

	if out == "" || out == "-" {
		fmt.Print(tape)
		return nil
	}
	if err := os.WriteFile(out, []byte(tape), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Wrote %s\n", out)
	return nil
}

// buildTape turns captured events into a .tape script, inferring Sleep pauses
// from the real gaps between interactions.
func buildTape(url string, w, h int, evs []recEvent) string {
	var b strings.Builder
	b.WriteString("Output recording.mp4\n")
	fmt.Fprintf(&b, "Set Width %d\n", w)
	fmt.Fprintf(&b, "Set Height %d\n\n", h)
	fmt.Fprintf(&b, "Goto %s\n", url)

	prevT := int64(-1)
	lastY := 0
	for _, e := range collapseFills(evs) {
		// Decide the action line first; skip events that emit nothing (e.g. a
		// scroll that didn't move the window) so they leave no orphan Sleep.
		var line string
		switch e.Type {
		case "click":
			line = "Click " + quoteArg(e.Selector)
		case "fill":
			line = "Fill " + quoteArg(e.Selector) + " " + quoteArg(e.Value)
		case "press":
			line = "Press " + e.Key
		case "scroll":
			d := e.Y - lastY
			lastY = e.Y
			switch {
			case d > 0:
				line = fmt.Sprintf("Scroll Down %d", d)
			case d < 0:
				line = fmt.Sprintf("Scroll Up %d", -d)
			default:
				continue
			}
		default:
			continue
		}

		// Sleep covers the real gap since the last emitted action (so time spent
		// on skipped events folds into the next pause).
		if prevT >= 0 {
			if gap := e.T - prevT; gap >= 200 {
				fmt.Fprintf(&b, "Sleep %dms\n", roundTo(gap, 100))
			}
		}
		fmt.Fprintf(&b, "%s\n", line)
		prevT = e.T
	}
	return b.String()
}

// collapseFills keeps only the final value when the same field is edited in a
// run of consecutive change events (typing fires several).
func collapseFills(evs []recEvent) []recEvent {
	out := make([]recEvent, 0, len(evs))
	for _, e := range evs {
		if e.Type == "fill" && len(out) > 0 {
			last := out[len(out)-1]
			if last.Type == "fill" && last.Selector == e.Selector {
				out[len(out)-1] = e
				continue
			}
		}
		out = append(out, e)
	}
	return out
}

// roundTo rounds n to the nearest step (and never below step).
func roundTo(n int64, step int64) int64 {
	r := ((n + step/2) / step) * step
	if r < step {
		return step
	}
	return r
}

// quoteArg wraps a tape argument in quotes the tokenizer can read back.
func quoteArg(s string) string {
	if !strings.ContainsAny(s, "\"") {
		return `"` + s + `"`
	}
	if !strings.ContainsAny(s, "'") {
		return "'" + s + "'"
	}
	return `"` + strings.ReplaceAll(s, `"`, "") + `"`
}
