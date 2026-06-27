package runner

import (
	"encoding/json"
	"math"
	"time"

	"github.com/playwright-community/playwright-go"
)

// mouseState tracks the pointer's current position so each move can be animated
// from where the cursor actually is. Playwright starts the mouse at (0,0).
type mouseState struct {
	x, y float64
}

// Tuning for the animated glide. Distances are in viewport pixels.
const (
	mouseMinSteps  = 12
	mouseMaxSteps  = 40
	mousePxPerStep = 12
	mouseBaseMove  = 200 * time.Millisecond // floor on travel time
	mouseMaxMove   = 650 * time.Millisecond // ceiling on travel time
)

// moveMouseToSelector scrolls the target into view, finds its center, and glides
// the pointer there. Returns nil (letting the subsequent action report the
// error) if the element has no box, e.g. it isn't visible yet.
func moveMouseToSelector(page playwright.Page, ms *mouseState, selector string) error {
	syncCursorFromPage(page, ms)
	loc := page.Locator(selector)
	if err := loc.ScrollIntoViewIfNeeded(); err != nil {
		return err
	}
	box, err := loc.BoundingBox()
	if err != nil {
		return err
	}
	if box == nil {
		return nil
	}
	return moveMouseTo(page, ms, box.X+box.Width/2, box.Y+box.Height/2)
}

// moveMouseTo animates the pointer from its current position to (x, y) with an
// ease-in-out curve and a gentle arc, dispatching intermediate mousemove events
// so the on-page cursor glides instead of teleporting. Travel time scales with
// distance, clamped to a human-ish range.
func moveMouseTo(page playwright.Page, ms *mouseState, x, y float64) error {
	dx, dy := x-ms.x, y-ms.y
	dist := math.Hypot(dx, dy)
	if dist < 1 {
		ms.x, ms.y = x, y
		return page.Mouse().Move(x, y)
	}

	steps := int(dist / mousePxPerStep)
	if steps < mouseMinSteps {
		steps = mouseMinSteps
	}
	if steps > mouseMaxSteps {
		steps = mouseMaxSteps
	}

	total := mouseBaseMove + time.Duration(dist*float64(time.Millisecond))
	if total > mouseMaxMove {
		total = mouseMaxMove
	}
	stepDelay := total / time.Duration(steps)

	// Unit normal to the travel line; the path bows out along it and returns to
	// zero at both ends (sin), so the move still lands exactly on the target.
	nx, ny := -dy/dist, dx/dist
	arc := math.Min(dist*0.12, 36)

	for i := 1; i <= steps; i++ {
		t := float64(i) / float64(steps)
		e := easeInOut(t)
		bow := arc * math.Sin(math.Pi*t)
		px := ms.x + dx*e + nx*bow
		py := ms.y + dy*e + ny*bow
		if err := page.Mouse().Move(px, py); err != nil {
			return err
		}
		time.Sleep(stepDelay)
	}
	ms.x, ms.y = x, y
	return nil
}

// syncCursorFromPage reads the on-page cursor's actual position (exposed on
// window by the overlay script) into ms, so an animation resumes from where the
// cursor really is — notably the viewport center after a navigation recreated
// the overlay. A missing value (e.g. mid-navigation) leaves ms unchanged.
func syncCursorFromPage(page playwright.Page, ms *mouseState) {
	// Read via JSON so we don't depend on how the client decodes JS numbers.
	val, err := page.Evaluate("() => JSON.stringify([window.__vhswebCursorX ?? null, window.__vhswebCursorY ?? null])")
	if err != nil {
		return
	}
	s, ok := val.(string)
	if !ok {
		return
	}
	var xy []*float64
	if json.Unmarshal([]byte(s), &xy) != nil || len(xy) != 2 || xy[0] == nil || xy[1] == nil {
		return
	}
	ms.x, ms.y = *xy[0], *xy[1]
}

// easeInOut is the standard quadratic ease: slow start, fast middle, slow stop.
func easeInOut(t float64) float64 {
	if t < 0.5 {
		return 2 * t * t
	}
	return 1 - math.Pow(-2*t+2, 2)/2
}
