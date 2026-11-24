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
	"encoding/json"
	"reflect"
	"strconv"
	"testing"
	"time"
)

func TestDecodeHostResult(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want HostResult
	}{
		{
			name: "changed",
			raw:  `{"changed":true,"msg":"did a thing"}`,
			want: HostResult{Changed: true, Msg: "did a thing"},
		},
		{
			name: "failed",
			raw:  `{"failed":true,"msg":"nope"}`,
			want: HostResult{Failed: true, Msg: "nope"},
		},
		{
			name: "unreachable",
			raw:  `{"unreachable":true}`,
			want: HostResult{Unreachable: true},
		},
		{
			name: "skipped",
			raw:  `{"skipped":true}`,
			want: HostResult{Skipped: true},
		},
		{
			// A decode error must be treated as "no flags set," not
			// surfaced - the caller still wants to record the raw
			// payload even if it can't be classified.
			name: "malformed JSON",
			raw:  `not json at all`,
			want: HostResult{},
		},
		{
			name: "valid JSON but not an object",
			raw:  `[1,2,3]`,
			want: HostResult{},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := DecodeHostResult(json.RawMessage(c.raw))
			if got != c.want {
				t.Errorf("DecodeHostResult(%s) = %+v, want %+v", c.raw, got, c.want)
			}
		})
	}
}

func TestRawEvent_Timestamp(t *testing.T) {
	cases := []struct {
		name string
		text string
		want time.Time
	}{
		{
			name: "RFC3339 with fractional seconds",
			text: "2026-08-06T12:41:02.439015Z",
			want: time.Date(2026, 8, 6, 12, 41, 2, 439015000, time.UTC),
		},
		{
			name: "empty string",
			text: "",
			want: time.Time{},
		},
		{
			name: "garbage string",
			text: "not-a-timestamp",
			want: time.Time{},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ev := RawEvent{TimestampText: c.text}
			got := ev.Timestamp()
			if !got.Equal(c.want) {
				t.Errorf("Timestamp() = %v, want %v", got, c.want)
			}
		})
	}
}

// TestHasNonEmptyWarnings checks the same "presence and non-empty" rule,
// and the same multi-shape ("warnings" as a plain string or a list)
// tolerance, uikit's own JoinedStringList/HasWarnings apply - the two
// must stay in agreement (TaskNode.Warnings' own doc comment, aggregate.go)
// since one feeds the tree/recap's own indicators and the other the
// drill-down's full text, and a real bug elsewhere in this codebase was
// exactly two "same fact, computed two different ways" paths silently
// disagreeing.
func TestHasNonEmptyWarnings(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{"no warnings field at all", `{"changed":false}`, false},
		{"warnings: null", `{"warnings":null}`, false},
		{"warnings: empty string", `{"warnings":""}`, false},
		{"warnings: non-empty string", `{"warnings":"deprecated syntax"}`, true},
		{"warnings: empty list", `{"warnings":[]}`, false},
		{"warnings: list of one empty string", `{"warnings":[""]}`, false},
		{"warnings: list with a real entry", `{"warnings":["","deprecated syntax"]}`, true},
		{"warnings: list with a non-string entry mixed in", `{"warnings":[42,"deprecated syntax"]}`, true},
		{"warnings: list of only non-string entries", `{"warnings":[42,true]}`, false},
		{"deprecations: a real ansible-core 2.19 entry", `{"deprecations":[{"msg":"x is deprecated.","version":"2.24","collection_name":"ansible.builtin"}]}`, true},
		{"deprecations: a plain string", `{"deprecations":["old"]}`, true},
		{"deprecations: empty list", `{"deprecations":[]}`, false},
		{"deprecations: entry without a message", `{"deprecations":[{"version":"2.24"}]}`, false},
		{"deprecations: null", `{"deprecations":null}`, false},
		{"malformed JSON", `not json`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := hasNonEmptyWarnings(json.RawMessage(c.raw)); got != c.want {
				t.Errorf("hasNonEmptyWarnings(%s) = %v, want %v", c.raw, got, c.want)
			}
		})
	}
}

// TestHasNonEmptyStderr checks the same "presence and non-empty" rule
// hasNonEmptyWarnings above does, against the field tui_drilldown.go's own
// Errors section reads (`decoded["stderr"].(string)`) - unlike warnings,
// stderr is always a plain string, never a list.
func TestHasNonEmptyStderr(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{"no stderr field at all", `{"changed":false}`, false},
		{"stderr: empty string", `{"stderr":""}`, false},
		{"stderr: non-empty string", `{"stderr":"connection refused"}`, true},
		{"malformed JSON", `not json`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := hasNonEmptyStderr(json.RawMessage(c.raw)); got != c.want {
				t.Errorf("hasNonEmptyStderr(%s) = %v, want %v", c.raw, got, c.want)
			}
		})
	}
}

func TestResultWarnings(t *testing.T) {
	got := ResultWarnings(json.RawMessage(`{
		"warnings": ["interpreter discovered", ""],
		"deprecations": [
			{"msg": "Importing 'to_text' is deprecated.", "version": "2.24"},
			{"msg": "no version given"},
			{"msg": "numeric version", "version": 2.5},
			"a plain string"
		]}`))
	want := []string{
		"interpreter discovered",
		"Deprecated: Importing 'to_text' is deprecated. (removal in 2.24)",
		"Deprecated: no version given",
		"Deprecated: numeric version (removal in 2.5)",
		"Deprecated: a plain string",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q\nwant %q", got, want)
	}
	if ResultWarnings(json.RawMessage(`{"changed":true}`)) != nil {
		t.Error("a result with neither field must give nil")
	}
}

