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
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"code.aw.net/claude/tangsible/internal/playbook"
	"github.com/pmezard/go-difflib/difflib"
	"github.com/rivo/tview"
)

// PrimaryOutputField picks a single field to represent a host's textual
// output, preferring "stdout" (the actual command output, for
// command/shell-style modules) over the more generic "msg" field - on a
// command/shell task with a non-zero return code, msg is often just a
// fixed status string ("non-zero return code") alongside the real output
// already sitting in stdout. Falls back to msg for modules that only set
// that field (most non-command modules, e.g. debug/fail/assert) - msg
// need not be a plain string (ansible.builtin.debug's own msg: parameter
// can be given as a YAML list, confirmed empirically: `msg: [a, b]`
// arrives as a JSON array, not a string), so debugValueText handles
// whatever shape it turns out to be rather than a strict .(string)
// assertion silently discarding anything else. Falls back further still,
// for ansible.builtin.debug specifically, to debugVarValue - the var:
// form of that module has no "msg" key at all, only a key named after
// whatever variable/expression was given. Shared by formatHostOutput (the
// full drill-down view) and outputSummary (the collapsed treeview OK
// line) so both agree on what "the output" means for a given result.
func PrimaryOutputField(decoded map[string]interface{}) (label, text string) {
	if stdout, ok := decoded["stdout"].(string); ok && stdout != "" {
		return "STDOUT", stdout
	}
	if msg, ok := decoded["msg"]; ok {
		if text := DebugValueText(msg); text != "" {
			return "MSG", text
		}
	}
	if ModuleShortName(decoded) == "debug" {
		if text, ok := DebugVarValue(decoded); ok {
			return "MSG", text
		}
	}
	return "", ""
}

// DebugValueText renders a value from a debug task's own result (its
// "msg", or its var:-named key via debugVarValue below) as display text:
// a plain string as-is; a JSON array of strings newline-joined (matching
// debug's own `msg: [a, b]` shape, reusing joinedStringList); anything
// else (a dict, a list of non-strings, a number, a bool) as
// pretty-printed JSON - always something readable rather than silently
// nothing, same "always show *something*" fallback style as
// formatHostOutput's own decode-failure path.
func DebugValueText(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	if joined := JoinedStringList(v, "\n"); joined != "" {
		return joined
	}
	pretty, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return ""
	}
	return string(pretty)
}

// DebugStandardKeys are the fields an ansible.builtin.debug result always
// or commonly carries that are never the var: value itself - used by
// debugVarValue below to isolate the one remaining key that is.
var DebugStandardKeys = map[string]bool{
	"changed": true, "failed": true, "skipped": true, "unreachable": true,
	"action": true, "msg": true, "invocation": true, "warnings": true,
	"deprecations": true, "exception": true, "results": true,
	"item": true, "ansible_loop_var": true,
}

// DebugVarValue implements ansible.builtin.debug's var: form: unlike msg:,
// there's no fixed key to look up - the result's own key is named after
// whatever variable or expression var: was given (confirmed empirically:
// `var: some_list` reports a top-level "some_list" key; `var: outer.inner`
// reports a literal "outer.inner" key), which this file has no way to
// know in advance. debug's own documentation states var: and msg: are
// mutually exclusive, so - after excluding debugStandardKeys and any
// "_ansible_*" bookkeeping key - a debug result has either zero extra
// keys (an msg: task, or nothing usable) or exactly one (the var: task's
// own value, whatever it's named): ok is true only in that one
// unambiguous case, never guessing among several candidates.
func DebugVarValue(decoded map[string]interface{}) (text string, ok bool) {
	var found interface{}
	count := 0
	for k, v := range decoded {
		if DebugStandardKeys[k] || strings.HasPrefix(k, "_ansible_") {
			continue
		}
		found = v
		count++
	}
	if count != 1 {
		return "", false
	}
	return DebugValueText(found), true
}

// ModuleShortName returns decoded["action"] with any collection prefix
// stripped ("ansible.builtin.copy" and the plain "copy" both become
// "copy") - a task written with its fully-qualified name still reports the
// FQCN in "action" (confirmed empirically: a task using
// ansible.builtin.copy: reports action "ansible.builtin.copy", not
// "copy"), so additionalOutputLines' module matching below needs this
// normalization to recognize both spellings of the same module.
func ModuleShortName(decoded map[string]interface{}) string {
	action, _ := decoded["action"].(string)
	if idx := strings.LastIndex(action, "."); idx != -1 {
		return action[idx+1:]
	}
	return action
}

// JoinedStringList formats a decoded JSON value that might be a single
// string or a JSON array of strings (e.g. apt_repository's own
// "sources_added", or a command task's "cmd") as one sep-joined string -
// "" if v is neither shape, or an array with no string elements. Non-
// string array elements are silently skipped rather than erroring, same
// "don't trust live external jsonl blindly" caveat as this file's other
// decoders.
func JoinedStringList(v interface{}, sep string) string {
	switch vv := v.(type) {
	case string:
		return vv
	case []interface{}:
		parts := make([]string, 0, len(vv))
		for _, e := range vv {
			if s, ok := e.(string); ok && s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, sep)
	}
	return ""
}

