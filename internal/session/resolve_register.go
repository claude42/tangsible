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

package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

const (
	registerInlineMax = 60 // longest compact value kept on the register: line itself
	registerMaxLines  = 40 // longest pretty-printed value before truncation
)

var registerLinePattern = regexp.MustCompile(`^(\s*(?:- )?)register:\s*([A-Za-z_][A-Za-z0-9_]*)\s*$`)

// resolveRegisterInText rewrites the task's own `register: name` line in
// text (the Resolved tab's render) to `register: name = value`, with value
// taken from snapshot, where debugCaptureSnapshot's mergeRegisteredResult
// already stored this task's own result under that name. A short value
// stays on the line; anything longer goes below it as indented JSON,
// truncated to registerMaxLines. Unlike the rest of the tab this is a
// post-task value (what the task produced, not what it saw). Display only:
// text comes back unchanged when there is no register: line or the
// snapshot has no such key.
func resolveRegisterInText(text string, snapshot json.RawMessage) string {
	var vars map[string]json.RawMessage
	if err := json.Unmarshal(snapshot, &vars); err != nil {
		return text
	}
	lines := strings.Split(text, "\n")
	idx, minIndent := -1, 0
	var name string
	for i, l := range lines {
		m := registerLinePattern.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		// The task's own key is the shallowest match; a deeper one sits
		// inside module args.
		if indent := len(m[1]); idx < 0 || indent < minIndent {
			idx, minIndent, name = i, indent, m[2]
		}
	}
	if idx < 0 {
		return text
	}
	raw, ok := vars[name]
	if !ok {
		return text
	}

	m := registerLinePattern.FindStringSubmatch(lines[idx])
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return text
	}
	if compact.Len() <= registerInlineMax && !strings.Contains(compact.String(), "\n") {
		lines[idx] = fmt.Sprintf("%sregister: %s = %s", m[1], name, compact.String())
		return strings.Join(lines, "\n")
	}

	var pretty bytes.Buffer
	if err := json.Indent(&pretty, raw, "", "  "); err != nil {
		return text
	}
	body := strings.Split(pretty.String(), "\n")
	if len(body) > registerMaxLines {
		more := len(body) - registerMaxLines
		body = append(body[:registerMaxLines], fmt.Sprintf("... (%d more lines)", more))
	}
	pad := strings.Repeat(" ", len(strings.TrimLeft(m[1], "-"))+2)
	out := []string{fmt.Sprintf("%sregister: %s =", m[1], name)}
	for _, b := range body {
		out = append(out, pad+b)
	}
	lines = append(lines[:idx], append(out, lines[idx+1:]...)...)
	return strings.Join(lines, "\n")
}
