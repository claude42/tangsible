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

	"github.com/rivo/tview"
)

// ViewTask is what the view verb's drill-down shows about a task that no
// run produced: where its play runs and what the playbook says about it.
type ViewTask struct {
	Name   string // as listed, role-prefixed ("web : install")
	Path   string // "<file>:<line>", the key of the source index
	Action string
	Hosts  string // the play's host pattern, as written
	Tags   []string
	When   []string
}

// BuildViewTabs is BuildOutputTabs' counterpart for the view verb
// (design-docs/View.md): no host and no result, so only the Task, Task
// definition and Docs tabs.
func BuildViewTabs(t ViewTask, sourceIndex map[string]string, docs ResolvedRender) (names, contents []string) {
	names = append(names, "Task")
	contents = append(contents, BuildViewTaskTab(t))
	if src := BuildSourceTab(t.Path, sourceIndex); src != "" {
		names = append(names, "Task definition")
		contents = append(contents, src)
	}
	if !DocsTabHidden(docs) {
		names = append(names, "Docs")
		contents = append(contents, BuildDocsTab(docs))
	}
	return names, contents
}

// BuildViewTaskTab renders the Task tab's summary block like BuildTaskTab
// does (keys green, values lightsteelblue, one value column), with the
// play's Hosts in place of a host and status.
func BuildViewTaskTab(t ViewTask) string {
	role, taskName := SplitRolePrefix(t.Name)
	if role == "" {
		role = RoleFromPath(t.Path)
	}
	type field struct{ label, value string }
	var fields []field
	if role != "" {
		fields = append(fields, field{"Role", role})
	}
	fields = append(fields, field{"Task", taskName})
	if t.Action != "" {
		fields = append(fields, field{"Action", t.Action})
	}
	fields = append(fields, field{"Hosts", t.Hosts})
	if len(t.When) > 0 {
		fields = append(fields, field{"When", strings.Join(t.When, " and ")})
	}
	if len(t.Tags) > 0 {
		fields = append(fields, field{"Tags", strings.Join(t.Tags, ", ")})
	}
	width := 0
	for _, f := range fields {
		if l := len(f.label) + 1; l > width {
			width = l
		}
	}
	var b strings.Builder
	for _, f := range fields {
		label := f.label + ":"
		fmt.Fprintf(&b, "[%s]%s[-]%s[%s]%s[-]\n", KeyHighlightColor, label, strings.Repeat(" ", width-len(label)+1), ValueHighlightColor, tview.Escape(f.value))
	}
	return b.String()
}