// FilenameField backs additionalOutputLines' copy/file/stat/template/
// assemble/git case: design-docs/Drilldown, Task List.md's
// "Filename: <dest>" or "Filename: <path>", "depending on which field
// exists in the results". Checked top-level "dest" then "path" first
// (covers copy/template/assemble, which always report "dest", and file,
// which reports "dest" or "path" depending on the state: used - both
// confirmed empirically); stat and git are the exceptions - confirmed
// empirically neither reports "dest"/"path" at the top level, only nested
// under invocation.module_args (which every module echoes back verbatim,
// unresolved further) - so that's checked next, in the same
// dest-then-path order, before giving up.
func FilenameField(decoded map[string]interface{}) string {
	if v, ok := decoded["dest"].(string); ok && v != "" {
		return v
	}
	if v, ok := decoded["path"].(string); ok && v != "" {
		return v
	}
	if inv, ok := decoded["invocation"].(map[string]interface{}); ok {
		if args, ok := inv["module_args"].(map[string]interface{}); ok {
			if v, ok := args["dest"].(string); ok && v != "" {
				return v
			}
			if v, ok := args["path"].(string); ok && v != "" {
				return v
			}
		}
	}
	return ""
}

// AdditionalOutputLines implements design-docs/Drilldown, Task List.md's
// per-module special cases beyond debug (which already gets what it asks
// for as a side effect of primaryOutputField's plain msg fallback - no
// special-casing needed there): copy/file/stat/template/assemble/git get
// a "Filename: <dest-or-path>" line; command/shell get a "Command: <cmd>"
// line (a command task's own "cmd" is a JSON array of parsed argv tokens,
// a shell task's is a single raw string - confirmed empirically for both -
// joinedStringList handles either shape); apt_repository gets a
// "Filename: <sources_added>" line ("sources_added" is itself a list of
// paths per ansible-core's own module docs, only ever populated when a
// source was actually added); user gets both a "User: <name>" and, only
// when present (generate_ssh_key: true), a "SSH public key: <...>" line.
// nil for every other module, and for any of the above whose expected
// field is missing/empty - most tasks show no extra line at all, same as
// before this existed. Returns potentially more than one line (only ever
// true for user) rather than one combined string, so callers can lay
// multiple lines out differently: formatHostOutput's Output section wants
// them one per line, outputSummary's parenthetical wants them
// comma-joined into a single semicolon-separated part alongside the
// primary output summary. Shared by both, the same sharing relationship
// primaryOutputField already has with both.
func AdditionalOutputLines(decoded map[string]interface{}) []string {
	switch ModuleShortName(decoded) {
	case "copy", "file", "stat", "template", "assemble", "git":
		if fn := FilenameField(decoded); fn != "" {
			return []string{"Filename: " + fn}
		}
	case "command", "shell":
		if cmd := JoinedStringList(decoded["cmd"], " "); cmd != "" {
			return []string{"Command: " + cmd}
		}
	case "apt_repository":
		if fn := JoinedStringList(decoded["sources_added"], ", "); fn != "" {
			return []string{"Filename: " + fn}
		}
	case "user":
		var lines []string
		if name, ok := decoded["name"].(string); ok && name != "" {
			lines = append(lines, "User: "+name)
		}
		if key, ok := decoded["ssh_public_key"].(string); ok && key != "" {
			lines = append(lines, "SSH public key: "+key)
		}
		return lines
	}
	return nil
}

// OutputSummary returns the parenthesized detail hostLabel appends after
// "OK"/"Changed"/"Failed" - the single line of output verbatim if
// primaryOutputField's chosen text is exactly one line, or its line count
// otherwise, plus additionalOutputLines' own extra line(s) - comma-joined
// into one part - when any apply, semicolon-joined against the primary
// summary when both are present. "" (nothing appended) if neither yields
// anything, e.g. a module like template with nothing changed and no
// filename fields set (shouldn't happen for a real template result, but
// not trusted blindly - same caveat as formatHostOutput's own decode
// below).
func OutputSummary(raw json.RawMessage) string {
	var decoded map[string]interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return ""
	}
	_, text := PrimaryOutputField(decoded)
	text = strings.TrimRight(text, "\n")

	var parts []string
	if text != "" {
		lines := strings.Split(text, "\n")
		if len(lines) == 1 {
			parts = append(parts, lines[0])
		} else {
			parts = append(parts, fmt.Sprintf("%d lines of output", len(lines)))
		}
	}
	if extra := AdditionalOutputLines(decoded); len(extra) > 0 {
		parts = append(parts, strings.Join(extra, ", "))
	}
	if len(parts) == 0 {
		return ""
	}
	return fmt.Sprintf(" (%s)", strings.Join(parts, "; "))
}

// SkipDetail returns the parenthesized "(skip_reason: false_condition)"
// detail hostLabel appends after "Skipped", pulled straight from the
// task's own recorded result for that host - "" if skip_reason wasn't
// present (shouldn't happen for a real v2_runner_on_skipped event, but
// this is live external jsonl, not trusted blindly - same caveat as
// formatHostOutput's own decode below).
func SkipDetail(raw json.RawMessage) string {
	var decoded map[string]interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return ""
	}
	reason, _ := decoded["skip_reason"].(string)
	if reason == "" {
		return ""
	}
	if cond, ok := decoded["false_condition"].(string); ok && cond != "" {
		return fmt.Sprintf(" (%s: %s)", reason, cond)
	}
	return fmt.Sprintf(" (%s)", reason)
}

