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

// Implements the drill-down view's "Resolved" section (design-docs/
// Drilldown, Resolved Values.md): a task's own source, re-rendered with
// its variables filled in, by feeding it back into ansible as a real
// ansible.builtin.template task rather than reimplementing variable
// resolution ourselves - ansible's own templating engine, on ansible's
// own terms, is the only thing that can correctly resolve a value without
// duplicating its ~14-level variable precedence chain (or reaching into
// undocumented internals no supported plugin API exposes - confirmed live
// in the design conversation this follows from that even a custom
// callback plugin doesn't get handed the VariableManager either).
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"code.aw.net/claude/tangsible/internal/playbook"
	"code.aw.net/claude/tangsible/internal/uikit"
)

// jinjaExpressionPattern matches one {{ ... }} expression block in raw
// ansible source text - a regex-based heuristic, not a real Jinja lexer,
// same "documented, not chased to 100%" tolerance this project already
// applies elsewhere (ColorizeYAML's YamlKeyLine, TaskLabel's truncation).
// Known blind spot: a string literal within the expression that itself
// contains a literal "}}" would confuse the non-greedy match boundary -
// not worth a full tokenizer over.
var jinjaExpressionPattern = regexp.MustCompile(`\{\{.*?\}\}`)

// wrapJinjaDefaults rewrites every {{ expr }} in source to
// {{ expr | default("{{ expr }}") }}, so a variable/expression that can't
// be resolved (a loop variable like item, anything genuinely undefined)
// falls back to showing its own original, literal {{ expr }} text rather
// than aborting the whole render. Confirmed live (see design-docs/
// Drilldown, Resolved Values.md) that Jinja doesn't recursively
// re-template a filter's own string output, so the fallback text is never
// itself re-evaluated - safe, ordinary behavior, not a fragile trick.
//
// This only reliably saves a bare variable/attribute reference or a
// filter chain applied to one ({{ x }}, {{ x.y }}, {{ x | upper }} all
// confirmed live to fall back cleanly) - an operator directly combining
// an undefined value with something else (confirmed live: {{ a ~ b }}
// with a undefined raises immediately, before default() ever receives a
// value) still aborts the whole render. resolveTaskValues' own caller
// surfaces that as a plain error for the section rather than a partial
// result - an accepted, documented gap (design-docs/Drilldown, Resolved
// Values.md), not a silent wrong answer.
//
// {% %} statement blocks (as opposed to {{ }} expressions) are left
// untouched entirely - wrapping them isn't meaningful the same way, and
// they're rare within a plain task's own source in practice.
func wrapJinjaDefaults(source string) string {
	return jinjaExpressionPattern.ReplaceAllStringFunc(source, func(match string) string {
		inner := strings.TrimSuffix(strings.TrimPrefix(match, "{{"), "}}")
		return "{{" + inner + "| default(" + jinjaStringLiteral(match) + ") }}"
	})
}

