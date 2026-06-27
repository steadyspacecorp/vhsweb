package runner

import (
	"testing"

	"github.com/playwright-community/playwright-go"
)

func TestLoadState(t *testing.T) {
	cases := map[string]*playwright.LoadState{
		"load":             playwright.LoadStateLoad,
		"DOMContentLoaded": playwright.LoadStateDomcontentloaded,
		"networkidle":      playwright.LoadStateNetworkidle,
		"#results":         nil, // ordinary selector
		"body":             nil,
	}
	for arg, want := range cases {
		if got := loadState(arg); got != want {
			t.Errorf("loadState(%q) = %v, want %v", arg, got, want)
		}
	}
}