// SkipOutputText builds formatHostOutput's Output section text for a
// Skipped host: "<skip_reason>: <false_condition>", or just <skip_reason>
// alone when false_condition isn't a plain string (e.g. a literal
// `when: false` serializes it as JSON false, not a string) - same
// underlying fields, same "reason: condition" phrasing, and the same
// string-or-fall-back caveat as skipDetail above (the tree row's own
// rendering of this same data), just without skipDetail's own wrapping
// parentheses, since this is the Output section's own standalone text,
// not something appended after another word. Takes the already-decoded
// result (unlike skipDetail, which decodes raw bytes itself -
// formatHostOutput already has decoded in scope, so there's no reason to
// decode twice).
func SkipOutputText(decoded map[string]interface{}) string {
	reason, _ := decoded["skip_reason"].(string)
	if reason == "" {
		return ""
	}
	if cond, ok := decoded["false_condition"].(string); ok && cond != "" {
		return fmt.Sprintf("%s: %s", reason, cond)
	}
	return reason
}

// LoopItemDetail is one element of a looped task's own "results" array -
// see loopItemDetails.
type LoopItemDetail struct {
	Label string
	// Msg is this item's own "msg" field, distinct from the task-level
	// "msg" formatHostOutput already shows via primaryOutputField (which,
	// for a failed loop, is Ansible's own generic "One or more items
	// failed" - never the reason any one item actually failed). Empty
	// when the item carries no "msg" of its own, which is the common case
	// for an OK/Changed item that never had anything to say.
	Msg string
}

// LoopItemDetails returns one label+msg pair per element of a looped
// task's own "results" array (present only when the task used
// loop:/with_*, confirmed empirically against a real loop's
// v2_runner_on_ok event - absent entirely for a non-looped task, so a
// task without it simply gets no Items section, per formatHostOutput's
// usual "omit rather than show empty" convention). Each item's own
// "_ansible_item_label" is what Ansible itself uses for display (e.g.
// "changed: [host] => (item=foo)") - used directly when it's a plain
// string (the common case: looping over a list of strings/numbers); for a
// loop over dicts/lists, that label is itself the raw structure rather
// than a string, so it's rendered as compact JSON instead, same "always
// show *something* readable" fallback style as formatHostOutput's own
// decode-failure path.
func LoopItemDetails(decoded map[string]interface{}) []LoopItemDetail {
	results, ok := decoded["results"].([]interface{})
	if !ok {
		return nil
	}
	details := make([]LoopItemDetail, 0, len(results))
	for _, r := range results {
		item, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		label, ok := item["_ansible_item_label"]
		if !ok {
			label = item["item"]
		}
		var labelText string
		if s, ok := label.(string); ok {
			labelText = s
		} else if b, err := json.Marshal(label); err == nil {
			labelText = string(b)
		}
		msg, _ := item["msg"].(string)
		details = append(details, LoopItemDetail{Label: labelText, Msg: msg})
	}
	return details
}

// LoopItemLabels is loopItemDetails' own labels, for callers (and
// existing tests) that only ever cared about item identity, not any
// per-item message.
func LoopItemLabels(decoded map[string]interface{}) []string {
	details := LoopItemDetails(decoded)
	if details == nil {
		return nil
	}
	labels := make([]string, 0, len(details))
	for _, d := range details {
		labels = append(labels, d.Label)
	}
	return labels
}

// sectionLabel renders one of formatHostOutput's section headers in its
// own color, bold - a distinct color per section (deliberately outside the
// outcome palette in colorTag, so a reader never confuses "this section is
// STDERR" with "this host's outcome is Failed") makes the view scannable
// at a glance rather than a wall of uniform text. label is a fixed literal
// from formatHostOutput itself, never external content, so it needs no
// escaping.
// YamlKeyLine matches a line's "key:" shape: optional leading indentation
// and a "- " list marker (both preserved verbatim, uncolored - group 1),
// a plain-scalar key (group 2, letters/digits/underscore/dot/hyphen -
// covers ordinary task fields like "name"/"when" as well as FQCN module
// names like "ansible.builtin.debug"), the colon itself (group 3), and
// whatever follows - either more content after a space, or nothing, for a
// key whose value starts on the next line (group 4). Deliberately not a
// real YAML parser - a line that doesn't match this shape (a continuation
// of a multi-line scalar, or a plain "- item" list entry with no key)
// just renders unstyled; good enough for the "key: value" and "- key:
// value" shapes every real task definition checked against this project
// uses.
var YamlKeyLine = regexp.MustCompile(`^(\s*(?:-\s+)?)([A-Za-z0-9_.-]+)(:)(\s.*|)$`)

