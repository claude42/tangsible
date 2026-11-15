// Copyright 2026 Klaus Wissmann
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package uikit

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// eighthBlocks[i] is a block i/8 of a cell wide, filled from the left
// (U+258F..U+2588), so a bar can end on a fraction of a cell (LabeledBar).
var eighthBlocks = []rune{' ', '▏', '▎', '▍', '▌', '▋', '▊', '▉', '█'}

// ContrastTextColor picks black or white text for a background in color
// bgName (a name or #rrggbb, as in the [colors] config), by luminance - for
// text drawn inside a filled bar. Hex values, not palette names: a theme can
// remap "black"/"white", which would defeat the point.
func ContrastTextColor(bgName string) string {
	r, g, b := tcell.GetColor(bgName).RGB()
	if r < 0 { // a terminal-default color has no RGB value
		return "#000000"
	}
	if 0.299*float64(r)+0.587*float64(g)+0.114*float64(b) < 128 {
		return "#ffffff"
	}
	return "#000000"
}

// LabeledBar renders a bar of width cells, fraction (0..1) of it filled in
// barColor, with label drawn inside it: the label's characters that fall on
// the filled cells are inverted (textColor on the bar color), so they look
// cut out of it, and those that run past the bar's end are drawn normally in
// outsideColor. One cell of bar precedes the label. A bar's partial last
// cell is an eighth block, unless a label character needs that cell. The
// result is exactly width cells wide, tview-tagged (label escaped).
func LabeledBar(label string, fraction float64, width int, barColor, textColor, outsideColor string) string {
	if width <= 0 {
		return ""
	}
	eighths := 0
	if fraction > 0 {
		if fraction > 1 {
			fraction = 1
		}
		eighths = int(fraction*float64(width)*8 + 0.5)
		if eighths < 1 {
			eighths = 1
		}
	}
	full, partial := eighths/8, eighths%8

	runes := []rune(label)
	if len(runes) > width-1 { // room for the leading bar cell
		if width-1 < 1 {
			runes = nil
		} else {
			runes = append(runes[:width-2], '…')
		}
	}
	inner := append([]rune{' '}, runes...) // the leading bar cell, then the label

	var b strings.Builder
	used := 0
	if full > 0 {
		inside := make([]rune, full)
		for i := range inside {
			inside[i] = ' '
			if i < len(inner) {
				inside[i] = inner[i]
			}
		}
		fmt.Fprintf(&b, "[%s:%s]%s[-:-]", textColor, barColor, tview.Escape(string(inside)))
		used = full
	}
	switch {
	case full == 0 && partial > 0:
		// A bar under one cell: its sliver, then the label as it falls.
		fmt.Fprintf(&b, "[%s]%c[-]", barColor, eighthBlocks[partial])
		used = 1
		if len(runes) > 0 {
			fmt.Fprintf(&b, "[%s]%s[-]", outsideColor, tview.Escape(string(runes)))
			used += len(runes)
		}
	case len(inner) > full:
		rest := string(inner[full:])
		fmt.Fprintf(&b, "[%s]%s[-]", outsideColor, tview.Escape(rest))
		used += len(inner) - full
	case partial > 0:
		fmt.Fprintf(&b, "[%s]%c[-]", barColor, eighthBlocks[partial])
		used++
	}
	if used < width {
		b.WriteString(strings.Repeat(" ", width-used))
	}
	return b.String()
}
