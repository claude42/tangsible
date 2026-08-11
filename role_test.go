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
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRoleStubFilename(t *testing.T) {
	if got, want := roleStubFilename("myrole"), ".tangsible-role-myrole.yml"; got != want {
		t.Errorf("roleStubFilename(myrole) = %q, want %q", got, want)
	}
}

func TestWriteRoleStub(t *testing.T) {
	t.Chdir(t.TempDir())

	path, err := writeRoleStub("myrole")
	if err != nil {
		t.Fatalf("writeRoleStub() error: %v", err)
	}
	if path != ".tangsible-role-myrole.yml" {
		t.Errorf("writeRoleStub() path = %q, want %q", path, ".tangsible-role-myrole.yml")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading generated stub: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "hosts: all") {
		t.Errorf("stub content = %q, want it to contain \"hosts: all\"", content)
	}
	if !strings.Contains(content, "- myrole") {
		t.Errorf("stub content = %q, want it to reference the role \"myrole\"", content)
	}

	t.Run("overwrites a stale leftover from a previous run", func(t *testing.T) {
		if err := os.WriteFile(path, []byte("stale content"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := writeRoleStub("myrole"); err != nil {
			t.Fatalf("writeRoleStub() error on overwrite: %v", err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "stale content") {
			t.Error("writeRoleStub() did not overwrite the stale leftover file")
		}
	})
}

func TestRoleFoundNearby(t *testing.T) {
	t.Chdir(t.TempDir())

	if roleFoundNearby("myrole") {
		t.Error("roleFoundNearby(myrole) = true before ./roles/myrole exists, want false")
	}

	if err := os.MkdirAll(filepath.Join("roles", "myrole", "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !roleFoundNearby("myrole") {
		t.Error("roleFoundNearby(myrole) = false after creating ./roles/myrole, want true")
	}

	// A plain file at that path (not a directory) shouldn't count.
	if err := os.WriteFile(filepath.Join("roles", "notadir"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if roleFoundNearby("notadir") {
		t.Error("roleFoundNearby(notadir) = true for a plain file, want false")
	}
}
