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

package playbook

import (
	"regexp"
	"strconv"
)

// noticeReach is how many lines below a task's own line (its path) a
// message's origin may lie and still be that task's: the module key, a
// `when:` or `vars:` block in between.
const noticeReach = 25

var originLine = regexp.MustCompile(`(?m)^Origin: (.+?):(\d+):\d+\s*$`)

// messageOrigin extracts "Origin: file:line:col" from an ansible message.
func messageOrigin(msg string) (file string, line int, ok bool) {
	m := originLine.FindStringSubmatch(msg)
	if m == nil {
		return "", 0, false
	}
	n, err := strconv.Atoi(m[2])
	if err != nil {
		return "", 0, false
	}
	return m[1], n, true
}

// pathLine splits a task path "<file>:<line>".
func pathLine(path string) (file string, line int, ok bool) {
	for i := len(path) - 1; i > 0; i-- {
		if path[i] == ':' {
			n, err := strconv.Atoi(path[i+1:])
			return path[:i], n, err == nil
		}
	}
	return "", 0, false
}

// locatedNotice is a controller-side message with a source location, kept
// until it has a task: the task in the same file starting closest above the
// origin line (within noticeReach) owns it, whichever started first.
type locatedNotice struct {
	file    string
	line    int
	text    string
	owner   *TaskNode
	ownerAt int
}

// offer hands the notice a task; it takes it when t is a better (closer)
// owner than the current one.
func (n *locatedNotice) offer(t *TaskNode) {
	file, line, ok := pathLine(t.Path)
	if !ok || file != n.file || line > n.line || n.line-line > noticeReach {
		return
	}
	if n.owner != nil && line <= n.ownerAt {
		return
	}
	if n.owner != nil {
		n.owner.removeNotice(n.text)
	}
	n.owner, n.ownerAt = t, line
	t.AddNotice(n.text)
}

// removeNotice drops the notice with exactly this text.
func (t *TaskNode) removeNotice(text string) {
	kept := t.Notices[:0]
	for _, have := range t.Notices {
		if have != text {
			kept = append(kept, have)
		}
	}
	t.Notices = kept
}
