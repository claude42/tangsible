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
	"bytes"
	"testing"
)

func TestIsBinaryContent(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want bool
	}{
		{"empty", nil, false},
		{"plain text", []byte("hello world\nline two\n"), false},
		{"a NUL byte", []byte("hello\x00world"), true},
		{"NUL byte just past the sniff window is not seen", append(bytes.Repeat([]byte("a"), 8000), 0), false},
		{"NUL byte within the sniff window", append([]byte("a"), append(bytes.Repeat([]byte("b"), 100), 0)...), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isBinaryContent(c.data); got != c.want {
				t.Errorf("isBinaryContent(...) = %v, want %v", got, c.want)
			}
		})
	}
}

func TestQuoteYAMLString(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain path", "/etc/hosts", `"/etc/hosts"`},
		{"a path with a space", "/etc/my file", `"/etc/my file"`},
		{"a double quote", `/tmp/say "hi"`, `"/tmp/say \"hi\""`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := quoteYAMLString(c.in); got != c.want {
				t.Errorf("quoteYAMLString(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
