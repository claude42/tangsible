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
	"strings"
	"testing"

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

func TestRevisitStatusLabel(t *testing.T) {
	cases := []struct {
		exitCode int
		want     string
	}{
		{0, "Success"},
		{runner.AnsibleUserInterruptedExitCode, "Aborted"},
		{2, "Failed (2)"},
		{-1, "Failed (-1)"},
		{255, "Failed (255)"},
	}
	for _, c := range cases {
		if got := RevisitStatusLabel(c.exitCode); got != c.want {
			t.Errorf("RevisitStatusLabel(%d) = %q, want %q", c.exitCode, got, c.want)
		}
	}
}

func TestRevisitRowTextStatusColor(t *testing.T) {
	success := RevisitRowText(RevisitEntry{Playbook: "site.yml", Time: "2026-08-23T15:00:00Z", ExitCode: 0}, 7, false)
	if !strings.Contains(success, "[green]") || !strings.Contains(success, "Success") {
		t.Errorf("RevisitRowText(exit 0) = %q, want a [green] \"Success\" label", success)
	}

	interrupted := RevisitRowText(RevisitEntry{Playbook: "site.yml", Time: "2026-08-23T15:00:00Z", ExitCode: runner.AnsibleUserInterruptedExitCode}, 7, false)
	if !strings.Contains(interrupted, "[gray]") || !strings.Contains(interrupted, "Aborted") {
		t.Errorf("RevisitRowText(exit 99) = %q, want a [gray] \"Aborted\" label", interrupted)
	}

	failed := RevisitRowText(RevisitEntry{Playbook: "site.yml", Time: "2026-08-23T15:00:00Z", ExitCode: 2}, 14, false)
	if !strings.Contains(failed, "[red]") || !strings.Contains(failed, "Failed (2)") {
		t.Errorf("RevisitRowText(exit 2) = %q, want a [red] \"Failed (2)\" label", failed)
	}

	// The timestamp itself is plain/uncolored - only the status label
	// carries the color, per your own request.
	if strings.Contains(failed, "[white]2026-08-23") == false {
		t.Errorf("RevisitRowText(exit 2) = %q, want the timestamp wrapped in a plain [white] tag", failed)
	}

	// labelWidth pads the label out so the trailing " - tangsible ..."
	// column lines up across rows with differently-sized labels.
	wantPadded := "Failed (2)" + strings.Repeat(" ", 14-len("Failed (2)"))
	if !strings.Contains(failed, wantPadded) {
		t.Errorf("RevisitRowText(exit 2, labelWidth=14) = %q, want %q padded out to 14 runes", failed, wantPadded)
	}

	// Selected rendering uses the shared PureBlack-on-lightgray convention,
	// not a per-status color - same as host.go's own HostRowText.
	selected := RevisitRowText(RevisitEntry{Playbook: "site.yml", Time: "2026-08-23T15:00:00Z", ExitCode: 2}, 10, true)
	if !strings.Contains(selected, "lightgray") {
		t.Errorf("RevisitRowText(selected) = %q, want the shared selected-row styling", selected)
	}
}