// jinjaStringLiteral quotes s as a Jinja/Python-style double-quoted
// string literal, backslash-escaping any backslash or double quote
// already present in s.
func jinjaStringLiteral(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		if r == '"' || r == '\\' {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('"')
	return b.String()
}

// resolveTaskValues renders taskSource (a task's own raw YAML source,
// exactly as shown in the drill-down's "Task definition" section) with
// its variables resolved for host, and returns the result. Reuses most of
// template.go's own machinery: role-owned vars_files detection
// (roleVarsFiles, keyed off taskPath the same way RoleFromPath already
// is), the same ignore_unreachable/delegate_to: localhost stub shape
// template.go already uses - except targeting host directly via
// hosts: <host> rather than hosts: all + --limit, since a drill-down is
// always for one already-known host, unlike tangsible template's own
// interactive host-switching. rest is the current generation's own
// passthrough args (importantly -i/-e, so the stub sees the same
// inventory/extra-vars context the real run did) - never contains
// -l/--limit itself (parsePassthroughArgs already extracts that
// separately), so there's no risk of it conflicting with this function's
// own hosts: line.
//
// err is non-nil for a genuine failure to determine anything at all
// (mirroring renderTemplate's own pre-flight-failure case) - a Jinja
// error wrapJinjaDefaults couldn't save (see its own doc comment) is
// reported the same way, since from the caller's point of view both just
// mean "show this error instead of a resolved result."
func resolveTaskValues(taskPath, taskSource, host string, rest []string) (string, error) {
	wrapped := wrapJinjaDefaults(taskSource)

	tplFile, err := os.CreateTemp("", "tangsible-resolve-*.j2")
	if err != nil {
		return "", err
	}
	tplPath := tplFile.Name()
	defer os.Remove(tplPath)
	if _, err := tplFile.WriteString(wrapped); err != nil {
		tplFile.Close()
		return "", err
	}
	tplFile.Close()

	outFile, err := os.CreateTemp("", "tangsible-resolve-out-*")
	if err != nil {
		return "", err
	}
	outputPath := outFile.Name()
	outFile.Close()
	defer os.Remove(outputPath)

	// taskPath is "<absolute file>:<line>" (events.go's own shape) -
	// roleVarsFiles expects a plain path, so the trailing ":<line>" is
	// stripped first; it has no bearing on which role (if any) owns the
	// file.
	roleTaskPath := taskPath
	if idx := strings.LastIndex(taskPath, ":"); idx != -1 {
		roleTaskPath = taskPath[:idx]
	}

	var b strings.Builder
	fmt.Fprintf(&b, "- hosts: %s\n", host)
	b.WriteString("  ignore_unreachable: true\n")
	if varsFiles := roleVarsFiles(roleTaskPath); len(varsFiles) > 0 {
		b.WriteString("  vars_files:\n")
		for _, f := range varsFiles {
			fmt.Fprintf(&b, "    - %s\n", f)
		}
	}
	b.WriteString("  tasks:\n")
	b.WriteString("    - name: resolve task values\n")
	b.WriteString("      delegate_to: localhost\n")
	b.WriteString("      ansible.builtin.template:\n")
	fmt.Fprintf(&b, "        src: %s\n", tplPath)
	fmt.Fprintf(&b, "        dest: %s\n", outputPath)

	stubFile, err := os.CreateTemp("", "tangsible-resolve-stub-*.yml")
	if err != nil {
		return "", err
	}
	stubPath := stubFile.Name()
	defer os.Remove(stubPath)
	if _, err := stubFile.WriteString(b.String()); err != nil {
		stubFile.Close()
		return "", err
	}
	stubFile.Close()

	cmd := exec.Command("ansible-playbook", append([]string{stubPath}, rest...)...)
	cmd.Env = append(os.Environ(),
		"ANSIBLE_STDOUT_CALLBACK=ansible.posix.jsonl",
		"ANSIBLE_JSON_INDENT=0",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, runErr := cmd.Output()

	// Same event-scanning approach as renderTemplate (template.go): scan
	// every line for our one host's own result on the one task this stub
	// ever runs, keeping only the last match (the delegated task's own
	// completion, overwriting any earlier event for the same host - e.g.
	// an ignored Gathering Facts unreachable).
	var raw json.RawMessage
	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var ev playbook.RawEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		switch ev.Event {
		case "v2_runner_on_ok", "v2_runner_on_failed", "v2_runner_on_unreachable":
			if hostRaw, ok := ev.Hosts[host]; ok {
				raw = hostRaw
			}
		}
	}

	if raw == nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" && runErr != nil {
			msg = runErr.Error()
		}
		if msg == "" {
			msg = fmt.Sprintf("no result reported for host %q", host)
		}
		return "", fmt.Errorf("%s", msg)
	}

	decoded := playbook.DecodeHostResult(raw)
	if decoded.Failed || decoded.Unreachable {
		var full map[string]interface{}
		_ = json.Unmarshal(raw, &full)
		_, msg := uikit.PrimaryOutputField(full)
		if msg == "" {
			msg = decoded.Msg
		}
		return "", fmt.Errorf("%s", msg)
	}

	content, err := os.ReadFile(outputPath)
	if err != nil {
		return "", fmt.Errorf("resolve task reported success but the rendered file couldn't be read: %w", err)
	}
	return string(content), nil
}
