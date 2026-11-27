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

package variables

import (
	"fmt"
	"strings"

	"github.com/rivo/tview"

	"code.aw.net/claude/tangsible/internal/uikit"
	"code.aw.net/claude/tangsible/internal/varscrawl"
)

// seg is a piece of a row: plain text and the color it is drawn in.
type seg struct {
	text  string
	color string
}

// styled draws segs normally, or - for the row under the cursor - as one run
// in the selected-row style (the tree's own convention).
func styled(selected bool, segs ...seg) string {
	var b strings.Builder
	if selected {
		var plain strings.Builder
		for _, s := range segs {
			plain.WriteString(s.text)
		}
		fmt.Fprintf(&b, "[%s:%s:b]%s[-:-:-]", uikit.SelectedRowTextColor, uikit.SelectedRowBackgroundColor, tview.Escape(plain.String()))
		return b.String()
	}
	for _, s := range segs {
		switch {
		case s.text == "":
		case s.color == "":
			b.WriteString(tview.Escape(s.text))
		default:
			fmt.Fprintf(&b, "[%s]%s[-]", s.color, tview.Escape(s.text))
		}
	}
	return b.String()
}

// fit cuts s to width runes ("…" when cut) and pads it with spaces to width.
func fit(s string, width int) string {
	r := []rune(s)
	if len(r) > width {
		if width < 1 {
			return ""
		}
		r = append(r[:width-1], '…')
	}
	return string(r) + strings.Repeat(" ", width-len(r))
}

// columns are the widths that line the rows up, worked out from the whole
// list so they do not jump when a row is expanded.
type columns struct {
	name, level, loc, total int
}

const (
	maxNameWidth  = 32
	maxLevelWidth = 38
	maxLocWidth   = 46
	minValueWidth = 8
)

func measure(vars []varscrawl.Variable, base string, total int) columns {
	c := columns{total: total}
	for _, v := range vars {
		n := len([]rune(v.Name))
		if v.Disagree {
			n += 2
		}
		if n > c.name {
			c.name = n
		}
		for _, d := range v.Defs {
			if l := len([]rune(levelLabel(d))); l > c.level {
				c.level = l
			}
			if l := len([]rune(locationLabel(d, base))); l > c.loc {
				c.loc = l
			}
		}
	}
	c.name = min(c.name, maxNameWidth)
	c.level = min(c.level, maxLevelWidth)
	c.loc = min(c.loc, maxLocWidth)
	return c
}

// variableRow: "▸ name  value  where".
func variableRow(v varscrawl.Variable, open, selected bool, c columns, base string) string {
	marker := "▸ "
	if open {
		marker = "▾ "
	}
	name := v.Name
	if v.Disagree {
		name += " ?"
	}
	val := ""
	where := ""
	if len(v.Defs) > 0 {
		val = valueSummary(v.Defs[0].Value)
		where = locationLabel(v.Defs[0], base)
	}
	valueW := max(minValueWidth, c.total-2-c.name-2-c.loc-2-1)
	return styled(selected,
		seg{marker, uikit.AnnotationColor},
		seg{fit(name, c.name), uikit.KeyHighlightColor},
		seg{"  ", ""},
		seg{fit(val, valueW), uikit.ValueHighlightColor},
		seg{"  ", ""},
		seg{fit(where, c.loc), uikit.AnnotationColor},
	)
}

// definitionRow: "    ● value  level (entity)  where" - the first, winning
// one marked with a filled dot.
func definitionRow(d varscrawl.Definition, winner, selected bool, c columns, base string) string {
	dot := "· "
	if winner {
		dot = "● "
	}
	valueW := max(minValueWidth, c.total-4-2-c.level-2-c.loc-2-1)
	return styled(selected,
		seg{"    ", ""},
		seg{dot, uikit.AnnotationColor},
		seg{fit(valueSummary(d.Value), valueW), uikit.ValueHighlightColor},
		seg{"  ", ""},
		seg{fit(levelLabel(d), c.level), uikit.StandardTextColor},
		seg{"  ", ""},
		seg{fit(locationLabel(d, base), c.loc), uikit.AnnotationColor},
	)
}

// infoRow is a line of explanation under a variable or at the end.
func infoRow(text string, selected bool) string {
	return styled(selected, seg{"    " + text, uikit.AnnotationColor})
}
