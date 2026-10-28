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

package host

import (
	"fmt"
	"strings"
	"testing"

	"code.aw.net/claude/tangsible/internal/config"
	"code.aw.net/claude/tangsible/internal/playbook"
	"code.aw.net/claude/tangsible/internal/runner"
)

func playWith(name string, hosts map[string]playbook.Outcome) *playbook.PlayNode {
	return &playbook.PlayNode{Name: name, Tasks: []*playbook.TaskNode{{Hosts: hosts}}}
}

func TestHostPlayRuns(t *testing.T) {
	state := &playbook.PlaybookState{Plays: []*playbook.PlayNode{
		playWith("ok", map[string]playbook.Outcome{"h": playbook.OutcomeOK}),
		playWith("failed", map[string]playbook.Outcome{"h": playbook.OutcomeFailed}),
		playWith("unreachable", map[string]playbook.Outcome{"h": playbook.OutcomeUnreachable}),
		playWith("other host", map[string]playbook.Outcome{"x": playbook.OutcomeOK}),
		playWith("skipped", map[string]playbook.Outcome{"h": playbook.OutcomeSkipped}),
	}}
	got := hostPlayRuns(state, "h")
	if r, ok := got["ok"]; !ok || r.failed {
		t.Errorf("ok play = %+v, %v; want ran, not failed", r, ok)
	}
	if r, ok := got["failed"]; !ok || !r.failed {
		t.Errorf("failed play = %+v, %v; want ran, failed", r, ok)
	}
	if _, ok := got["skipped"]; !ok {
		t.Error("a play whose tasks were all skipped did run")
	}
	for _, name := range []string{"unreachable", "other host"} {
		if _, ok := got[name]; ok {
			t.Errorf("%q must not count as ran on h", name)
		}
	}
}

func TestHostPlayRunsIgnoredFailureIsNotFailed(t *testing.T) {
	task := &playbook.TaskNode{
		Hosts:   map[string]playbook.Outcome{"h": playbook.OutcomeFailed},
		Ignored: map[string]bool{"h": true},
	}
	state := &playbook.PlaybookState{Plays: []*playbook.PlayNode{{Name: "p", Tasks: []*playbook.TaskNode{task}}}}
	if r := hostPlayRuns(state, "h")["p"]; r.failed {
		t.Error("an ignore_errors failure must not mark the play failed")
	}
}

func TestLastPlayRuns(t *testing.T) {
	ok := map[string]playbook.Outcome{"h": playbook.OutcomeOK}
	unreachable := map[string]playbook.Outcome{"h": playbook.OutcomeUnreachable}
	runs := map[string]*playbook.PlaybookState{
		"new": {Plays: []*playbook.PlayNode{playWith("web", ok), playWith("db", unreachable)}},
		"mid": {Plays: []*playbook.PlayNode{playWith("db", ok)}},
		"old": {Plays: []*playbook.PlayNode{playWith("web", ok), playWith("db", ok), playWith("gone", ok)}},
	}
	cfg := config.StateConfig{History: []config.PlaybookHistory{
		{Playbook: "./site.yml", Invocations: []config.InvocationRecord{
			{Args: "", Time: "2026-10-01T10:00:00Z", RunID: "old"},
			{Args: "--check", Time: "2026-10-03T10:00:00Z", RunID: "new"},
			{Args: "", Time: "2026-10-02T10:00:00Z", RunID: "mid"},
			{Args: "", Time: "2026-10-04T10:00:00Z", RunID: "pruned-by-hand"}, // replay fails: skipped
			{Args: "", Time: "2026-10-05T10:00:00Z"},                          // no RunID
		}},
		{Playbook: "other.yml", Invocations: []config.InvocationRecord{
			{Args: "", Time: "2026-10-09T10:00:00Z", RunID: "new"}, // another playbook: ignored
		}},
		{Role: "site.yml", Invocations: []config.InvocationRecord{
			{Args: "", Time: "2026-10-09T10:00:00Z", RunID: "new"}, // a role, not the playbook
		}},
	}}
	replay := func(id string) (*playbook.PlaybookState, error) {
		if s, ok := runs[id]; ok {
			return s, nil
		}
		return nil, fmt.Errorf("no such run")
	}
	wanted := map[string]bool{"web": true, "db": true, "gone": true, "never": true}
	got := LastPlayRuns(cfg, "site.yml", "h", wanted, replay)

	if r := got["web"]; r.time != "2026-10-03T10:00:00Z" || !r.check {
		t.Errorf("web = %+v, want the newest run, marked check", r)
	}
	if r := got["db"]; r.time != "2026-10-02T10:00:00Z" || r.check {
		t.Errorf("db = %+v, want the mid run (newest one left db unreachable)", r)
	}
	if r := got["gone"]; r.time != "2026-10-01T10:00:00Z" {
		t.Errorf("gone = %+v, want the old run", r)
	}
	if _, ok := got["never"]; ok {
		t.Error("a play in no retained run must be absent")
	}
}

func TestFormatLastPlayRun(t *testing.T) {
	for _, c := range []struct {
		r    lastPlayRun
		want string
	}{
		{lastPlayRun{time: "2026-10-20T12:03:00Z"}, "· ok"},
		{lastPlayRun{time: "2026-10-20T12:03:00Z", failed: true}, "· failed"},
		{lastPlayRun{time: "2026-10-20T12:03:00Z", check: true}, "· ok, check"},
	} {
		if got := formatLastPlayRun(c.r); !strings.Contains(got, c.want) {
			t.Errorf("formatLastPlayRun(%+v) = %q, want it to contain %q", c.r, got, c.want)
		}
	}
}

func TestRenderHostPlaysBracketsLastRun(t *testing.T) {
	entries := []runner.ProgressEntry{{Play: "Web", Task: "a"}, {Play: "Db", Task: "b"}}
	got := RenderHostPlays(entries, map[string]lastPlayRun{"Web": {time: "2026-10-20T12:03:00Z"}})
	if !strings.Contains(got, "(last ran ") || strings.Count(got, "last ran") != 1 {
		t.Errorf("want exactly one bracketed annotation (Web's), got:\n%s", got)
	}
	if plain := RenderHostPlays(entries, nil); strings.Contains(plain, "last ran") {
		t.Errorf("no lookup result, no annotation, got:\n%s", plain)
	}
}
