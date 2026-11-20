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
	"regexp"
	"strings"
	"testing"

	"code.aw.net/claude/tangsible/internal/playbook"
	"code.aw.net/claude/tangsible/internal/uikit"
)

var tagRE = regexp.MustCompile(`\[[^\[\]]*\]`)

func plain(s string) string { return strings.TrimRight(tagRE.ReplaceAllString(s, ""), " ") }

func messageRowTexts(rows []uikit.Row) []string {
	var out []string
	for _, r := range rows {
		out = append(out, plain(r.Text))
	}
	return out
}

func TestFlattenMessageRows(t *testing.T) {
	version := &playbook.TaskNode{Name: "Show version"}
	who := &playbook.TaskNode{Name: "Show who"}
	tasks := []uikit.DebugTask{
		{Play: "web", Task: version, HostCount: 2, Groups: []uikit.DebugGroup{{Hosts: []string{"h1", "h2"}, Messages: []string{"nginx 1.24.0"}}}},
		{Play: "web", Task: who, HostCount: 5, Groups: []uikit.DebugGroup{
			{Hosts: []string{"h1"}, Messages: []string{"a"}},
			{Hosts: []string{"h2"}, Messages: []string{"b"}},
			{Hosts: []string{"h3"}, Messages: []string{"c"}},
			{Hosts: []string{"h4"}, Messages: []string{"d"}},
		}},
	}
	var opened []string
	toggled := 0
	rows := flattenMessageRows(tasks, false, func() { toggled++ }, func(task *playbook.TaskNode, host string) { opened = append(opened, task.Name+"@"+host) })

	want := []string{
		"",
		"Messages (2)",
		"============",
		"",
		"  web",
		"    Show version  all 2 hosts",
		"        nginx 1.24.0",
		"    Show who  h1",
		"        a",
		"      h2",
		"        b",
		"      h3",
		"        c",
		"      ... (+1 more groups)",
	}
	got := messageRowTexts(rows)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("rows:\n%q\nwant:\n%q", got, want)
	}

	// only the heading and the group rows are interactive: the play name,
	// the message lines and the "more groups" row are skipped by the cursor
	var interactive []string
	for i, r := range rows {
		if r.Selected != nil {
			interactive = append(interactive, got[i])
		}
	}
	if strings.Join(interactive, "|") != "Messages (2)|    Show version  all 2 hosts|    Show who  h1|      h2|      h3" {
		t.Errorf("interactive rows = %q", interactive)
	}

	rows[1].Selected()
	if toggled != 1 {
		t.Error("Enter on the heading toggles the section")
	}
	rows[5].Selected()
	rows[9].Selected()
	if strings.Join(opened, ",") != "Show version@h1,Show who@h2" {
		t.Errorf("drill-down opened for %q", opened)
	}

	if got := messageRowTexts(flattenMessageRows(tasks, true, nil, nil)); strings.Join(got, "|") != "|Messages (2)|============" {
		t.Errorf("collapsed: %q", got)
	}
	if rows := flattenMessageRows(nil, false, nil, nil); rows != nil {
		t.Errorf("no messages, no section: %+v", rows)
	}
}

func TestFlattenMessageRowsPlayNameOncePerPlay(t *testing.T) {
	a, b, c := &playbook.TaskNode{Name: "a"}, &playbook.TaskNode{Name: "b"}, &playbook.TaskNode{Name: "c"}
	g := []uikit.DebugGroup{{Hosts: []string{"h"}, Messages: []string{"m"}}}
	rows := flattenMessageRows([]uikit.DebugTask{
		{Play: "one", Task: a, HostCount: 1, Groups: g},
		{Play: "one", Task: b, HostCount: 1, Groups: g},
		{Play: "two", Task: c, HostCount: 1, Groups: g},
	}, false, nil, nil)
	var plays []string
	for _, r := range rows {
		if _, ok := r.ID.(messagePlayRowID); ok {
			plays = append(plays, plain(r.Text))
		}
	}
	if strings.Join(plays, "|") != "  one|  two" {
		t.Errorf("play rows = %q", plays)
	}
}