// ColorizeYAML renders raw YAML task source (see source.go) with a light,
// line-based highlight: each line's "key:" portion, if it has one, is
// colored, so structure is scannable at a glance without a real
// tokenizer. Every dynamic piece is escaped separately (not the line as a
// whole before coloring) so a literal "[" in the source itself - e.g.
// "tags: [foo, bar]", which this project's own test fixtures actually
// contain - can never be misread as a color tag.
func ColorizeYAML(raw string) string {
	lines := strings.Split(raw, "\n")
	for i, line := range lines {
		m := YamlKeyLine.FindStringSubmatch(line)
		if m == nil {
			lines[i] = tview.Escape(line)
			continue
		}
		lines[i] = tview.Escape(m[1]) + "[orange::b]" + tview.Escape(m[2]+m[3]) + "[-::-]" + tview.Escape(m[4])
	}
	return strings.Join(lines, "\n")
}

// SectionLabel's own trailing blank line (after the "====" underline,
// before any content) matches design-docs/drilldown.txt's spacing - every
// section there has one blank line between its underline and its content.
func SectionLabel(color, label string) string {
	return fmt.Sprintf("[%s::b]%s[-::-]\n[%s]%s[-]\n\n", color, label, color, strings.Repeat("=", len([]rune(label))))
}

// TaskSourceLocation formats task.Path ("<absolute file>:<line>", see
// events.go/aggregate.go) as "[<file>, line <n>]" for display right below
// the Task section's own YAML - "" if path doesn't have that shape
// (shouldn't happen for a real event, but not trusted blindly).
func TaskSourceLocation(path string) string {
	idx := strings.LastIndex(path, ":")
	if idx == -1 {
		return ""
	}
	file, lineStr := path[:idx], path[idx+1:]
	if file == "" || lineStr == "" {
		return ""
	}
	return fmt.Sprintf("[%s, line %s]", file, lineStr)
}

// TaskSourceFile extracts just the file portion of task.Path
// ("<absolute file>:<line>", see events.go/aggregate.go) - "" if path
// doesn't have that shape (shouldn't happen for a real event, but not
// trusted blindly, same caution as taskSourceLocation above). Backs the
// output drill-down view's own 'e' binding (openTaskSourceFile below),
// which only needs the file to hand to an editor, not the line.
func TaskSourceFile(path string) string {
	idx := strings.LastIndex(path, ":")
	if idx == -1 {
		return ""
	}
	return path[:idx]
}

// RolePathPattern matches the standard Ansible role directory layout
// ("roles/<name>/tasks/...", "roles/<name>/handlers/...", or
// "roles/<name>/templates/...") within a task's or template's own source
// path. A heuristic, not derived from any event field - confirmed
// empirically that a role-sourced task's own v2_playbook_on_task_start
// event carries no distinct "role" field at all, only the convention of
// prefixing the task's display name as "<role> : <task name>" (which this
// deliberately doesn't parse instead - a path-based match is more robust
// than relying on a cosmetic display-name convention). Same "good enough
// for the standard convention, not chased further" style as this file's
// other derived-but-unlabeled info, e.g. colorizeYAML's yamlKeyLine.
// templates was added alongside tasks/handlers for design-docs/Tangsible
// template.md's own role-detection: a template path matching
// roles/<name>/templates/... auto-detects the same way a task's own path
// already does, reusing this single pattern/function rather than a
// parallel one.
var RolePathPattern = regexp.MustCompile(`/roles/([^/]+)/(?:tasks|handlers|templates)/`)

// RoleFromPath returns the role name a task's or template's own path was
// sourced from, or "" if it doesn't match the standard
// roles/<name>/tasks|handlers|templates/ layout at all (a play-level task,
// a template outside any role, or a role laid out unconventionally).
// Matched directly against the full "<file>:<line>" path (or, for a
// template, a plain path with no ":line" suffix at all) with no need to
// strip anything first - the pattern only looks for a "/" immediately
// after "tasks"/"handlers"/"templates", which a trailing ":<line>" (a
// task's own path shape) never interferes with.
func RoleFromPath(path string) string {
	m := RolePathPattern.FindStringSubmatch(path)
	if m == nil {
		return ""
	}
	return m[1]
}

// ResolvedRender is one (task, host) pair's own "Resolved" section state
// (design-docs/Drilldown, Resolved Values.md) - Pending means the
// background render (see NewLiveTUI's resolveCache) hasn't finished yet;
// otherwise exactly one of Text (success - the task's own source with its
// variables filled in) or Err (the resolve attempt's own failure message,
// distinct from the real task's own Failed/Unreachable outcome) is set.
// The zero value (Pending false, Text/Err both empty) means "never
// requested" - formatHostOutput treats that identically to Pending, since
// showOutput always requests a resolve the moment a drill-down opens, so
// in practice the zero value is only ever seen for the single frame
// before that request is even issued.
type ResolvedRender struct {
	Pending bool
	Text    string
	Err     string
}

