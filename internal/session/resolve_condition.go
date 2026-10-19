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

// Resolves the variable references within a task's when:/failed_when:/
// changed_when:/until: conditions, for the debug-mode Resolved tab
// (design-docs/DebugMode.md). These are plain Jinja boolean expressions,
// never {{ }}-wrapped - resolveTaskValues' own {{ }}-regex mechanism
// (resolved.go's wrapJinjaDefaults) never touches them, and re-running
// the *whole* condition through ansible's own templating would only ever
// collapse it to a flat true/false, discarding exactly the "which
// operand was which" detail worth showing. resolve_condition.py (go:
// embed'd below, run as `python3 -c <script>`) parses the real Jinja
// grammar via the jinja2 library ansible itself depends on, substitutes
// only the variable-reference subtrees that fully resolve against the
// task's own captured snapshot, and renders the result back to text -
// everything else (operators, filters, tests, literals) passes through
// unchanged. A condition this can't resolve - a parse error, an
// ansible-specific Jinja extension, a reference nothing in the snapshot
// satisfies (a register-captured result, a loop item) - is left exactly
// as written, the same silent-fallback contract the rest of this file's
// package already applies to an unresolvable {{ }} expression.
package session

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"os/exec"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed resolve_condition.py
var resolveConditionScript string

// conditionKeys are the task-level keys ansible evaluates as bare Jinja
// boolean expressions (confirmed against ansible-core's own TaskExecutor/
// PlayIterator: when/until gate execution, failed_when/changed_when
// reinterpret a completed result) - the only keys this file resolves.
var conditionKeys = []string{"when", "failed_when", "changed_when", "until"}

// taskCondition is one conditionKeys entry found in a task's raw source,
// with enough to both resolve its expression(s) and splice the result
// back into that same source text unchanged elsewhere.
type taskCondition struct {
	key   string
	block string // exact original text span for this key, incl. a list form's own "- " lines
	exprs []string

	keyIndent  int // spaces before key, for a scalar "key: expr" line
	isList     bool
	itemIndent int // spaces before "- " on a list form's own item lines
}

// extractTaskConditions parses taskSource (a task's own raw YAML source,
// exactly as sliced by internal/source - leading "- " list marker and
// all) and returns every when:/failed_when:/changed_when:/until: key it
// finds at the task's own top level, each with its exact original text
// span (for splicing) and its expression(s) (for resolving). A condition
// is a plain scalar ("when: x != 0") or a block list ("when:\n  - a\n  -
// b", ansible ANDs the items) - any other shape (a flow list, a folded/
// literal block scalar) is silently skipped, same "regex/parser
// heuristic, not chased to 100%" tolerance resolved.go's own
// jinjaExpressionPattern already documents. A taskSource that doesn't
// parse as YAML at all (shouldn't happen - internal/source only ever
// slices real file text) returns nil, same as "nothing to resolve."
func extractTaskConditions(taskSource string) []taskCondition {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(taskSource), &doc); err != nil {
		return nil
	}
	root := &doc
	if root.Kind == yaml.DocumentNode && len(root.Content) == 1 {
		root = root.Content[0]
	}
	if root.Kind == yaml.SequenceNode && len(root.Content) == 1 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return nil
	}

	lines := strings.Split(taskSource, "\n")
	var conds []taskCondition
	for i := 0; i+1 < len(root.Content); i += 2 {
		key := root.Content[i]
		val := root.Content[i+1]
		if !slices.Contains(conditionKeys, key.Value) {
			continue
		}
		// endLine is derived strictly from the value's own extent, never
		// from "the next sibling key, or else end of taskSource" - a
		// condition key with no sibling after it (the common case: when:/
		// failed_when:/etc. is usually the task's last key) would
		// otherwise swallow everything through end of taskSource into
		// block, including unrelated trailing comments. Confirmed live as
		// a real bug: a trailing commented-out task containing its own
		// {{ some_var }} gets that substituted by the unrelated {{ }}-
		// regex resolve pass elsewhere in resolveTaskValues, so block (cut
		// from the untouched original) no longer matches anywhere in the
		// already-rendered text - the splice below silently finds nothing,
		// leaving the condition completely unresolved even though its own
		// variables were perfectly resolvable.
		startLine := key.Line
		endLine := startLine + 1
		switch val.Kind {
		case yaml.ScalarNode:
			if (val.Style & (yaml.LiteralStyle | yaml.FoldedStyle)) != 0 {
				// A block scalar's value spans lines past val.Line with no
				// end position exposed by yaml.Node - not safe to bound,
				// so left alone entirely (same "not chased further"
				// tolerance as a flow list, below).
				continue
			}
			if val.Line+1 > endLine {
				endLine = val.Line + 1
			}
		case yaml.SequenceNode:
			if len(val.Content) > 0 {
				endLine = val.Content[len(val.Content)-1].Line + 1
			}
		}
		if startLine < 1 || startLine > len(lines) || endLine > len(lines)+1 {
			continue
		}
		block := strings.TrimRight(strings.Join(lines[startLine-1:endLine-1], "\n"), " \t\n")

		cond := taskCondition{key: key.Value, block: block, keyIndent: key.Column - 1}
		switch val.Kind {
		case yaml.ScalarNode:
			cond.exprs = []string{val.Value}
		case yaml.SequenceNode:
			cond.isList = true
			if val.Column > 0 {
				// val.Column is the column of the block sequence's own
				// "-" marker (confirmed live: yaml.v3 gives the
				// SequenceNode itself this column, not its first item's
				// content column, which would be two past the dash).
				cond.itemIndent = val.Column - 1
			} else {
				cond.itemIndent = cond.keyIndent + 2
			}
			for _, item := range val.Content {
				if item.Kind == yaml.ScalarNode {
					cond.exprs = append(cond.exprs, item.Value)
				}
			}
		}
		if len(cond.exprs) == 0 {
			continue
		}
		conds = append(conds, cond)
	}
	return conds
}

