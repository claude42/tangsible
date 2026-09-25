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
	"os"
	"path/filepath"
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

// TestReadLocalFileContents covers the delegate_to: localhost/control-host
// path (readLocalFileContents' own doc comment) - a plain os.ReadFile plus
// the same binary check fetchRemoteFileContents shares, so unlike that
// function it needs no ansible-playbook invocation at all to test.
func TestReadLocalFileContents(t *testing.T) {
	t.Run("plain text file returns its content", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "hosts")
		if err := os.WriteFile(path, []byte("127.0.0.1 localhost\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := readLocalFileContents(path)
		if err != nil {
			t.Fatalf("readLocalFileContents() error = %v", err)
		}
		if got != "127.0.0.1 localhost\n" {
			t.Errorf("readLocalFileContents() = %q, want the file's own content", got)
		}
	})

	t.Run("binary content returns empty string, not an error", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "binary.dat")
		if err := os.WriteFile(path, []byte("hello\x00world"), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := readLocalFileContents(path)
		if err != nil {
			t.Fatalf("readLocalFileContents() error = %v, want nil (binary content is a silent \"nothing to show\", not an error)", err)
		}
		if got != "" {
			t.Errorf("readLocalFileContents() = %q, want empty for binary content", got)
		}
	})

	t.Run("a nonexistent file reports a real error", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "does-not-exist")
		if _, err := readLocalFileContents(path); err == nil {
			t.Error("readLocalFileContents() on a missing file returned nil error, want a real one")
		}
	})
}
