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

package revisit

import (
	"path/filepath"
	"strings"
	"testing"

	"code.aw.net/claude/tangsible/internal/config"
	"code.aw.net/claude/tangsible/internal/runner"
)

func TestRevisitCommandText(t *testing.T) {
	cases := []struct {
		name string
		e    RevisitEntry
		want string
	}{
		{"playbook, no args", RevisitEntry{Playbook: "site.yml"}, "tangsible run site.yml"},
		{"playbook with args", RevisitEntry{Playbook: "site.yml", Args: "-l zen"}, "tangsible run site.yml -l zen"},
		{"role, no args", RevisitEntry{Role: "postfix"}, "tangsible role postfix"},
		{"role with args", RevisitEntry{Role: "postfix", Args: "--tags foo"}, "tangsible role postfix --tags foo"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := RevisitCommandText(c.e); got != c.want {
				t.Errorf("RevisitCommandText(%+v) = %q, want %q", c.e, got, c.want)
			}
		})
	}
}

func TestFormatRevisitTime(t *testing.T) {
	got := FormatRevisitTime("2026-08-23T15:00:00Z")
	if got == "2026-08-23T15:00:00Z" || got == "" {
		t.Errorf("FormatRevisitTime() = %q, want it reformatted, not left raw", got)
	}

	// A malformed/empty stored time falls back to the raw string rather
	// than panicking or silently showing nothing.
	if got := FormatRevisitTime("not a timestamp"); got != "not a timestamp" {
		t.Errorf("FormatRevisitTime(garbage) = %q, want the raw string back", got)
	}
}

func boolPtr(b bool) *bool { return &b }

// TestRevisitStatusLabel exercises classifyExit's bitmask decoding via its
// own RevisitStatusLabel wrapper - the fixed cases (0/99/250/-1), every
// real single-bit and combined-bit TaskQueueManager code (see classifyExit's
// own doc comment), the exit-4 collision in both directions, and the
// genuinely-unrecognized fallback.
func TestRevisitStatusLabel(t *testing.T) {
	cases := []struct {
		name           string
		exitCode       int
		hadUnreachable *bool
		want           string
	}{
		{"OK", 0, nil, "Success"},
		{"user interrupted", runner.AnsibleUserInterruptedExitCode, nil, "Aborted"},
		{"unhandled exception", unhandledExceptionExitCode, nil, "Failed"},
		{"never started (tangsible's own sentinel)", -1, nil, "Failed"},

		{"generic error alone", 1, nil, "Failed"},
		{"failed hosts alone", 2, nil, "Host failed"},
		{"error|failed hosts", 3, nil, "Host failed"},
		{"failed hosts dominates break-play", 2 | 8, nil, "Host failed"},
		{"break-play alone", 8, nil, "Failed"},
		{"error|break-play", 1 | 8, nil, "Failed"},

		{"unreachable alone, confirmed by real event", 4, boolPtr(true), "Success"},
		{"unreachable alone, confirmed no unreachable event (parser error)", 4, boolPtr(false), "Failed"},
		{"unreachable alone, unresolvable (log missing/pruned)", 4, nil, "Failed"},
		{"failed hosts|unreachable - dominates, no ambiguity", 2 | 4, boolPtr(true), "Host failed"},
		{"error|unreachable (also AnsibleOptionsError's own fixed 5)", 1 | 4, nil, "Failed"},

		{"outside the legitimate bitmask range", 255, nil, "Failed (255)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := RevisitStatusLabel(c.exitCode, c.hadUnreachable); got != c.want {
				t.Errorf("RevisitStatusLabel(%d, %v) = %q, want %q", c.exitCode, c.hadUnreachable, got, c.want)
			}
		})
	}
}