// BuildOutputTabs is the output drill-down view's own tab-content builder
// (design-docs/Tabbed UI.md), replacing what used to be one monolithic
// formatHostOutput string with up to 7 named tabs instead - Task, Output
// (merging what used to be separate Output/Warnings/Items/Error sections
// into one tab, per design-docs/Tabbed UI.md's own content-mapping
// decision - each piece keeps its own sectionLabel header within it, so
// several distinct pieces sharing one tab don't become an undifferentiated
// blob), Diff, Task definition, Resolved, Docs, and Details - in that
// order. Every tab but Task/Details is dynamic: names/contents simply
// omits one entirely when that particular task has nothing for it (an
// empty "" from that tab's own builder, or - Docs/Resolved specifically -
// docsTabHidden/resolvedTabHidden's own conditions), matching this
// function's predecessor's "omit rather than show a placeholder"
// convention, just applied to whole tabs instead of stacked sections.
// Diff is the newest of these (--diff is now always passed to
// ansible-playbook, see spawnGeneration in main.go) - buildDiffTab below
// omits it whenever a task's result carries no diff key, or one that
// resolves to no actual change (e.g. before == after). Play definition (a
// task's parent play, shown the same way as Task definition) existed
// briefly and was removed after real use showed it was never actually
// consulted - see design-docs/Ideas.md.
//
// Decodes into a generic map (not a fixed struct) since different Ansible
// modules return wildly different result shapes - shared across every
// tab builder below, computed once here. Every piece of dynamic/external
// content in any tab - task source, ansible-doc's own output, stdout/
// stderr/msg, the full JSON, even the raw bytes on a decode failure - is
// individually tview.Escape()'d before being written (each tab's own
// TextView has dynamic colors on, see NewLiveTUI/showOutput), so a
// literal "[" in any of it (e.g. "tags: [a, b]", a JSON array) can never
// be misread as a color tag; only each builder's own fixed label/status
// text is trusted unescaped.
//
// sourceIndex backs Task definition (task.Path) - best-effort: a lookup
// miss (an empty path, or just not found - an unusual file layout, or a
// task genuinely generated at runtime) omits that tab entirely rather
// than showing an error.
//
// resolved carries the "Resolved" tab's own state (design-docs/
// Drilldown, Resolved Values.md) - computed asynchronously by the caller
// (showOutput, see NewLiveTUI), never by this function itself, since a
// real ansible-playbook invocation is far too slow to run synchronously
// from inside a pure render function. docs carries the "Docs" tab's own
// state the same way (showOutput, backed by ansibledoc.go's
// FetchAnsibleDoc) - a real `ansible-doc` invocation, same reasoning.
// Resolved is placed right after Task definition, Docs right after that -
// requested in that order over the module-reference-first ordering this
// originally shipped with, once live use showed the task's own values
// mattered more, front and center, than the module's general docs.
func BuildOutputTabs(task *playbook.TaskNode, host string, sourceIndex map[string]string, resolved ResolvedRender, docs ResolvedRender) (names []string, contents []string) {
	raw := task.Raw[host]
	if len(raw) == 0 {
		// Shouldn't happen in normal operation - every host recorded via
		// recordHost always has some raw payload - but a live jsonl stream
		// from an external process isn't something to trust blindly, so
		// degrade gracefully rather than showing a blank screen.
		return []string{"Output"}, []string{fmt.Sprintf("(no output recorded for %s)", tview.Escape(host))}
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		// Not a JSON object - shouldn't happen for any real module
		// result, but show the raw bytes rather than nothing.
		return []string{"Output"}, []string{tview.Escape(string(raw))}
	}

	o := task.Hosts[host]

	add := func(name, content string) {
		if content == "" {
			return
		}
		names = append(names, name)
		contents = append(contents, content)
	}

	// Task/Details are always present - appended directly rather than
	// through add()'s own empty-skips-it convention, since neither can
	// genuinely come back empty (Task always has at least Name/Host/
	// Status; Details' JSON dump always succeeds or reports its own
	// failure). Resolved is the one exception to "always present" among
	// this trio - see resolvedMatchesSource below.
	names = append(names, "Task")
	contents = append(contents, BuildTaskTab(task, host, decoded, o))

	add("Output", BuildOutputTab(decoded, o))
	add("Diff", BuildDiffTab(decoded))
	taskSource := sourceIndex[task.Path]
	add("Task definition", BuildSourceTab(task.Path, sourceIndex))

	// Omitted specifically when it would show nothing "Task definition"
	// doesn't already - i.e. resolving finished cleanly and came back
	// byte-for-byte the same as the raw source, which happens whenever a
	// task has no {{ }} expressions at all, or every one of them fell
	// back to its own literal text via wrapJinjaDefaults' default()
	// wrapper (design-docs/Drilldown, Resolved Values.md) because nothing
	// could actually resolve it. Deliberately still shown while Pending
	// (the eventual text isn't known yet) or on a genuine Err (that's
	// itself information "Task definition" doesn't carry), and whenever
	// there's no source to compare against at all (taskSource == "",
	// meaning "Task definition" was itself omitted above) - nothing to
	// call "identical" to in that case.
	if !ResolvedTabHidden(resolved, taskSource) {
		names = append(names, "Resolved")
		contents = append(contents, BuildResolvedTab(resolved))
	}

	if !DocsTabHidden(docs) {
		names = append(names, "Docs")
		contents = append(contents, BuildDocsTab(docs))
	}

	names = append(names, "Details")
	contents = append(contents, BuildDetailsTab(decoded, raw))

	return names, contents
}

