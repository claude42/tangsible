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

// Stage 4 of debug mode (design-docs/DebugMode.md's "UI details variable
// explorer"/"Stage 4 UX decisions" sections): evaluates a user-typed
// watch expression - a bare variable name, or a dotted/bracket-index path
// into one (foo.bar, foo[0], foo['key']) - against a captured task_vars
// snapshot. Deliberately NOT full Jinja (no filters/operators/calls - an
// explicit upstream constraint, not an oversight) and deliberately not
// resolve_condition.go's existing jinja2-backed mechanism: that one's
// "always echo back unchanged on any failure" contract can't distinguish
// "doesn't parse" from "doesn't resolve" (both produce output == input),
// and a partially-resolving chain produces a hybrid literal-plus-leftover-
// accessor string, not a clean signal - this package needs a clean 3-way
// reject/unavailable/resolved split instead, which is simpler to get by
// hand-walking the tiny grammar below than by reshaping that mechanism.
// Pure and tview-free, same "testable in isolation" shape as
// uikit.FilteredMatch.
package session

import (
	"encoding/json"
	"strconv"
)

type watchAccessorKind int

const (
	watchAccessorField watchAccessorKind = iota // .name
	watchAccessorIndex                          // [123] / [-1]
	watchAccessorKey                            // ['name'] / ["name"]
)

type watchAccessor struct {
	kind  watchAccessorKind
	name  string
	index int
}

type watchExpr struct {
	root      string
	accessors []watchAccessor
}

// parseWatchExpr parses expr against the grammar `IDENT ('.' IDENT | '['
// INDEX ']' | '[' STRING ']')*` - IDENT = [A-Za-z_][A-Za-z0-9_]*, INDEX =
// '-'? [0-9]+, STRING = a '-or-"-quoted run with no escapes (not needed
// for variable/key names). No whitespace is accepted anywhere. Returns
// false for anything that doesn't match this exactly - including a
// genuine Jinja expression (filters, operators, calls), which is outside
// this grammar entirely, not merely "doesn't resolve."
func parseWatchExpr(expr string) (watchExpr, bool) {
	r := []rune(expr)
	i := 0
	name, ni, ok := scanIdent(r, i)
	if !ok {
		return watchExpr{}, false
	}
	i = ni
	ast := watchExpr{root: name}
	for i < len(r) {
		switch r[i] {
		case '.':
			i++
			name, ni, ok := scanIdent(r, i)
			if !ok {
				return watchExpr{}, false
			}
			i = ni
			ast.accessors = append(ast.accessors, watchAccessor{kind: watchAccessorField, name: name})
		case '[':
			i++
			if i >= len(r) {
				return watchExpr{}, false
			}
			switch {
			case r[i] == '-' || (r[i] >= '0' && r[i] <= '9'):
				start := i
				if r[i] == '-' {
					i++
				}
				digitsStart := i
				for i < len(r) && r[i] >= '0' && r[i] <= '9' {
					i++
				}
				if i == digitsStart {
					return watchExpr{}, false // "-" alone
				}
				if i >= len(r) || r[i] != ']' {
					return watchExpr{}, false
				}
				n, err := strconv.Atoi(string(r[start:i]))
				if err != nil {
					return watchExpr{}, false
				}
				i++
				ast.accessors = append(ast.accessors, watchAccessor{kind: watchAccessorIndex, index: n})
			case r[i] == '\'' || r[i] == '"':
				quote := r[i]
				i++
				start := i
				for i < len(r) && r[i] != quote {
					i++
				}
				if i >= len(r) {
					return watchExpr{}, false // unterminated quote
				}
				key := string(r[start:i])
				i++ // closing quote
				if i >= len(r) || r[i] != ']' {
					return watchExpr{}, false
				}
				i++
				ast.accessors = append(ast.accessors, watchAccessor{kind: watchAccessorKey, name: key})
			default:
				return watchExpr{}, false
			}
		default:
			return watchExpr{}, false // whitespace, operators, anything else
		}
	}
	return ast, true
}

func scanIdent(r []rune, i int) (string, int, bool) {
	start := i
	if i >= len(r) || !isIdentStart(r[i]) {
		return "", i, false
	}
	i++
	for i < len(r) && isIdentCont(r[i]) {
		i++
	}
	return string(r[start:i]), i, true
}

func isIdentStart(c rune) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentCont(c rune) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
}

type watchResultKind int

const (
	watchRejected    watchResultKind = iota // bad syntax
	watchUnavailable                        // valid syntax, doesn't resolve, or a non-scalar (v1 scope)
	watchResolved
)

type watchResult struct {
	kind  watchResultKind
	value any // bool | string | float64 | nil; only meaningful when kind == watchResolved
}

// evaluateWatchExpr parses expr and, if valid, walks snapshot (a flat
// top-level JSON object - debugTaskVarsCache's own shape) to resolve it.
// snapshot == nil (no captured snapshot for the current host/task) is
// "unavailable," the same as a snapshot that exists but doesn't contain
// the expression's own root name - both collapse to the identical
// rendering per design-docs/DebugMode.md's Stage 4 decision.
func evaluateWatchExpr(expr string, snapshot json.RawMessage) watchResult {
	ast, ok := parseWatchExpr(expr)
	if !ok {
		return watchResult{kind: watchRejected}
	}
	if len(snapshot) == 0 {
		return watchResult{kind: watchUnavailable}
	}
	var root map[string]any
	if json.Unmarshal(snapshot, &root) != nil {
		return watchResult{kind: watchUnavailable}
	}
	val, ok := root[ast.root]
	if !ok {
		return watchResult{kind: watchUnavailable}
	}
	for _, acc := range ast.accessors {
		val, ok = applyWatchAccessor(val, acc)
		if !ok {
			return watchResult{kind: watchUnavailable}
		}
	}
	if !isWatchScalar(val) {
		return watchResult{kind: watchUnavailable} // v1 scope: dicts/lists aren't rendered yet
	}
	return watchResult{kind: watchResolved, value: val}
}

func applyWatchAccessor(val any, acc watchAccessor) (any, bool) {
	switch acc.kind {
	case watchAccessorField, watchAccessorKey:
		m, ok := val.(map[string]any)
		if !ok {
			return nil, false
		}
		v, ok := m[acc.name]
		return v, ok
	case watchAccessorIndex:
		arr, ok := val.([]any)
		if !ok {
			return nil, false
		}
		idx := acc.index
		if idx < 0 {
			idx += len(arr)
		}
		if idx < 0 || idx >= len(arr) {
			return nil, false
		}
		return arr[idx], true
	}
	return nil, false
}

func isWatchScalar(v any) bool {
	switch v.(type) {
	case nil, bool, string, float64:
		return true
	}
	return false
}

// formatWatchScalar renders a resolved scalar the way the UX mockup shows
// it (design-docs/DebugMode.md): a quoted string, lowercase true/false,
// "null", or a plain number - not Go's own %v spelling for any of these.
func formatWatchScalar(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case bool:
		if x {
			return "true"
		}
		return "false"
	case string:
		return strconv.Quote(x)
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'g', -1, 64)
	}
	return ""
}
