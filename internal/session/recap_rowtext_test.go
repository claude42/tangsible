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

// Tests for recap.go's row-rendering half (recapHeadingRowText through
// flattenRecapRows) - recap_test.go already covers the data-computation
// half (recapForHost, recapNarrativeSummary, recapCategoryColor). These
// are plain functions over plain data, the same shape as uikit's already-
// tested TaskLabel/HostLabel/PlayRowText - no tview.Application needed,
// just tview.Print's own tag syntax to read back out.
package session

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"code.aw.net/claude/tangsible/internal/playbook"
	"code.aw.net/claude/tangsible/internal/uikit"
	"github.com/rivo/tview"
)

func TestRecapHeadingRowText(t *testing.T) {
	got := recapHeadingRowText()
	want := "[white::b]Summary[-::-]"
	if got != want {
		t.Errorf("recapHeadingRowText() = %q, want %q", got, want)
	}
}

func TestRecapHeadingUnderlineRowText(t *testing.T) {
	got := recapHeadingUnderlineRowText()
	want := "[white]=======[-]" // len("Summary") == 7
	if got != want {
		t.Errorf("recapHeadingUnderlineRowText() = %q, want %q", got, want)
	}
}

func TestRecapNarrativeRowText(t *testing.T) {
	s := &playbook.PlaybookState{}
	s.Apply(playStartEvent("my play"))
	s.Apply(taskStartEvent("t1", "task one", "/pb.yml:3"))
	s.Apply(hostResultEvent("v2_runner_on_ok", "t1", "web1", json.RawMessage(`{"changed":false}`)))

	elapsed := 5 * time.Second
	got := recapNarrativeRowText(s, elapsed)
	want := recapNarrativeSummary(s, elapsed)
	if got != want {
		t.Errorf("recapNarrativeRowText() = %q, want (unescaped, since the narrative never contains a literal %q) %q", got, "[", want)
	}
}

// recapComputeColumnWidths must take the *widest* value across every host
// for each column - hostname length and each field's own digit count -
// not just the last host scanned, and a zero count still costs one digit
// (its own printed "0"), never zero width.
func TestRecapComputeColumnWidths(t *testing.T) {
	addTasks := func(play *playbook.PlayNode, host string, outcome playbook.Outcome, n int) {
		for i := 0; i < n; i++ {
			play.Tasks = append(play.Tasks, &playbook.TaskNode{
				Name:  "task",
				Hosts: map[string]playbook.Outcome{host: outcome},
				Raw:   map[string]json.RawMessage{host: json.RawMessage(`{}`)},
			})
		}
	}

	play := &playbook.PlayNode{Name: "p"}
	addTasks(play, "a", playbook.OutcomeOK, 9)                  // 1-digit OK, short hostname
	addTasks(play, "longhostname", playbook.OutcomeChanged, 10) // 2-digit Changed
	addTasks(play, "longhostname", playbook.OutcomeFailed, 3)   // 1-digit Failed
	addTasks(play, "longhostname", playbook.OutcomeUnreachable, 1)

	state := &playbook.PlaybookState{
		Plays:    []*playbook.PlayNode{play},
		AllHosts: []string{"a", "longhostname"},
	}

	got := recapComputeColumnWidths(state)
	want := recapColumnWidths{
		Host:        len("longhostname"), // 12, widest of "a"/"longhostname"
		OK:          1,                   // max(digits(9), digits(0)) = max(1,1)
		Changed:     2,                   // max(digits(0), digits(10)) = max(1,2)
		Unreachable: 1,                   // max(digits(0), digits(1))
		Failed:      1,                   // max(digits(0), digits(3))
		Skipped:     1,                   // both hosts have 0
		Warnings:    1,                   // both hosts have 0
		Ignored:     1,                   // both hosts have 0
		// ShowDuration/TotalSeconds stay zero - neither task carries any
		// per-host duration data.
	}
	if got != want {
		t.Errorf("recapComputeColumnWidths() = %+v, want %+v", got, want)
	}
}

func TestRecapSummaryFieldColor(t *testing.T) {
	cases := []struct {
		name  string
		label string
		n     int
		want  string
	}{
		{"zero count grays out regardless of label", "ok", 0, uikit.GrayTag},
		{"zero count grays out warnings too", "warnings", 0, uikit.GrayTag},
		{"non-zero ok is green", "ok", 5, "green"},
		{"non-zero failed is red", "failed", 1, "red"},
		{"non-zero warnings is the warning color, not an outcome color", "warnings", 1, uikit.WarningColor},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := recapSummaryFieldColor(c.label, c.n); got != c.want {
				t.Errorf("recapSummaryFieldColor(%q, %d) = %q, want %q", c.label, c.n, got, c.want)
			}
		})
	}
}