// BuildTaskTab renders the Task tab's own summary block
// (Name/Action/Role/Host/Status). Role is derived from task.Path via
// roleFromPath - a heuristic, not something any event reports directly -
// and the line is omitted entirely when it's not role-sourced.
func BuildTaskTab(task *playbook.TaskNode, host string, decoded map[string]interface{}, o playbook.Outcome) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Name: %s\n", tview.Escape(task.Name))
	if action, ok := decoded["action"].(string); ok && action != "" {
		fmt.Fprintf(&b, "Action: %s\n", tview.Escape(action))
	}
	if role := RoleFromPath(task.Path); role != "" {
		fmt.Fprintf(&b, "Role: %s\n", tview.Escape(role))
	}
	fmt.Fprintf(&b, "Host: %s\n", tview.Escape(host))
	fmt.Fprintf(&b, "Status: [%s::b]%s[-::-]\n", ColorTag(o), tview.Escape(o.String()))
	return b.String()
}

// BuildOutputTab merges what used to be separate Output/Warnings/Items/
// Error sections into this one tab's own content (design-docs/Tabbed
// UI.md) - each piece keeps its own sectionLabel header, exactly as
// before, so several distinct pieces sharing one tab still read as
// distinct pieces. "" (the tab omitted entirely by buildOutputTabs' own
// add()) only if all four are empty.
func BuildOutputTab(decoded map[string]interface{}, o playbook.Outcome) string {
	var b strings.Builder
	// writeTextSection renders one label+plain-text piece - omitted
	// entirely when text is empty. The trailing "\n\n\n" closes text's
	// own last line and adds two blank lines before whatever comes next,
	// matching sectionLabel's own one-blank-line-after-the-underline
	// spacing to produce drilldown.txt's two-blank-line gap between
	// pieces.
	writeTextSection := func(color, label, text string) {
		if text == "" {
			return
		}
		b.WriteString(SectionLabel(color, label))
		b.WriteString(tview.Escape(text))
		b.WriteString("\n\n\n")
	}
	// Only one of MSG/STDOUT is shown, not both - see primaryOutputField;
	// always labeled "Output" here regardless of which field it came
	// from, since that distinction is an internal selection detail, not
	// something worth surfacing in the header. Skipped is its own case: a
	// skipped result's msg/stdout are rarely useful (often empty, or a
	// generic message that doesn't say what condition skipped it) -
	// skip_reason/false_condition (skipOutputText) is what's actually
	// worth showing here instead.
	var outputText string
	if o == playbook.OutcomeSkipped {
		outputText = SkipOutputText(decoded)
	} else {
		_, outputText = PrimaryOutputField(decoded)
		// additionalOutputLines' own line(s) (Filename:/Command:/User:/SSH
		// public key:, see design-docs/Drilldown, Task List.md) are
		// appended after whatever's already here, one per line, per its
		// own "in addition to anything that might have gone to stdout
		// already" wording - not shown for Skipped, which has its own,
		// unrelated Output content above.
		if extra := AdditionalOutputLines(decoded); len(extra) > 0 {
			joined := strings.Join(extra, "\n")
			if outputText != "" {
				outputText += "\n" + joined
			} else {
				outputText = joined
			}
		}
	}
	writeTextSection("aqua", "Output", outputText)

	// Warnings: design-docs/Drilldown, Task List.md's general rule, not
	// module-specific like everything else here - any result carrying a
	// "warnings" field (a JSON array of strings, confirmed empirically -
	// e.g. ansible's own discovered-interpreter notice) gets its contents
	// shown here, one per line, regardless of outcome or module.
	if warnings := JoinedStringList(decoded["warnings"], "\n"); warnings != "" {
		writeTextSection(WarningColor, "Warnings", warnings)
	}

	// Items: only present for a looped task (loop:/with_*) - see
	// loopItemDetails. Rendered as a "* label" bullet list, matching
	// drilldown.txt's own mockup, one per loop item - plus, when that
	// item carries its own "msg" (typically a failed item's own specific
	// reason, e.g. a per-file permission error a loop over several
	// ansible.builtin.file tasks can produce - distinct from the
	// task-level "msg" the Output section above already shows, which for
	// a failed loop is just Ansible's generic "One or more items
	// failed"), it's shown indented on the line right below that item's
	// own bullet, so each item's reason sits next to the item it belongs
	// to rather than requiring a trip to Details' full JSON to find it.
	if items := LoopItemDetails(decoded); len(items) > 0 {
		b.WriteString(SectionLabel("yellow", "Items"))
		for _, item := range items {
			fmt.Fprintf(&b, "* %s\n", tview.Escape(item.Label))
			if item.Msg != "" {
				fmt.Fprintf(&b, "  %s\n", tview.Escape(item.Msg))
			}
		}
		b.WriteString("\n\n")
	}

	stderr, _ := decoded["stderr"].(string)
	writeTextSection("red", "Error", stderr)

	return strings.TrimRight(b.String(), "\n")
}

