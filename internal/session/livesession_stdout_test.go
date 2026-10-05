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

import "testing"

func TestStdoutLineText(t *testing.T) {
	line := "\x1b[0;32mok: [h1]\x1b[0m"
	if got, want := stdoutLineText(line, false), "ok: [h1[]"; got != want {
		t.Errorf("no color: %q, want %q (escape kept, ANSI dropped)", got, want)
	}
	got := stdoutLineText(line, true)
	if want := "[green:-:-]ok: [h1[][-:-:-][-:-:-]"; got != want {
		t.Errorf("color: %q, want %q", got, want)
	}
	if got := stdoutLineText("[WARNING]: plain", true); got != "[WARNING[]: plain[-:-:-]" {
		t.Errorf("plain line with brackets: %q", got)
	}
}
