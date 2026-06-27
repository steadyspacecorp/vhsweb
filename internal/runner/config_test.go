package runner

import "testing"

func TestApplySetNewKeys(t *testing.T) {
	c := DefaultConfig()
	set := func(k, v string) {
		t.Helper()
		if err := c.applySet(k, v); err != nil {
			t.Fatalf("Set %s %s: %v", k, v, err)
		}
	}
	set("PlaybackSpeed", "1.5")
	set("LoopOffset", "20%")
	set("Margin", "40")
	set("MarginFill", "#1E1E1E")
	set("Padding", "16")
	set("BorderRadius", "24")
	set("WindowBar", "ColorfulRight")
	set("Theme", "dark")

	if c.ColorScheme != "dark" {
		t.Errorf("ColorScheme = %q, want dark", c.ColorScheme)
	}
	if c.PlaybackSpeed != 1.5 {
		t.Errorf("PlaybackSpeed = %v", c.PlaybackSpeed)
	}
	if c.LoopOffset != 0.2 {
		t.Errorf("LoopOffset = %v, want 0.2", c.LoopOffset)
	}
	if c.Margin != 40 || c.Padding != 16 || c.BorderRadius != 24 {
		t.Errorf("framing px = %d/%d/%d", c.Margin, c.Padding, c.BorderRadius)
	}
	if c.MarginFill != "#1E1E1E" || c.WindowBar != "ColorfulRight" {
		t.Errorf("fill/bar = %q/%q", c.MarginFill, c.WindowBar)
	}
}

func TestApplySetRejects(t *testing.T) {
	c := DefaultConfig()
	for _, tc := range []struct{ key, val string }{
		{"WindowBar", "Bogus"},
		{"PlaybackSpeed", "0"},
		{"Margin", "-5"},
		{"BorderRadius", "x"},
		{"Theme", "sepia"},
	} {
		if err := c.applySet(tc.key, tc.val); err == nil {
			t.Errorf("Set %s %s: expected error", tc.key, tc.val)
		}
	}
}

func TestParseFraction(t *testing.T) {
	cases := map[string]float64{"20%": 0.2, "0.5": 0.5, "150%": 0.999, "-1": 0}
	for in, want := range cases {
		got, err := parseFraction(in)
		if err != nil {
			t.Fatalf("parseFraction(%q): %v", in, err)
		}
		if got != want {
			t.Errorf("parseFraction(%q) = %v, want %v", in, got, want)
		}
	}
}