// BuildDiffTab renders the Diff tab's own content - "" (the tab omitted
// entirely by buildOutputTabs' own add()) whenever decoded["diff"] is
// absent, or present but produces no actual change to show.
//
// This is a direct port of ansible-core's own default display callback,
// CallbackBase._get_diff (ansible/plugins/callback/__init__.py) - not a
// from-scratch format, so a task's Diff tab here reads exactly like what
// `ansible-playbook --diff` itself would print, since that's the same
// data (decoded["diff"]) and the same shape decisions (a single dict, or
// a list of them for a multi-file diff; each dict may carry
// binary/oversize skip notices, a before/after pair, and/or a literal
// preformatted "prepared" string - never mutually exclusive, matching
// the original's own non-early-returning structure).
func BuildDiffTab(decoded map[string]interface{}) string {
	raw, ok := decoded["diff"]
	if !ok || raw == nil {
		return ""
	}

	var entries []interface{}
	if list, isList := raw.([]interface{}); isList {
		entries = list
	} else {
		entries = []interface{}{raw}
	}

	var blocks []string
	for _, entry := range entries {
		d, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		if block := BuildDiffEntry(d); block != "" {
			blocks = append(blocks, block)
		}
	}
	return strings.Join(blocks, "\n")
}

// BuildDiffEntry renders one diff dict's own content (_get_diff's
// per-entry body) - "" if this entry produced nothing to show at all.
func BuildDiffEntry(d map[string]interface{}) string {
	var b strings.Builder

	if _, ok := d["dst_binary"]; ok {
		b.WriteString("diff skipped: destination file appears to be binary\n")
	}
	if _, ok := d["src_binary"]; ok {
		b.WriteString("diff skipped: source file appears to be binary\n")
	}
	if v, ok := d["dst_larger"]; ok {
		fmt.Fprintf(&b, "diff skipped: destination file size is greater than %v\n", v)
	}
	if v, ok := d["src_larger"]; ok {
		fmt.Fprintf(&b, "diff skipped: source file size is greater than %v\n", v)
	}

	if _, hasBefore := d["before"]; hasBefore {
		if _, hasAfter := d["after"]; hasAfter {
			b.WriteString(UnifiedDiffText(d))
		}
	}

	if prepared, ok := d["prepared"].(string); ok && prepared != "" {
		b.WriteString(tview.Escape(prepared))
	}

	return b.String()
}

// UnifiedDiffText computes and colorizes the before/after unified diff
// for one diff dict - "" if before and after are identical (no hunks),
// mirroring _get_diff's own has_diff check so an unchanged file
// contributes nothing to the tab.
func UnifiedDiffText(d map[string]interface{}) string {
	beforeHeader := "before"
	if h, ok := d["before_header"]; ok {
		beforeHeader = fmt.Sprintf("before: %v", h)
	}
	afterHeader := "after"
	if h, ok := d["after_header"]; ok {
		afterHeader = fmt.Sprintf("after: %v", h)
	}

	beforeLines := DiffLinesWithMarker(DiffFieldText(d["before"]))
	afterLines := DiffLinesWithMarker(DiffFieldText(d["after"]))

	return ColorizedUnifiedDiff(beforeLines, afterLines, beforeHeader, afterHeader)
}

// ColorizedUnifiedDiff computes a's vs b's own unified diff
// (difflib.GetUnifiedDiffString) and colorizes it the same way real
// `diff -u` output reads: green "+" lines, red "-" lines, teal "@@" hunk
// headers, everything else (context lines) plain - "" if a and b produce
// no hunks at all (identical). Shared by unifiedDiffText above (ansible's
// own before/after, within one run's own Diff tab) and design-docs/
// Diff.md's own drill-down tab diffing (diff.go, between two separate
// runs) - the exact same rendering convention, reused rather than
// reinvented, per your own "similar to how other diff utils do it."
func ColorizedUnifiedDiff(a, b []string, fromFile, toFile string) string {
	text, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        a,
		FromFile: fromFile,
		B:        b,
		ToFile:   toFile,
		Context:  3,
	})
	if err != nil || text == "" {
		return ""
	}

	var out strings.Builder
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "+"):
			fmt.Fprintf(&out, "[green]%s[-]\n", tview.Escape(line))
		case strings.HasPrefix(line, "-"):
			fmt.Fprintf(&out, "[red]%s[-]\n", tview.Escape(line))
		case strings.HasPrefix(line, "@@"):
			fmt.Fprintf(&out, "[teal]%s[-]\n", tview.Escape(line))
		default:
			out.WriteString(tview.Escape(line))
			out.WriteString("\n")
		}
	}
	return out.String()
}

// DiffFieldText converts one before/after value into plain text -
// verbatim for a string, "" for nil/absent, else pretty-printed JSON
// (matching _serialize_diff's own default result_format of "json", not
// YAML) for a module that reports a structured value instead (e.g. the
// file module's mode changes).
func DiffFieldText(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	default:
		pretty, err := json.MarshalIndent(t, "", "    ")
		if err != nil {
			return ""
		}
		return string(pretty) + "\n"
	}
}

// DiffLinesWithMarker splits s into lines the way go-difflib's Matcher
// expects - each line keeping its own trailing "\n" (a plain "\n"-based
// heuristic, not Python's full splitlines(True), same "good enough, not
// chased further" tolerance as this file's other line-based heuristics,
// e.g. yamlKeyLine) - and, if the final line has none, appends the same
// "\ No newline at end of file" marker ansible's own _get_diff does, so a
// diff against a file with no trailing newline reads identically.
func DiffLinesWithMarker(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.SplitAfter(s, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	} else {
		lines[len(lines)-1] += "\n\\ No newline at end of file\n"
	}
	return lines
}

