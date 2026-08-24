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

package ansibledoc

import "testing"

func TestAnsiCSIStripsAnsibleDocsOwnEscapeCodes(t *testing.T) {
	// The exact three SGR codes confirmed live against a real
	// `ansible-doc` invocation (see ansiCSI's own doc comment) - bold,
	// underline, reset.
	in := "\x1b[1m> MODULE ansible.builtin.command\x1b[0m\n  \x1b[4mNOTE\x1b[0m: plain text stays untouched"
	want := "> MODULE ansible.builtin.command\n  NOTE: plain text stays untouched"
	if got := ansiCSI.ReplaceAllString(in, ""); got != want {
		t.Errorf("ansiCSI stripping = %q, want %q", got, want)
	}
}

func TestAnsiCSILeavesLiteralBracketsAlone(t *testing.T) {
	// A module's own EXAMPLES text can contain literal "[...]" (e.g. a
	// YAML flow-style list, "tags: [rolestuff]") that must survive
	// untouched - only a real ESC-prefixed CSI sequence is a match.
	in := "tags: [rolestuff, another]"
	if got := ansiCSI.ReplaceAllString(in, ""); got != in {
		t.Errorf("ansiCSI stripping = %q, want the literal brackets left alone: %q", got, in)
	}
}
