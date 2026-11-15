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
