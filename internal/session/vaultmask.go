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

// Masks vault-protected values in a captured task_vars snapshot for the
// places that must not show them (the Resolved tab, the watch panel's
// autocomplete) - design-docs/Data Tagging.md. The child-side half is
// callback/tangsible_events.py's tangsible_snapshot.
package session

import (
	"bytes"
	"encoding/json"
	"regexp"
	"sort"
	"strings"

	"code.aw.net/claude/tangsible/internal/origin"
)

const (
	vaultPlaceholder = "<vaulted>"
	// minMaskedSecretLen: a shorter secret ("1", "yes") would shred every
	// value it happens to occur in, so it is only masked as a whole value.
	minMaskedSecretLen = 4

	// debugSnapshotExpr asks the debugger REPL for task_vars plus the
	// vault description; the helper lives in the callback plugin.
	debugSnapshotExpr = `__import__('sys').modules['ansible.plugins.callback.tangsible_events'].tangsible_snapshot(task_vars)`
)

// vaultMask says which parts of a snapshot are vault-protected: Vaulted
// holds key/index paths (strings and numbers) of wholly protected values,
// Secrets the decrypted strings, to be found inside derived values too.
type vaultMask struct {
	Vaulted [][]any
	Secrets []string
}

// parseSnapshotQuery splits tangsible_snapshot's JSON reply into the
// task_vars object, its vault mask and its variable origins.
func parseSnapshotQuery(raw json.RawMessage) (json.RawMessage, vaultMask, map[string]origin.Origin, bool) {
	var reply struct {
		Vars    json.RawMessage          `json:"vars"`
		Vaulted [][]any                  `json:"vaulted"`
		Secrets []string                 `json:"secrets"`
		Origins map[string]origin.Origin `json:"origins"`
	}
	if json.Unmarshal(raw, &reply) != nil || len(reply.Vars) == 0 {
		return nil, vaultMask{}, nil, false
	}
	return reply.Vars, vaultMask{Vaulted: reply.Vaulted, Secrets: reply.Secrets}, reply.Origins, true
}

// maskSnapshot returns vars with every vaulted path replaced by
// vaultPlaceholder and every occurrence of a secret inside any other string
// (case-insensitive) replaced by it too. vars is returned unchanged when
// the mask is empty or vars doesn't parse.
func maskSnapshot(vars json.RawMessage, mask vaultMask) json.RawMessage {
	if len(mask.Vaulted) == 0 && len(mask.Secrets) == 0 {
		return vars
	}
	dec := json.NewDecoder(bytes.NewReader(vars))
	dec.UseNumber()
	var root any
	if dec.Decode(&root) != nil {
		return vars
	}
	for _, path := range mask.Vaulted {
		root = setAtPath(root, path)
	}
	root = replaceSecrets(root, secretPattern(mask.Secrets))
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	if enc.Encode(root) != nil {
		return vars
	}
	return bytes.TrimRight(out.Bytes(), "\n")
}

// setAtPath replaces the value at path inside node with the placeholder;
// a path that no longer resolves leaves node alone.
func setAtPath(node any, path []any) any {
	if len(path) == 0 {
		return vaultPlaceholder
	}
	switch n := node.(type) {
	case map[string]any:
		key, ok := path[0].(string)
		if !ok {
			return node
		}
		if child, ok := n[key]; ok {
			n[key] = setAtPath(child, path[1:])
		}
	case []any:
		idx, ok := path[0].(float64)
		if !ok || idx < 0 || int(idx) >= len(n) {
			return node
		}
		n[int(idx)] = setAtPath(n[int(idx)], path[1:])
	}
	return node
}

// secretPattern matches any secret long enough to be searched for, longest
// first so an overlapping shorter one can't split it.
func secretPattern(secrets []string) *regexp.Regexp {
	var parts []string
	for _, s := range secrets {
		if len(s) >= minMaskedSecretLen {
			parts = append(parts, regexp.QuoteMeta(s))
		}
	}
	if len(parts) == 0 {
		return nil
	}
	sort.Slice(parts, func(i, j int) bool { return len(parts[i]) > len(parts[j]) })
	return regexp.MustCompile("(?i)" + strings.Join(parts, "|"))
}

func replaceSecrets(node any, pat *regexp.Regexp) any {
	if pat == nil {
		return node
	}
	switch n := node.(type) {
	case string:
		return pat.ReplaceAllLiteralString(n, vaultPlaceholder)
	case map[string]any:
		for k, v := range n {
			n[k] = replaceSecrets(v, pat)
		}
	case []any:
		for i, v := range n {
			n[i] = replaceSecrets(v, pat)
		}
	}
	return node
}
