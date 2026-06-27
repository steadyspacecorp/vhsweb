package runner

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/playwright-community/playwright-go"
)

func TestBuildTape(t *testing.T) {
	evs := []recEvent{
		{Type: "click", Selector: "#go", T: 1000},
		{Type: "fill", Selector: "#name", Value: "ab", T: 1500},
		{Type: "fill", Selector: "#name", Value: "abc", T: 1600}, // collapses with prev
		{Type: "press", Key: "Enter", T: 3000},                   // 1400ms gap -> Sleep 1400ms
		{Type: "scroll", Y: 600, T: 3100},
		{Type: "scroll", Y: 200, T: 3300},
		{Type: "scroll", Selector: "#panel", Y: 300, T: 3500}, // inner container
	}
	got := buildTape("https://example.com", 1280, 720, evs)
	for _, want := range []string{
		"Goto https://example.com",
		`Click "#go"`,
		`Fill "#name" "abc"`,
		"Sleep 1400ms",
		"Press Enter",
		"Scroll Down 600",
		"Scroll Up 400",
		`Scroll Down 300 "#panel"`, // delta tracked separately from window
	} {
		if !strings.Contains(got, want) {
			t.Errorf("tape missing %q\n---\n%s", want, got)
		}
	}
	if strings.Contains(got, `Fill "#name" "ab"`) {
		t.Errorf("consecutive fill not collapsed:\n%s", got)
	}
}

func TestBuildTapeDropsNoopScrolls(t *testing.T) {
	// Scrolls that don't move the window (common on SPAs with inner scroll
	// containers) must not leave orphan Sleep lines.
	evs := []recEvent{
		{Type: "click", Selector: "#a", T: 1000},
		{Type: "scroll", Y: 0, T: 2000}, // no movement
		{Type: "scroll", Y: 0, T: 3000}, // no movement
		{Type: "click", Selector: "#b", T: 4000},
	}
	got := buildTape("https://example.com", 1280, 720, evs)
	if strings.Contains(got, "Scroll") {
		t.Errorf("zero-delta scroll produced a Scroll line:\n%s", got)
	}
	// Exactly one Sleep, covering the full 3s gap between the two clicks.
	if n := strings.Count(got, "Sleep"); n != 1 {
		t.Errorf("got %d Sleep lines, want 1:\n%s", n, got)
	}
	if !strings.Contains(got, "Sleep 3000ms") {
		t.Errorf("want a single Sleep 3000ms between clicks:\n%s", got)
	}
}

// TestScrollSelectorMovesInnerContainer verifies Scroll with a selector wheels
// the targeted element rather than the page.
func TestScrollSelectorMovesInnerContainer(t *testing.T) {
	pw, err := playwright.Run()
	if err != nil {
		t.Skipf("playwright unavailable: %v", err)
	}
	defer pw.Stop()
	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{Headless: playwright.Bool(true)})
	if err != nil {
		t.Skipf("chromium unavailable: %v", err)
	}
	defer browser.Close()
	page, err := browser.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	html := `<div id="box" style="width:300px;height:200px;overflow:auto">` +
		`<div style="height:3000px">tall</div></div>`
	if _, err := page.Goto("data:text/html," + html); err != nil {
		t.Fatal(err)
	}

	if err := scroll(page, &mouseState{}, []string{"Down", "300", "#box"}); err != nil {
		t.Fatal(err)
	}
	top, err := page.Locator("#box").Evaluate("el => el.scrollTop", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !positive(top) {
		t.Errorf("inner container scrollTop = %v, want > 0", top)
	}
}

// positive reports whether an Evaluate result is a number greater than zero
// (playwright-go may decode JS numbers as int or float64).
func positive(v any) bool {
	switch n := v.(type) {
	case int:
		return n > 0
	case float64:
		return n > 0
	default:
		return false
	}
}

// TestRecorderCapturesInteractions drives a page programmatically to confirm
// the injected recorder + binding actually capture clicks, fills, and presses.
func TestRecorderCapturesInteractions(t *testing.T) {
	pw, err := playwright.Run()
	if err != nil {
		t.Skipf("playwright unavailable: %v", err)
	}
	defer pw.Stop()

	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{Headless: playwright.Bool(true)})
	if err != nil {
		t.Skipf("chromium unavailable: %v", err)
	}
	defer browser.Close()

	ctx, err := browser.NewContext()
	if err != nil {
		t.Fatal(err)
	}

	var (
		mu     sync.Mutex
		events []recEvent
	)
	if err := ctx.ExposeBinding("__vhswebRecord", func(_ *playwright.BindingSource, args ...any) any {
		var e recEvent
		if s, ok := args[0].(string); ok && json.Unmarshal([]byte(s), &e) == nil {
			mu.Lock()
			events = append(events, e)
			mu.Unlock()
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := ctx.AddInitScript(playwright.Script{Content: playwright.String(recorderScript)}); err != nil {
		t.Fatal(err)
	}

	page, err := ctx.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	// A real navigation makes the init-script recorder run on this document.
	if _, err := page.Goto("data:text/html,<button id=%22go%22>Go</button><input id=%22name%22>"); err != nil {
		t.Fatal(err)
	}
	if installed, _ := page.Evaluate("() => !!window.__vhswebInstalled"); installed != true {
		t.Fatalf("recorder did not install on the page")
	}

	if err := page.Click("#go"); err != nil {
		t.Fatal(err)
	}
	if err := page.Fill("#name", "hello"); err != nil {
		t.Fatal(err)
	}
	if err := page.Press("#name", "Enter"); err != nil {
		t.Fatal(err)
	}
	// Fill emits change on blur; click elsewhere to flush it.
	if err := page.Click("#go"); err != nil {
		t.Fatal(err)
	}
	page.WaitForTimeout(300) // let the async binding messages arrive

	mu.Lock()
	defer mu.Unlock()
	kinds := map[string]bool{}
	for _, e := range events {
		kinds[e.Type] = true
	}
	for _, want := range []string{"click", "fill", "press"} {
		if !kinds[want] {
			t.Errorf("did not capture %q event; got %+v", want, events)
		}
	}
}