// A task whose result carries only a deprecation is marked like one with a
// warning (the ⚠ in the tree, the recap's warnings count).
func TestApply_DeprecationOnlyResultCountsAsWarning(t *testing.T) {
	task := &TaskNode{Warnings: map[string]bool{}, Raw: map[string]json.RawMessage{}, Hosts: map[string]Outcome{}, Finished: map[string]time.Time{},
		HasStderr: map[string]bool{}, Ignored: map[string]bool{}, InFlight: map[string]bool{}}
	task.record("h", OutcomeOK, json.RawMessage(`{"deprecations":[{"msg":"old","version":"2.24"}]}`), time.Time{})
	if !task.Warnings["h"] {
		t.Error("a deprecation did not set TaskNode.Warnings")
	}
}

func applyJSON(t *testing.T, s *PlaybookState, lines ...string) {
	t.Helper()
	for _, l := range lines {
		var ev RawEvent
		if err := json.Unmarshal([]byte(l), &ev); err != nil {
			t.Fatal(err)
		}
		s.Apply(ev)
	}
}

func TestApply_ControllerMessageBecomesTaskNotice(t *testing.T) {
	s := &PlaybookState{}
	applyJSON(t, s,
		`{"_event":"v2_playbook_on_play_start","play":{"name":"p"}}`,
		`{"_event":"v2_playbook_on_task_start","task":{"id":"t1","name":"a","path":"/x.yml:3"}}`,
		`{"_event":"tangsible_message","level":"deprecation","message":"apt_repository has been\ndeprecated.","task":{"id":"t1"}}`,
		`{"_event":"tangsible_message","level":"deprecation","message":"apt_repository has been deprecated.","task":{"id":"t1"}}`,
		`{"_event":"tangsible_message","level":"warning","message":"early","task":null}`,
		`{"_event":"tangsible_message","level":"warning","message":"stale","task":{"id":"nope"}}`,
	)
	task := s.Plays[0].Tasks[0]
	want := []string{"Deprecated: apt_repository has been\ndeprecated."}
	if !reflect.DeepEqual(task.Notices, want) {
		t.Errorf("Notices = %q, want %q (wrapped duplicate folded, unknown task dropped)", task.Notices, want)
	}
}

func TestApply_NoticeRepeatingAResultWarningIsDropped(t *testing.T) {
	s := &PlaybookState{}
	applyJSON(t, s,
		`{"_event":"v2_playbook_on_play_start","play":{"name":"p"}}`,
		`{"_event":"v2_playbook_on_task_start","task":{"id":"t1","name":"a","path":"/x.yml:3"}}`,
		// The user's own callback prints the result's warning first ...
		`{"_event":"tangsible_message","level":"warning","message":"Platform linux on host h is using the\ndiscovered Python interpreter","task":{"id":"t1"}}`,
		`{"_event":"tangsible_message","level":"warning","message":"something else","task":{"id":"t1"}}`,
		`{"_event":"v2_runner_on_ok","task":{"id":"t1","name":"a","path":"/x.yml:3"},"hosts":{"h":{"warnings":["Platform linux on host h is using the discovered Python interpreter"]}}}`,
		// ... or after the result.
		`{"_event":"tangsible_message","level":"warning","message":"Platform linux on host h is using the discovered Python interpreter","task":{"id":"t1"}}`,
	)
	task := s.Plays[0].Tasks[0]
	if !reflect.DeepEqual(task.Notices, []string{"something else"}) {
		t.Errorf("Notices = %q", task.Notices)
	}
}

func TestApply_NoticeWithOriginFindsItsTaskEvenIfPrintedFirst(t *testing.T) {
	msg := func(line int) string {
		return `{"_event":"tangsible_message","level":"deprecation","message":"apt_repository is deprecated.\nOrigin: /r/tasks/main.yml:` + strconv.Itoa(line) + `:3\n\n17 - name: x","task":null}`
	}
	s := &PlaybookState{}
	applyJSON(t, s,
		// Printed while the roles load, before any play or task.
		msg(18), msg(14),
		`{"_event":"v2_playbook_on_play_start","play":{"name":"p"}}`,
		`{"_event":"v2_playbook_on_task_start","task":{"id":"a","name":"a","path":"/r/tasks/main.yml:3"}}`,
		`{"_event":"v2_playbook_on_task_start","task":{"id":"b","name":"b","path":"/r/tasks/main.yml:13"}}`,
		`{"_event":"v2_playbook_on_task_start","task":{"id":"c","name":"c","path":"/r/tasks/main.yml:17"}}`,
		`{"_event":"v2_playbook_on_task_start","task":{"id":"d","name":"d","path":"/other.yml:17"}}`,
	)
	got := map[string]int{}
	for _, task := range s.Plays[0].Tasks {
		got[task.Name] = len(task.Notices)
	}
	want := map[string]int{"a": 0, "b": 1, "c": 1, "d": 0}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("notices per task = %v, want %v", got, want)
	}
}
