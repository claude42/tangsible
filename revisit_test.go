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

package main

import (
	"strings"
	"testing"
)

func TestRevisitCommandText(t *testing.T) {
	cases := []struct {
		name string
		e    revisitEntry
		want string
	}{
		{"playbook, no args", revisitEntry{Playbook: "site.yml"}, "tangsible run site.yml"},
		{"playbook with args", revisitEntry{Playbook: "site.yml", Args: "-l zen"}, "tangsible run site.yml -l zen"},
		{"role, no args", revisitEntry{Role: "postfix"}, "tangsible role postfix"},
		{"role with args", revisitEntry{Role: "postfix", Args: "--tags foo"}, "tangsible role postfix --tags foo"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := revisitCommandText(c.e); got != c.want {
				t.Errorf("revisitCommandText(%+v) = %q, want %q", c.e, got, c.want)
			}
		})
	}
}

func TestFormatRevisitTime(t *testing.T) {
	got := formatRevisitTime("2026-08-23T15:00:00Z")
	if got == "2026-08-23T15:00:00Z" || got == "" {
		t.Errorf("formatRevisitTime() = %q, want it reformatted, not left raw", got)
	}

	// A malformed/empty stored time falls back to the raw string rather
	// than panicking or silently showing nothing.
	if got := formatRevisitTime("not a timestamp"); got != "not a timestamp" {
		t.Errorf("formatRevisitTime(garbage) = %q, want the raw string back", got)
	}
}

func TestRevisitStatusLabel(t *testing.T) {
	cases := []struct {
		exitCode int
		want     string
	}{
		{0, "Success"},
		{ansibleUserInterruptedExitCode, "Aborted"},
		{2, "Failed (2)"},
		{-1, "Failed (-1)"},
		{255, "Failed (255)"},
	}
	for _, c := range cases {
		if got := revisitStatusLabel(c.exitCode); got != c.want {
			t.Errorf("revisitStatusLabel(%d) = %q, want %q", c.exitCode, got, c.want)
		}
	}
}

func TestRevisitRowTextStatusColor(t *testing.T) {
	success := revisitRowText(revisitEntry{Playbook: "site.yml", Time: "2026-08-23T15:00:00Z", ExitCode: 0}, 7, false)
	if !strings.Contains(success, "[green]") || !strings.Contains(success, "Success") {
		t.Errorf("revisitRowText(exit 0) = %q, want a [green] \"Success\" label", success)
	}

	interrupted := revisitRowText(revisitEntry{Playbook: "site.yml", Time: "2026-08-23T15:00:00Z", ExitCode: ansibleUserInterruptedExitCode}, 7, false)
	if !strings.Contains(interrupted, "[gray]") || !strings.Contains(interrupted, "Aborted") {
		t.Errorf("revisitRowText(exit 99) = %q, want a [gray] \"Aborted\" label", interrupted)
	}

	failed := revisitRowText(revisitEntry{Playbook: "site.yml", Time: "2026-08-23T15:00:00Z", ExitCode: 2}, 14, false)
	if !strings.Contains(failed, "[red]") || !strings.Contains(failed, "Failed (2)") {
		t.Errorf("revisitRowText(exit 2) = %q, want a [red] \"Failed (2)\" label", failed)
	}

	// The timestamp itself is plain/uncolored - only the status label
	// carries the color, per your own request.
	if strings.Contains(failed, "[white]2026-08-23") == false {
		t.Errorf("revisitRowText(exit 2) = %q, want the timestamp wrapped in a plain [white] tag", failed)
	}

	// labelWidth pads the label out so the trailing " - tangsible ..."
	// column lines up across rows with differently-sized labels.
	wantPadded := "Failed (2)" + strings.Repeat(" ", 14-len("Failed (2)"))
	if !strings.Contains(failed, wantPadded) {
		t.Errorf("revisitRowText(exit 2, labelWidth=14) = %q, want %q padded out to 14 runes", failed, wantPadded)
	}

	// Selected rendering uses the shared pureBlack-on-lightgray convention,
	// not a per-status color - same as host.go's own hostRowText.
	selected := revisitRowText(revisitEntry{Playbook: "site.yml", Time: "2026-08-23T15:00:00Z", ExitCode: 2}, 10, true)
	if !strings.Contains(selected, "lightgray") {
		t.Errorf("revisitRowText(selected) = %q, want the shared selected-row styling", selected)
	}
}
