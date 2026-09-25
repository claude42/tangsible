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

// stripANSI is FetchAnsibleDoc's own pure text transform, pulled out
// specifically so it's testable without a real ansible-doc binary on
// PATH - see its own doc comment.
func TestStripANSI(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"no escape sequences at all", "plain text, no codes here", "plain text, no codes here"},
		{"bold SGR", "\x1b[1mMODULE NAME\x1b[0m", "MODULE NAME"},
		{"underline SGR", "\x1b[4mSYNOPSIS\x1b[0m", "SYNOPSIS"},
		{"multiple sequences in one string", "\x1b[1mcopy\x1b[0m - \x1b[4mCopies files\x1b[0m", "copy - Copies files"},
		{"a multi-parameter SGR (bold+underline combined)", "\x1b[1;4mheading\x1b[0m", "heading"},
		{"empty string", "", ""},
		// A real "[" from a module's own docs/EXAMPLES text (no ESC byte
		// preceding it) must survive untouched - only a genuine CSI
		// sequence (ESC "[" ... final byte) is stripped, not every
		// literal "[" in the text.
		{"a literal bracket with no preceding ESC is left alone", "state: [present, absent]", "state: [present, absent]"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := stripANSI(c.in); got != c.want {
				t.Errorf("stripANSI(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