// TestRecapCategoryColor exercises every one of recapCategoryColor's own
// labels directly, including its "unrecognized label" default - the one
// branch nothing exercises indirectly through recapForHost, which only
// ever calls it with its own seven fixed labels.
func TestRecapCategoryColor(t *testing.T) {
	cases := []struct {
		label string
		want  string
	}{
		{"ok", "green"},
		{"skipped", "teal"},
		{"changed", "yellow"},
		{"unreachable", "maroon"},
		{"failed", "red"},
		{"warnings", uikit.WarningColor},
		{"ignored", uikit.IgnoredColor},
		{"some-unrecognized-label", "white"},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			if got := recapCategoryColor(c.label); got != c.want {
				t.Errorf("recapCategoryColor(%q) = %q, want %q", c.label, got, c.want)
			}
		})
	}
}

func TestRecapDurationText(t *testing.T) {
	t.Run("no column reserved at all when nothing in the run has a known duration", func(t *testing.T) {
		w := recapColumnWidths{ShowDuration: false, TotalSeconds: 4}
		s := recapHostSummary{HasDuration: true, TotalDuration: 3*time.Second + 400*time.Millisecond}
		if got := recapDurationText(s, w); got != "" {
			t.Errorf("recapDurationText() = %q, want \"\" when w.ShowDuration is false", got)
		}
	})

	t.Run("a known duration renders right-aligned to the column width", func(t *testing.T) {
		w := recapColumnWidths{ShowDuration: true, TotalSeconds: 5} // "12.3" is 4 chars, want 1 leading space
		s := recapHostSummary{HasDuration: true, TotalDuration: 12*time.Second + 300*time.Millisecond}
		if got, want := recapDurationText(s, w), "( 12.3s)"; got != want {
			t.Errorf("recapDurationText() = %q, want %q", got, want)
		}
	})

	t.Run("a host with no known duration gets a same-width blank placeholder, not an empty string", func(t *testing.T) {
		w := recapColumnWidths{ShowDuration: true, TotalSeconds: 4}
		s := recapHostSummary{HasDuration: false}
		got := recapDurationText(s, w)
		want := strings.Repeat(" ", w.TotalSeconds+3) // "(" + width + "s)"
		if got != want {
			t.Errorf("recapDurationText() = %q (len %d), want %q (len %d)", got, len(got), want, len(want))
		}
	})
}

func TestRecapHostRowTextUnselected(t *testing.T) {
	w := recapColumnWidths{Host: 5, OK: 2, Changed: 1, Unreachable: 1, Failed: 1, Skipped: 1, Warnings: 1, Ignored: 1}
	s := recapHostSummary{OK: 12, Changed: 0, Unreachable: 0, Failed: 1, Skipped: 0, Warnings: 0, Ignored: 0}

	got := recapHostRowText("web1", s, w, false)

	// Every field present, right-padded to its own column width, colored
	// by whether it's zero (gray) or not (its outcome color).
	for _, want := range []string{
		"[white::b]web1 [-::-]", // hostname padded out to w.Host=5
		"[green]ok=12[-]",       // non-zero -> outcome color
		"[gray]skipped=0[-]",    // zero -> GrayTag
		"[gray]changed=0[-]",
		"[gray]unreachable=0[-]",
		"[red]failed=1[-]", // non-zero -> outcome color
		"[gray]warnings=0[-]",
		"[gray]ignored=0[-]",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("recapHostRowText() = %q, want it to contain %q", got, want)
		}
	}

	// Fields must appear in ansible's own recap order (plus this app's
	// own warnings/ignored additions, tacked on last): ok, skipped,
	// changed, unreachable, failed, warnings, ignored.
	order := []string{"ok=", "skipped=", "changed=", "unreachable=", "failed=", "warnings=", "ignored="}
	last := -1
	for _, field := range order {
		idx := strings.Index(got, field)
		if idx == -1 {
			t.Fatalf("recapHostRowText() = %q, missing field %q", got, field)
		}
		if idx <= last {
			t.Errorf("recapHostRowText() = %q, field %q out of order", got, field)
		}
		last = idx
	}

	// Unselected rendering must never use the selected rendering's
	// PureBlack-foreground/lightgray-background tag shape.
	if strings.Contains(got, "lightgray") {
		t.Errorf("recapHostRowText(selected=false) = %q, must not use the selected style", got)
	}
}

