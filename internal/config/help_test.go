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

package config

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// scdUnderscore matches the _ that scdoc uses for italics (at a word edge),
// not one inside a name like role_name.
var scdUnderscore = regexp.MustCompile(`(^|[\s\[])_|_($|[\s\]])`)

func stripSCD(s string) string {
	s = strings.ReplaceAll(s, "*", "")
	return scdUnderscore.ReplaceAllString(s, "$1$2")
}

// scdSection returns the first non-empty line under "# name".
func scdSection(t *testing.T, doc, name string) string {
	t.Helper()
	lines := strings.Split(doc, "\n")
	for i, l := range lines {
		if l == "# "+name {
			for _, n := range lines[i+1:] {
				if strings.TrimSpace(n) != "" {
					return strings.TrimSpace(n)
				}
			}
		}
	}
	t.Fatalf("no %s section", name)
	return ""
}

func TestVerbHelpTableCoversEveryVerb(t *testing.T) {
	for _, h := range VerbHelpTable {
		if _, _, ok := ParseVerb([]string{string(h.Verb)}); !ok {
			t.Errorf("table entry %q is not a parseable verb", h.Verb)
		}
	}
	// Every verb ParseVerb accepts needs an entry.
	for _, v := range []Verb{VerbRun, VerbRerun, VerbRole, VerbTemplate, VerbHost, VerbHosts, VerbInventory, VerbRevisit, VerbVault, VerbVersion, VerbDebug, VerbHelp} {
		if _, ok := LookupVerbHelp(v); !ok {
			t.Errorf("verb %q has no help entry", v)
		}
	}
}

func TestVerbHelpMatchesManPages(t *testing.T) {
	for _, h := range VerbHelpTable {
		path := filepath.Join("..", "..", "man", "tangsible-"+string(h.Verb)+".1.scd")
		b, err := os.ReadFile(path)
		if err != nil {
			continue // version and help have no page of their own
		}
		doc := string(b)
		name := stripSCD(scdSection(t, doc, "NAME"))
		wantSummary := strings.TrimSuffix(strings.TrimPrefix(name, "tangsible-"+string(h.Verb)+" - "), ".")
		if h.Summary != wantSummary {
			t.Errorf("%s summary = %q, man NAME says %q", h.Verb, h.Summary, wantSummary)
		}
		if got := stripSCD(scdSection(t, doc, "SYNOPSIS")); h.Synopsis != got {
			t.Errorf("%s synopsis = %q, man SYNOPSIS says %q", h.Verb, h.Synopsis, got)
		}
	}
}

func TestWriteHelp(t *testing.T) {
	var all bytes.Buffer
	if !WriteHelp(&all, "") {
		t.Fatal("WriteHelp(\"\") = false")
	}
	for _, h := range VerbHelpTable {
		if !strings.Contains(all.String(), "tangsible "+string(h.Verb)) || !strings.Contains(all.String(), h.Summary) {
			t.Errorf("overview is missing %s", h.Verb)
		}
	}

	if strings.Contains(all.String(), "[ansible-playbook args") {
		t.Error("overview must not contain synopses")
	}

	var one bytes.Buffer
	if !WriteHelp(&one, "vault") {
		t.Fatal("WriteHelp(vault) = false")
	}
	if !strings.Contains(one.String(), "tangsible vault filename") || strings.Contains(one.String(), "tangsible run") {
		t.Errorf("per-verb help = %q", one.String())
	}

	var bad bytes.Buffer
	if WriteHelp(&bad, "nope") || bad.Len() != 0 {
		t.Error("an unknown topic must print nothing and return false")
	}
}

func TestIsHelpFlag(t *testing.T) {
	for _, a := range []string{"-h", "--help"} {
		if !IsHelpFlag(a) {
			t.Errorf("IsHelpFlag(%q) = false", a)
		}
	}
	if IsHelpFlag("help") || IsHelpFlag("-v") {
		t.Error("IsHelpFlag accepted a non-help argument")
	}
}
