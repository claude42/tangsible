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
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"code.aw.net/claude/tangsible/internal/origin"
	"code.aw.net/claude/tangsible/internal/varscrawl"
)

// The pure half of the screen (design-docs/Variables.md): what a row says and
// which rows there are. Nothing here touches tview.

type rowKind int

const (
	rowVariable   rowKind = iota // a variable, with its winning definition
	rowDefinition                // one definition of the variable above it
	rowInfo                      // a line of explanation, not selectable in effect
)

// rowRef points at what a row shows: variable and definition indexes into the
// host's Result, or (rowInfo) just a text.
type rowRef struct {
	kind     rowKind
	variable int
	def      int
	text     string
}

// flatten lists the rows of the tree: every variable, and below an expanded
// one its definitions highest precedence first (with a line saying so when
// ansible's own answer differed from the crawl's order).
func flatten(vars []varscrawl.Variable, expanded map[string]bool) []rowRef {
	var rows []rowRef
	for i, v := range vars {
		rows = append(rows, rowRef{kind: rowVariable, variable: i})
		if !expanded[v.Name] {
			continue
		}
		if v.Disagree {
			rows = append(rows, rowRef{kind: rowInfo, variable: i, text: disagreementText})
		}
		for j := range v.Defs {
			rows = append(rows, rowRef{kind: rowDefinition, variable: i, def: j})
		}
	}
	return rows
}

const disagreementText = "ansible's own answer is the first definition; the order below it is our reconstruction and may differ"

// valueSummary is a value on one line: a string without its quotes, anything
// else as compact JSON, only the first line of a multi-line string (cut with
// "…" so it is clear there is more).
func valueSummary(raw json.RawMessage) string {
	var v any
	if err := json.Unmarshal(raw, &v); err == nil {
		if str, ok := v.(string); ok {
			first, rest, more := strings.Cut(str, "\n")
			if more && strings.TrimSpace(rest) != "" {
				return first + " …"
			}
			return first
		}
	}
	var b bytes.Buffer
	if err := json.Compact(&b, raw); err != nil {
		return string(raw)
	}
	return b.String()
}

// levelLabel says where in ansible's order a definition sits: the level and
// the group, host or role it belongs to.
func levelLabel(d varscrawl.Definition) string {
	switch {
	case d.FromAnsible:
		return d.Level
	case d.Entity == "" || d.Level == "extra vars":
		return d.Level
	}
	return fmt.Sprintf("%s (%s)", d.Level, d.Entity)
}

// locationLabel is the project-relative file:line of a definition, or what
// stands in for one (-e).
func locationLabel(d varscrawl.Definition, base string) string {
	return origin.Label(origin.Origin{Path: d.Path, Line: d.Line, Desc: d.Desc}, "", "", base)
}

// plainListing is the variables as plain text, for copying: one line per
// variable with its winning value, then its definitions in order.
func plainListing(vars []varscrawl.Variable, base string) string {
	var b strings.Builder
	for _, v := range vars {
		if len(v.Defs) == 0 {
			continue
		}
		fmt.Fprintf(&b, "%s: %s  # %s\n", v.Name, valueSummary(v.Defs[0].Value), locationLabel(v.Defs[0], base))
		if len(v.Defs) > 1 {
			for _, d := range v.Defs {
				fmt.Fprintf(&b, "    %s: %s  # %s\n", levelLabel(d), valueSummary(d.Value), locationLabel(d, base))
			}
		}
	}
	return b.String()
}
