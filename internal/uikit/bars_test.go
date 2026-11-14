package uikit

import (
	"strings"
	"testing"
)

func TestBarEighths(t *testing.T) {
	cases := []struct {
		fraction float64
		width    int
		want     string
	}{
		{1, 10, strings.Repeat("█", 10)},
		{0.5, 10, strings.Repeat("█", 5)},
		{0.55, 10, strings.Repeat("█", 5) + "▌"},
		{0.01, 10, "▏"}, // a positive value never vanishes
		{0, 10, ""},
		{-1, 10, ""},
		{2, 4, "████"}, // clamped
		{1, 0, ""},
	}
	for _, c := range cases {
		if got := BarEighths(c.fraction, c.width); got != c.want {
			t.Errorf("BarEighths(%v, %d) = %q, want %q", c.fraction, c.width, got, c.want)
		}
	}
}