func TestRecapHostRowTextSelected(t *testing.T) {
	w := recapColumnWidths{Host: 4, OK: 2, Changed: 1, Unreachable: 1, Failed: 1, Skipped: 1, Warnings: 1}
	s := recapHostSummary{OK: 12, Failed: 1}

	got := recapHostRowText("web1", s, w, true)

	// The hostname segment gets the uniform PureBlack-on-lightgray title
	// background, matching HostRowText/TaskLabel's own selected-row
	// convention elsewhere.
	if !strings.Contains(got, "["+uikit.PureBlack+":lightgray:b]web1") {
		t.Errorf("recapHostRowText(selected=true) = %q, want the hostname wrapped in the selected title style", got)
	}
	// Each "label=N" segment inverts to PureBlack text on its own outcome
	// color as a background, not the plain unselected "[color]label=N[-]"
	// shape.
	if !strings.Contains(got, "["+uikit.PureBlack+":green:b]ok=12") {
		t.Errorf("recapHostRowText(selected=true) = %q, want the ok segment as PureBlack-on-green", got)
	}
	if !strings.Contains(got, "["+uikit.PureBlack+":red:b]  failed=1") {
		t.Errorf("recapHostRowText(selected=true) = %q, want the failed segment as PureBlack-on-red with its own leading gap", got)
	}
	if !strings.Contains(got, "["+uikit.PureBlack+":gray:b]  ignored=0") {
		t.Errorf("recapHostRowText(selected=true) = %q, want the trailing ignored segment in the selected style too", got)
	}
	if strings.Contains(got, "[green]ok=12[-]") {
		t.Errorf("recapHostRowText(selected=true) = %q, must not fall back to the unselected tag shape", got)
	}
}

func TestRecapCategoryRowText(t *testing.T) {
	c := recapCategory{
		Label: "failed",
		Color: "red",
		Tasks: []*playbook.TaskNode{{Name: "t1"}, {Name: "t2"}},
	}

	if got, want := recapCategoryRowText(c, false), "[red]  failed (2)[-]"; got != want {
		t.Errorf("recapCategoryRowText(selected=false) = %q, want %q", got, want)
	}
	if got, want := recapCategoryRowText(c, true), "["+uikit.PureBlack+":lightgray:b]  failed (2)[-:-:-]"; got != want {
		t.Errorf("recapCategoryRowText(selected=true) = %q, want %q", got, want)
	}
}

func TestRecapTaskRowText(t *testing.T) {
	task := &playbook.TaskNode{Name: "install nginx"}
	detail := " (that line)"
	color := "green"

	if got, want := recapTaskRowText(task, detail, color, false), "[green]    install nginx (that line)[-]"; got != want {
		t.Errorf("recapTaskRowText(selected=false) = %q, want %q", got, want)
	}
	if got, want := recapTaskRowText(task, detail, color, true), "["+uikit.PureBlack+":lightgray:b]    install nginx (that line)[-:-:-]"; got != want {
		t.Errorf("recapTaskRowText(selected=true) = %q, want %q", got, want)
	}
}

// TestRecapTaskRowTextEscapesBrackets confirms task names/details flowing
// through untouched from ansible (a role or var name could legitimately
// contain "[") get tview.Escape'd, the same discipline every other piece
// of dynamic content in this app follows once dynamic colors are on.
func TestRecapTaskRowTextEscapesBrackets(t *testing.T) {
	task := &playbook.TaskNode{Name: "task [with brackets]"}
	got := recapTaskRowText(task, "", "white", false)
	want := "[white]    " + tview.Escape("task [with brackets]") + "[-]"
	if got != want {
		t.Errorf("recapTaskRowText() = %q, want %q", got, want)
	}
}

