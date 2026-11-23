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

package inventory

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func fakeInventory(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ansible-inventory"), []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

const fakeTree = `{"_meta":{"hostvars":{"h1":{"ansible_host":"10.0.0.1"},"h2":{}}},"all":{"children":["web"]},"web":{"hosts":["h1","h2"]}}`

func TestListInventoryHosts(t *testing.T) {
	fakeInventory(t, "echo '"+fakeTree+"'")
	got, err := ListInventoryHosts(nil)
	if err != nil || !slices.Equal(got, []string{"h1", "h2"}) {
		t.Errorf("got %v, %v", got, err)
	}
}

func TestListInventoryRaw_ForwardsArgs(t *testing.T) {
	args := filepath.Join(t.TempDir(), "args")
	fakeInventory(t, `echo "$@" > `+args+`; echo '{}'`)
	if _, err := ListInventoryRaw([]string{"-i", "hosts.ini"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(args); strings.TrimSpace(string(got)) != "--list -i hosts.ini" {
		t.Errorf("args = %q", got)
	}
}

func TestListInventoryRaw_Errors(t *testing.T) {
	t.Run("stderr of a failing command", func(t *testing.T) {
		fakeInventory(t, `echo "bad inventory" >&2; exit 1`)
		_, err := ListInventoryRaw(nil)
		if err == nil || !strings.Contains(err.Error(), "bad inventory") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("failing silently falls back to the exit error", func(t *testing.T) {
		fakeInventory(t, `exit 3`)
		if _, err := ListInventoryRaw(nil); err == nil || !strings.Contains(err.Error(), "exit status 3") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("not JSON: the output is quoted, truncated", func(t *testing.T) {
		fakeInventory(t, `printf 'warning: %0400d' 0`)
		_, err := ListInventoryRaw(nil)
		if err == nil || !strings.Contains(err.Error(), "valid JSON") || !strings.HasSuffix(err.Error(), "...") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("binary missing", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		_, err := ListInventoryRaw(nil)
		if err == nil || !strings.Contains(err.Error(), "ansible-inventory not found") {
			t.Errorf("err = %v", err)
		}
	})
}

func TestListHostVarsExport(t *testing.T) {
	args := filepath.Join(t.TempDir(), "args")
	fakeInventory(t, `echo "$@" > `+args+`; echo '`+fakeTree+`'`)
	got, err := ListHostVarsExport([]string{"-i", "x"}, "h1")
	if err != nil || got["ansible_host"] != "10.0.0.1" {
		t.Fatalf("got %v, %v", got, err)
	}
	if a, _ := os.ReadFile(args); strings.TrimSpace(string(a)) != "--list --export -i x" {
		t.Errorf("args = %q", a)
	}
	if got, err := ListHostVarsExport(nil, "h2"); err != nil || len(got) != 0 {
		t.Errorf("host without vars: %v, %v", got, err)
	}
	if got, err := ListHostVarsExport(nil, "unknown"); err != nil || got != nil {
		t.Errorf("unknown host must yield nil, nil: %v, %v", got, err)
	}
}

func TestListHostVarsExport_Errors(t *testing.T) {
	fakeInventory(t, `echo '{"_meta":"not an object"}'`)
	if _, err := ListHostVarsExport(nil, "h1"); err == nil || !strings.Contains(err.Error(), "_meta") {
		t.Errorf("malformed _meta: err = %v", err)
	}
	fakeInventory(t, `exit 1`)
	if _, err := ListHostVarsExport(nil, "h1"); err == nil {
		t.Error("a failing ansible-inventory must be an error")
	}
}