// rebuildConditionBlock renders c's key back out with each of its
// expressions replaced by resolved[i], preserving c's own original
// shape (scalar vs. list) and indentation.
func rebuildConditionBlock(c taskCondition, resolved []string) string {
	indent := strings.Repeat(" ", c.keyIndent)
	if !c.isList {
		return fmt.Sprintf("%s%s: %s", indent, c.key, resolved[0])
	}
	itemIndent := strings.Repeat(" ", c.itemIndent)
	lines := make([]string, 0, len(resolved)+1)
	lines = append(lines, indent+c.key+":")
	for _, e := range resolved {
		lines = append(lines, itemIndent+"- "+e)
	}
	return strings.Join(lines, "\n")
}

// resolveConditionExprs resolves exprs (in order) against snapshot's own
// task_vars (debugTaskVarsCache's own captured JSON object, already the
// shape resolve_condition.py's "vars" expects - debug mode's only
// caller) by shelling out to the embedded Python script. Any failure
// (python3 missing, a malformed snapshot) returns an error; the caller
// treats that as "leave every condition exactly as written," never as a
// reason to fail the whole Resolved tab.
func resolveConditionExprs(snapshot json.RawMessage, exprs []string) ([]string, error) {
	varsmap := snapshot
	if len(varsmap) == 0 {
		varsmap = json.RawMessage("{}")
	}
	reqJSON, err := json.Marshal(struct {
		Vars  json.RawMessage `json:"vars"`
		Exprs []string        `json:"exprs"`
	}{Vars: varsmap, Exprs: exprs})
	if err != nil {
		return nil, err
	}

	cmd := exec.Command("python3", "-c", resolveConditionScript)
	cmd.Stdin = bytes.NewReader(reqJSON)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("%s", msg)
		}
		return nil, err
	}

	var out []string
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		return nil, err
	}
	if len(out) != len(exprs) {
		return nil, fmt.Errorf("resolve_condition.py returned %d results for %d expressions", len(out), len(exprs))
	}
	return out, nil
}

// resolveConditionsInText finds every when:/failed_when:/changed_when:/
// until: key in taskSource, resolves its expression(s) against
// snapshot, and splices each resolved key back into text (the module-
// args-resolved render resolveTaskValues already produced) in place of
// its original, unresolved text - which text carries forward unchanged
// since Jinja preserves literal text outside {{ }}/{% %} verbatim, so a
// condition key's own span is guaranteed byte-identical between
// taskSource and text (the one exception - a condition whose own text
// happens to contain a literal {{ }} - just silently fails to find its
// span in text and is left alone, the same "not chased further" gap
// wrapJinjaDefaults' own doc comment already accepts for that shape).
// Never returns an error: anything that goes wrong here (no conditions
// present, python3 missing, an unresolvable expression) just means text
// comes back with that much less resolved, never a reason to fail the
// render resolveTaskValues already produced.
func resolveConditionsInText(text, taskSource string, snapshot json.RawMessage) string {
	conds := extractTaskConditions(taskSource)
	if len(conds) == 0 {
		return text
	}

	var allExprs []string
	for _, c := range conds {
		allExprs = append(allExprs, c.exprs...)
	}
	resolved, err := resolveConditionExprs(snapshot, allExprs)
	if err != nil {
		return text
	}

	pos := 0
	for _, c := range conds {
		n := len(c.exprs)
		block := rebuildConditionBlock(c, resolved[pos:pos+n])
		pos += n
		if block != c.block {
			text = strings.Replace(text, c.block, block, 1)
		}
	}
	return text
}