// TestFlattenRecapRows exercises the three-level expand/collapse tree
// (host -> category -> task) end to end, since it's what actually wires
// recapHostRowText/recapCategoryRowText/recapTaskRowText together into
// uikit.Row - a real integration point, not just a formatting detail.
func TestFlattenRecapRows(t *testing.T) {
	taskOK := &playbook.TaskNode{
		Name:  "task ok",
		Hosts: map[string]playbook.Outcome{"web1": playbook.OutcomeOK},
		Raw:   map[string]json.RawMessage{"web1": json.RawMessage(`{}`)},
	}
	taskFailed := &playbook.TaskNode{
		Name:  "task failed",
		Hosts: map[string]playbook.Outcome{"web1": playbook.OutcomeFailed},
		Raw:   map[string]json.RawMessage{"web1": json.RawMessage(`{"msg":"boom"}`)},
	}
	taskWeb2 := &playbook.TaskNode{
		Name:  "task on web2",
		Hosts: map[string]playbook.Outcome{"web2": playbook.OutcomeOK},
		Raw:   map[string]json.RawMessage{"web2": json.RawMessage(`{}`)},
	}
	play := &playbook.PlayNode{Name: "p", Tasks: []*playbook.TaskNode{taskOK, taskFailed, taskWeb2}}
	state := &playbook.PlaybookState{
		Plays:    []*playbook.PlayNode{play},
		AllHosts: []string{"web1", "web2"},
	}

	var shownTask *playbook.TaskNode
	var shownHost string
	showOutput := func(task *playbook.TaskNode, host string) {
		shownTask, shownHost = task, host
	}

	hostExpanded := map[string]bool{}
	categoryExpanded := map[recapCategoryRowID]bool{}

	// Fully collapsed: one row per host, nothing else.
	rows := flattenRecapRows(state, hostExpanded, categoryExpanded, showOutput)
	if len(rows) != 2 {
		t.Fatalf("collapsed: got %d rows, want 2 (one per host): %+v", len(rows), rows)
	}
	if rows[0].ID != recapHostRowID("web1") || rows[1].ID != recapHostRowID("web2") {
		t.Fatalf("collapsed: row IDs = [%v, %v], want [web1, web2] in state.AllHosts order", rows[0].ID, rows[1].ID)
	}

	// Expanding a host row's own toggle (its Selected callback) must only
	// affect that host, and must be driven purely through hostExpanded -
	// exactly what a click on that row does in the real tree.
	rows[0].Selected()
	if !hostExpanded["web1"] {
		t.Fatal("web1's row Selected() did not set hostExpanded[\"web1\"]")
	}
	if hostExpanded["web2"] {
		t.Fatal("web1's row Selected() must not affect web2")
	}

	rows = flattenRecapRows(state, hostExpanded, categoryExpanded, showOutput)
	// web1 (host row) -> ok category, failed category -> web2 (host row).
	if len(rows) != 4 {
		t.Fatalf("web1 expanded: got %d rows, want 4: %+v", len(rows), rows)
	}
	wantCat := []recapCategoryRowID{
		{host: "web1", label: "ok"},
		{host: "web1", label: "failed"},
	}
	if rows[1].ID != wantCat[0] || rows[2].ID != wantCat[1] {
		t.Fatalf("category rows = [%v, %v], want %v (ok before failed, matching recapForHost's fixed category order)", rows[1].ID, rows[2].ID, wantCat)
	}
	if rows[3].ID != recapHostRowID("web2") {
		t.Fatalf("row after web1's categories = %v, want web2's own host row", rows[3].ID)
	}

	// Expanding just the "failed" category must reveal only its own task
	// row, leaving the still-collapsed "ok" category's task hidden.
	rows[2].Selected() // the "failed" category row
	if !categoryExpanded[recapCategoryRowID{host: "web1", label: "failed"}] {
		t.Fatal("failed category's row Selected() did not set categoryExpanded")
	}

	rows = flattenRecapRows(state, hostExpanded, categoryExpanded, showOutput)
	if len(rows) != 5 {
		t.Fatalf("failed category expanded: got %d rows, want 5: %+v", len(rows), rows)
	}
	wantTaskID := recapTaskRowID{host: "web1", label: "failed", task: taskFailed}
	if rows[3].ID != wantTaskID {
		t.Fatalf("task row ID = %+v, want %+v", rows[3].ID, wantTaskID)
	}
	if !strings.Contains(rows[3].Text, "task failed") {
		t.Errorf("task row text = %q, want it to contain the task's own name", rows[3].Text)
	}

	// The task row's own Selected callback must be showOutput(task, host),
	// unmodified - this is what Enter/click on a recap task row actually
	// opens.
	rows[3].Selected()
	if shownTask != taskFailed || shownHost != "web1" {
		t.Fatalf("task row Selected() called showOutput(%v, %q), want (%v, %q)", shownTask, shownHost, taskFailed, "web1")
	}
}

func TestFlattenRecapRowsHostWithNoCategoriesHasNoExpansion(t *testing.T) {
	// A host that never reported anything (recapForHost's own "host never
	// reported" case, already covered in recap_test.go) must still get a
	// row, and expanding it must add nothing since it has zero categories.
	state := &playbook.PlaybookState{
		Plays:    []*playbook.PlayNode{{Name: "p"}},
		AllHosts: []string{"idle-host"},
	}
	hostExpanded := map[string]bool{"idle-host": true}
	rows := flattenRecapRows(state, hostExpanded, map[recapCategoryRowID]bool{}, nil)
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1 (just the host row, no categories to expand into)", len(rows))
	}
}
