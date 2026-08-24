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

// Implements the drill-down view's "Docs" tab (design-docs/Ideas.md's
// "ansible-doc -s" entry, tui.go): shows a task's own module documentation
// by shelling out to the real `ansible-doc` binary, the same way
// resolved.go shells out to a real `ansible-playbook` for the "Resolved"
// tab - reusing ansible's own knowledge of what modules/collections are
// installed rather than trying to bundle or re-derive it.

package main

import (
	"bytes"
	"errors"
	"os/exec"
	"regexp"
	"strings"
)

// ansiCSI matches an ANSI CSI escape sequence (ESC "[" ... final byte) -
// ansible-doc unconditionally writes a handful of these (confirmed live:
// just SGR bold/underline/reset - "\x1b[1m"/"\x1b[4m"/"\x1b[0m" - around
// its own headers) regardless of whether stdout is a real terminal, so
// they show up in fetchAnsibleDoc's captured output too. tview has no
// ANSI support of its own (tview.TranslateANSI exists, but turning its
// output into real style tags and then still safely escaping every other
// literal "[" a module's own EXAMPLES/docs text might contain - a real
// risk, per this file's own tview.Escape() discipline - isn't worth the
// complexity here); stripping them and showing plain text is simpler and
// still correct.
var ansiCSI = regexp.MustCompile("\x1b\\[[0-9;]*[A-Za-z]")

// fetchAnsibleDoc runs `ansible-doc <action>` and returns its stdout, ANSI
// codes stripped (see ansiCSI). The plain long-form output is used
// deliberately, not -s's compact playbook-syntax form - live use showed
// it read better for this tab (design-docs/Ideas.md). action is exactly
// whatever the task's own "action" result field reported (see tui.go's
// TaskAction) - ansible-doc resolves both a bare module name ("copy") and
// its FQCN ("ansible.builtin.copy") on its own, so no normalization is
// needed here. A nonzero exit (module not found, ansible-doc missing from
// PATH, etc.) is reported as an error built from stderr - real
// information the Docs tab shows rather than hides (see tui.go's
// DocsTabHidden).
func fetchAnsibleDoc(action string) (string, error) {
	cmd := exec.Command("ansible-doc", action)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", errors.New(msg)
	}
	return ansiCSI.ReplaceAllString(string(out), ""), nil
}