// BuildSourceTab renders the Task definition tab's own content - "" (the
// tab omitted entirely by buildOutputTabs' own add()) on a sourceIndex
// lookup miss.
func BuildSourceTab(path string, sourceIndex map[string]string) string {
	source, ok := sourceIndex[path]
	if !ok || source == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString(ColorizeYAML(source))
	b.WriteString("\n")
	if loc := TaskSourceLocation(path); loc != "" {
		fmt.Fprintf(&b, "[gray]%s[-]\n", tview.Escape(loc))
	}
	return b.String()
}

// ResolvedMatchesSource reports whether resolved's own rendered text is
// byte-for-byte identical to source (the task's raw, unresolved YAML,
// i.e. sourceIndex[task.Path] - the same text "Task definition" shows) -
// true only once resolving has actually finished successfully (Pending
// and Err both count as "not identical," since there's genuinely
// different information to show in either case: a still-running resolve,
// or a failure message). Trims one trailing newline from each side before
// comparing - ansible.builtin.template's own file write and this
// project's own source-extraction (source.go) aren't guaranteed to agree
// on a single trailing newline, and that alone shouldn't count as a real
// difference worth a whole extra tab for.
func ResolvedMatchesSource(resolved ResolvedRender, source string) bool {
	if resolved.Pending || resolved.Err != "" {
		return false
	}
	return strings.TrimSuffix(resolved.Text, "\n") == strings.TrimSuffix(source, "\n")
}

// ResolvedTabHidden is buildOutputTabs' actual "omit the Resolved tab"
// decision, factored out so showOutput's own async completion callback
// (which needs the identical condition to decide whether finishing a
// resolve should make the tab appear) can't silently drift out of
// agreement with it. Hidden while still Pending - there's no "Resolving..."
// placeholder shown anymore (see showOutput's own comment on why: a tab
// that shows up immediately only to sometimes vanish once resolving
// finishes identical to source read as broken, not as a feature). Never
// hidden on a genuine Err - that's real information "Task definition"
// doesn't carry. Otherwise hidden exactly when resolvedMatchesSource says
// so, except source == "" (no "Task definition" tab to compare against in
// the first place - see buildOutputTabs) always means "don't hide."
func ResolvedTabHidden(resolved ResolvedRender, source string) bool {
	if resolved.Pending {
		return true
	}
	if resolved.Err != "" {
		return false
	}
	return source != "" && ResolvedMatchesSource(resolved, source)
}

// BuildResolvedTab renders the Resolved tab's own content. Never called
// with resolved.Pending true - buildOutputTabs' own resolvedTabHidden gate
// (this tab's only call site) keeps the tab, and so this function, out of
// the picture entirely until a resolve has actually finished - so unlike
// its predecessor, this has no "Resolving..." case to render.
func BuildResolvedTab(resolved ResolvedRender) string {
	if resolved.Err != "" {
		return "Could not resolve: " + tview.Escape(resolved.Err)
	}
	return tview.Escape(resolved.Text)
}

// DocsTabHidden is buildOutputTabs' "omit the Docs tab" decision, factored
// out the same way resolvedTabHidden is so showOutput's own async
// completion callback (which needs the identical condition to decide
// whether a finished ansible-doc lookup should make the tab appear) can't
// drift out of agreement with it. Hidden while still Pending - same
// "silently absent, not a placeholder that might vanish" reasoning as
// Resolved. Also hidden on the zero value (docs.Pending == false,
// Text == "", Err == "") - what showOutput passes when the task's result
// carries no "action" field at all (see taskAction), i.e. there was never
// anything to look up in the first place. Never hidden on a genuine Err
// once a lookup was actually attempted - e.g. "module not found" or
// ansible-doc missing from PATH is real information worth surfacing, not
// worth pretending the tab doesn't exist over.
func DocsTabHidden(docs ResolvedRender) bool {
	if docs.Pending {
		return true
	}
	return docs.Text == "" && docs.Err == ""
}

// BuildDocsTab renders the Docs tab's own content - ansible-doc -s's own
// output verbatim (design-docs/Ideas.md's "ansible-doc -s" entry), never
// called with docs.Pending true, same reasoning as buildResolvedTab.
func BuildDocsTab(docs ResolvedRender) string {
	if docs.Err != "" {
		return "Could not fetch ansible-doc: " + tview.Escape(docs.Err)
	}
	return tview.Escape(docs.Text)
}

// BuildDetailsTab renders the full result as pretty-printed JSON - always
// succeeds or reports its own formatting failure, so this tab is never
// omitted; this is also what makes every tab set work for any module
// type without having to special-case each one.
func BuildDetailsTab(decoded map[string]interface{}, raw json.RawMessage) string {
	pretty, err := json.MarshalIndent(decoded, "", "  ")
	if err != nil {
		return fmt.Sprintf("(failed to format: %s)\n%s", tview.Escape(err.Error()), tview.Escape(string(raw)))
	}
	return tview.Escape(string(pretty))
}
