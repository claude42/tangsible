package uikit

import (
	"regexp"
	"strings"
	"testing"
)

var tagPattern = regexp.MustCompile(`\[[^\]]*\]`)

func plainCells(s string) string { return tagPattern.ReplaceAllString(s, "") }

func TestLabeledBarLongBar(t *testing.T) {
	got := LabeledBar("Play A", 1, 20, "blue", "#fff", "silver")
	if want := "[#fff:blue] Play A" + strings.Repeat(" ", 13) + "[-:-]"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if n := len([]rune(plainCells(got))); n != 20 {
		t.Errorf("width = %d, want 20", n)
	}
}

func TestLabeledBarShortBar(t *testing.T) {
	// 6 of 20 cells filled: " Not q" inverted, the rest of the label normal.
	got := LabeledBar("Not quite as long", 0.25, 24, "blue", "#fff", "silver")
	if !strings.HasPrefix(got, "[#fff:blue] Not q[-:-]") {
		t.Errorf("inverted part wrong: %q", got)
	}
	if !strings.Contains(got, "[silver]uite as long[-]") {
		t.Errorf("outside part wrong: %q", got)
	}
	if n := len([]rune(plainCells(got))); n != 24 {
		t.Errorf("width = %d, want 24", n)
	}
}

func TestLabeledBarPartialCell(t *testing.T) {
	// 2.5 cells (20 eighths of 8 cells at 0.3125): label shorter than the bar,
	// so the partial block shows after the inverted run.
	got := LabeledBar("a", 2.5/8, 8, "blue", "#fff", "silver")
	if plain := plainCells(got); !strings.HasPrefix(plain, " a▌") {
		t.Errorf("plain = %q, want ' a▌' then padding", plain)
	}
	// A long label takes over the partial cell: no sliver glyph.
	got = LabeledBar("abcdef", 2.5/8, 8, "blue", "#fff", "silver")
	if strings.Contains(got, "▌") || !strings.Contains(plainCells(got), " abcdef") {
		t.Errorf("long label should replace the sliver: %q", got)
	}
}

func TestLabeledBarTinyAndEmpty(t *testing.T) {
	got := LabeledBar("x", 0.001, 20, "blue", "#fff", "silver")
	if plain := plainCells(got); !strings.HasPrefix(plain, "▏x") || len([]rune(plain)) != 20 {
		t.Errorf("a sub-cell bar keeps its sliver: %q", plain)
	}
	if plain := plainCells(LabeledBar("x", 0, 10, "blue", "#fff", "silver")); !strings.HasPrefix(plain, " x") || len([]rune(plain)) != 10 {
		t.Errorf("a zero bar is just the label: %q", plain)
	}
}

func TestLabeledBarTruncatesAndEscapes(t *testing.T) {
	got := LabeledBar("a very long play name indeed", 1, 10, "blue", "#fff", "silver")
	if plain := plainCells(got); plain != " a very l…" {
		t.Errorf("plain = %q", plain)
	}
	if got := LabeledBar("[red]x", 1, 12, "blue", "#fff", "silver"); !strings.Contains(got, "[red[]x") {
		t.Errorf("label must be tview-escaped: %q", got)
	}
}

func TestContrastTextColor(t *testing.T) {
	cases := map[string]string{"lightsteelblue": "#000000", "#101030": "#ffffff", "default": "#000000"}
	for in, want := range cases {
		if got := ContrastTextColor(in); got != want {
			t.Errorf("ContrastTextColor(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSpanBarOffsetAndWidth(t *testing.T) {
	// A bar starting halfway, a quarter long, in 40 cells: 20 blank cells,
	// then 10 filled cells with the label inside.
	got := plainCells(SpanBar("build", 0.5, 0.25, 40, "green", "#000000", "white"))
	if want := strings.Repeat(" ", 20) + " build" + strings.Repeat(" ", 14); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if n := len([]rune(got)); n != 40 {
		t.Errorf("width = %d, want 40", n)
	}
}

func TestSpanBarLabelRightAlignsWhenItCannotFitRight(t *testing.T) {
	// Starts near the right edge: no room for the name after the bar's
	// start, so it ends exactly at the right edge.
	got := plainCells(SpanBar("install", 0.9, 0.1, 40, "green", "#000000", "white"))
	if n := len([]rune(got)); n != 40 {
		t.Fatalf("width = %d, want 40: %q", n, got)
	}
	if !strings.HasSuffix(got, "install  ") {
		t.Errorf("label should end two cells before the right edge: %q", got)
	}
}

func TestSpanBarRightAlignedLabelIsInvertedOverTheBar(t *testing.T) {
	got := SpanBar("install", 0.9, 0.1, 40, "green", "#000000", "white")
	// The bar covers the last 4 cells ("ll" and the 2-cell margin): inverted;
	// the rest of the label plain.
	if !strings.Contains(got, "[white]insta[-]") || !strings.Contains(got, "[#000000:green]ll[-:-][#000000:green]  [-:-]") {
		t.Errorf("got %q", got)
	}
}

func TestSpanBarTruncatesAtTheWidth(t *testing.T) {
	got := plainCells(SpanBar("a very long task name indeed", 0.9, 0.1, 20, "green", "#000000", "white"))
	if n := len([]rune(got)); n != 20 || !strings.HasSuffix(got, "…") && !strings.Contains(got, "…") {
		t.Errorf("got %q (%d cells), want 20 cells with …", got, n)
	}
}

func TestSpanBarEdges(t *testing.T) {
	for _, tc := range []struct{ start, length float64 }{{0, 0}, {1, 0.5}, {0, 1}, {-1, 2}} {
		got := plainCells(SpanBar("x", tc.start, tc.length, 10, "green", "#000000", "white"))
		if n := len([]rune(got)); n != 10 {
			t.Errorf("start %v length %v: width %d: %q", tc.start, tc.length, n, got)
		}
	}
}

func TestLabeledBarMarked(t *testing.T) {
	// The marker shows on a blank cell and on a filled one...
	if plain := plainCells(LabeledBarMarked("a", 0.5, 10, "blue", "#fff", "silver", 8, "gray")); plain != " a      ┆ " {
		t.Errorf("blank cell: %q", plain)
	}
	got := LabeledBarMarked("a", 1, 10, "blue", "#fff", "silver", 5, "gray")
	if plain := plainCells(got); plain != " a   ┆    " {
		t.Errorf("filled cell: %q", plain)
	}
	if !strings.Contains(got, "[#fff:blue]┆[-:-]") {
		t.Errorf("on the bar the marker takes the bar's text color: %q", got)
	}
	// ...but never over a label character, and a marker outside the area is ignored.
	if plain := plainCells(LabeledBarMarked("abc", 1, 10, "blue", "#fff", "silver", 2, "gray")); plain != plainCells(LabeledBar("abc", 1, 10, "blue", "#fff", "silver")) {
		t.Errorf("a label character stays: %q", plain)
	}
	for _, m := range []int{-1, 10, 99} {
		if a, b := LabeledBarMarked("x", 0.3, 10, "blue", "#fff", "silver", m, "gray"), LabeledBar("x", 0.3, 10, "blue", "#fff", "silver"); a != b {
			t.Errorf("mark %d should change nothing: %q vs %q", m, a, b)
		}
	}
	if got := plainCells(MarkerCell(6, 2, "gray")); got != "  ┆   " {
		t.Errorf("MarkerCell = %q", got)
	}
	if got := MarkerCell(4, 9, "gray"); got != "    " {
		t.Errorf("MarkerCell out of range = %q", got)
	}
}
