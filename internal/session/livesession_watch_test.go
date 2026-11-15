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
	"encoding/json"
	"strings"
	"testing"

	"github.com/rivo/tview"
)

func TestWatchRenderKeepsOneRowPerEntry(t *testing.T) {
	w := newWatchPanel(tview.NewApplication(), func() (json.RawMessage, bool) { return nil, true })
	w.entries = []watchEntry{{expr: "greeting"}, {expr: "app.port"}}
	evaluate := func(string) watchResult { return watchResult{kind: watchResolved, value: "hello"} }
	longOrigin := "/very/long/path/to/some/project/directory/with/many/parts/vars.yml:5"
	origin := func(string) string { return longOrigin }

	for _, width := range []int{40, 60, 200} {
		w.render(evaluate, origin, false, "blue", width, true)
		lines := strings.Split(strings.TrimRight(w.list.GetText(true), "\n"), "\n")
		if len(lines) != 1+len(w.entries) {
			t.Fatalf("width %d: %d lines, want %d: %q", width, len(lines), 1+len(w.entries), lines)
		}
		for _, l := range lines[1:] {
			if n := tview.TaggedStringWidth(l); n > width {
				t.Errorf("width %d: row is %d wide: %q", width, n, l)
			}
		}
		if width == 40 && !strings.Contains(lines[1], "vars.yml:5)") {
			t.Errorf("origin not cut from the left, tail lost: %q", lines[1])
		}
	}

	// Too narrow for any origin: the value stays, the origin goes.
	w.render(evaluate, origin, false, "blue", 22, true)
	if got := w.list.GetText(true); strings.Contains(got, "vars.yml") {
		t.Errorf("origin shown without room: %q", got)
	}
}