func TestRevisitRowTextStatusColor(t *testing.T) {
	success := RevisitRowText(RevisitEntry{Playbook: "site.yml", Time: "2026-08-23T15:00:00Z", ExitCode: 0}, nil, 7, false)
	if !strings.Contains(success, "[green]") || !strings.Contains(success, "Success") {
		t.Errorf("RevisitRowText(exit 0) = %q, want a [green] \"Success\" label", success)
	}

	interrupted := RevisitRowText(RevisitEntry{Playbook: "site.yml", Time: "2026-08-23T15:00:00Z", ExitCode: runner.AnsibleUserInterruptedExitCode}, nil, 7, false)
	if !strings.Contains(interrupted, "[gray]") || !strings.Contains(interrupted, "Aborted") {
		t.Errorf("RevisitRowText(exit 99) = %q, want a [gray] \"Aborted\" label", interrupted)
	}

	failed := RevisitRowText(RevisitEntry{Playbook: "site.yml", Time: "2026-08-23T15:00:00Z", ExitCode: 2}, nil, 14, false)
	if !strings.Contains(failed, "[red]") || !strings.Contains(failed, "Host failed") {
		t.Errorf("RevisitRowText(exit 2) = %q, want a [red] \"Host failed\" label", failed)
	}

	// The timestamp itself is plain/uncolored - only the status label
	// carries the color, per your own request.
	if strings.Contains(failed, "[white]2026-08-23") == false {
		t.Errorf("RevisitRowText(exit 2) = %q, want the timestamp wrapped in a plain [white] tag", failed)
	}

	// labelWidth pads the label out so the trailing " - tangsible ..."
	// column lines up across rows with differently-sized labels.
	wantPadded := "Host failed" + strings.Repeat(" ", 14-len("Host failed"))
	if !strings.Contains(failed, wantPadded) {
		t.Errorf("RevisitRowText(exit 2, labelWidth=14) = %q, want %q padded out to 14 runes", failed, wantPadded)
	}

	// The exit-4 collision reads hadUnreachable, not just the exit code.
	unreachable := RevisitRowText(RevisitEntry{Playbook: "site.yml", Time: "2026-08-23T15:00:00Z", ExitCode: 4}, boolPtr(true), 7, false)
	if !strings.Contains(unreachable, "[green]") || !strings.Contains(unreachable, "Success") {
		t.Errorf("RevisitRowText(exit 4, hadUnreachable=true) = %q, want a [green] \"Success\" label", unreachable)
	}
	parserErr := RevisitRowText(RevisitEntry{Playbook: "site.yml", Time: "2026-08-23T15:00:00Z", ExitCode: 4}, boolPtr(false), 7, false)
	if !strings.Contains(parserErr, "[red]") || !strings.Contains(parserErr, "Failed") {
		t.Errorf("RevisitRowText(exit 4, hadUnreachable=false) = %q, want a [red] \"Failed\" label", parserErr)
	}

	// Selected rendering uses the shared PureBlack-on-lightgray convention,
	// not a per-status color - same as host.go's own HostRowText.
	selected := RevisitRowText(RevisitEntry{Playbook: "site.yml", Time: "2026-08-23T15:00:00Z", ExitCode: 2}, nil, 11, true)
	if !strings.Contains(selected, "lightgray") {
		t.Errorf("RevisitRowText(selected) = %q, want the shared selected-row styling", selected)
	}
}

// withTestStatePath points config.TangsibleStatePath at a fresh temp
// directory for the duration of one test - the same global t.TempDir()'s
// own file happens to be gone once the isolated directory does, so no
// separate os.Remove/restore-file-contents cleanup is needed, only
// restoring the var itself.
func withTestStatePath(t *testing.T) string {
	t.Helper()
	orig := config.TangsibleStatePath
	statePath := filepath.Join(t.TempDir(), ".tangsible", "state.toml")
	config.TangsibleStatePath = statePath
	t.Cleanup(func() { config.TangsibleStatePath = orig })
	return statePath
}

func writeTestRunLog(t *testing.T, statePath, runID string, lines ...string) {
	t.Helper()
	f := config.CreateRunLog(statePath, runID)
	if f == nil {
		t.Fatal("CreateRunLog returned nil")
	}
	for _, line := range lines {
		if _, err := f.WriteString(line + "\n"); err != nil {
			t.Fatalf("write run log: %v", err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close run log: %v", err)
	}
}

func TestResolveHadUnreachable(t *testing.T) {
	statePath := withTestStatePath(t)

	writeTestRunLog(t, statePath, "run-with-unreachable",
		`{"_event":"v2_playbook_on_play_start"}`,
		`{"_event":"v2_runner_on_unreachable"}`,
	)
	if got := resolveHadUnreachable(RevisitEntry{RunID: "run-with-unreachable"}); got == nil || !*got {
		t.Errorf("resolveHadUnreachable(run with a real unreachable event) = %v, want a pointer to true", got)
	}

	writeTestRunLog(t, statePath, "run-without-unreachable",
		`{"_event":"v2_playbook_on_play_start"}`,
		`{"_event":"v2_runner_on_ok"}`,
	)
	if got := resolveHadUnreachable(RevisitEntry{RunID: "run-without-unreachable"}); got == nil || *got {
		t.Errorf("resolveHadUnreachable(run with no unreachable event) = %v, want a pointer to false", got)
	}

	if got := resolveHadUnreachable(RevisitEntry{RunID: "no-such-run"}); got != nil {
		t.Errorf("resolveHadUnreachable(missing log) = %v, want nil", got)
	}
}

// TestClassifyExitIntegratesResolvedUnreachable is an end-to-end check,
// through RunRevisitListTUI's own actual call site shape (RevisitStatusLabel
// fed a resolveHadUnreachable result), that a real saved run with a genuine
// v2_runner_on_unreachable event resolves exit 4 to "Success" - not just
// that the two pieces are individually correct in isolation.
func TestClassifyExitIntegratesResolvedUnreachable(t *testing.T) {
	statePath := withTestStatePath(t)
	writeTestRunLog(t, statePath, "run-4",
		`{"_event":"v2_playbook_on_play_start"}`,
		`{"_event":"v2_runner_on_unreachable"}`,
	)
	e := RevisitEntry{RunID: "run-4", ExitCode: 4}
	if got := RevisitStatusLabel(e.ExitCode, resolveHadUnreachable(e)); got != "Success" {
		t.Errorf("RevisitStatusLabel(exit 4, resolved from a real unreachable event) = %q, want %q", got, "Success")
	}
}
