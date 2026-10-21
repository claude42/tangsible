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
	"path/filepath"
	"strings"
	"testing"
)

func TestWrapJinjaDefaults(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "bare variable",
			source: "{{ some_var }}",
			want:   `{{ some_var | default("{{ some_var }}") }}`,
		},
		{
			name:   "no expression at all - unchanged",
			source: "plain text, no jinja here",
			want:   "plain text, no jinja here",
		},
		{
			name:   "two expressions on the same line",
			source: "a={{ x }} b={{ y }}",
			want:   `a={{ x | default("{{ x }}") }} b={{ y | default("{{ y }}") }}`,
		},
		{
			name:   "a filter chain",
			source: "{{ x | upper }}",
			want:   `{{ x | upper | default("{{ x | upper }}") }}`,
		},
		{
			name:   "an expression already containing a double quote",
			source: `{{ x | default("y") }}`,
			want:   `{{ x | default("y") | default("{{ x | default(\"y\") }}") }}`,
		},
		{
			name:   "surrounding YAML is left untouched",
			source: "name: install {{ package_name }}\nstate: present",
			want:   "name: install {{ package_name | default(\"{{ package_name }}\") }}\nstate: present",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := wrapJinjaDefaults(c.source); got != c.want {
				t.Errorf("wrapJinjaDefaults(%q) = %q, want %q", c.source, got, c.want)
			}
		})
	}
}

func TestJinjaStringLiteral(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain text", in: "hello", want: `"hello"`},
		{name: "a double quote", in: `say "hi"`, want: `"say \"hi\""`},
		{name: "a backslash", in: `C:\path`, want: `"C:\\path"`},
		{name: "empty string", in: "", want: `""`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := jinjaStringLiteral(c.in); got != c.want {
				t.Errorf("jinjaStringLiteral(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestWrapJinjaDefaultsFallbackRoundTrips(t *testing.T) {
	// The fallback text embedded in the default(...) call must be the
	// exact original {{ ... }} block, quotes and all, still recognizable
	// as itself - a sanity check that jinjaStringLiteral's own escaping
	// doesn't mangle it into something unrecognizable.
	source := `{{ x | default("y") }}`
	got := wrapJinjaDefaults(source)
	if !strings.Contains(got, `\"y\"`) {
		t.Errorf("wrapJinjaDefaults(%q) = %q, want the embedded fallback to contain an escaped \\\"y\\\"", source, got)
	}
}

// TestResolveTaskValues_RealPlaybookDir_Live confirms resolveTaskValues'
// own realPlaybookDir rewrite - design-docs/DebugMode.md's "faked paths"
// follow-up: playbook_dir is the one magic variable
// debugUnfakeShadowPaths' own snapshot-level rewrite can't reach for the
// Resolved tab (ansible-core injects it unconditionally, never
// overridable via the stub's own --extra-vars - confirmed in the spike
// this follows from), so a task referencing it in its own source needs
// this second, output-side fix instead. requireAnsiblePlaybook is
// defined in livesession_debug_test.go, same package.
func TestResolveTaskValues_RealPlaybookDir_Live(t *testing.T) {
	requireAnsiblePlaybook(t)

	repoRoot, err := filepath.Abs("../../")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TANGSIBLE_CALLBACK_DIR", filepath.Join(repoRoot, "callback"))

	taskSource := "{{ playbook_dir }}\n"
	rest := []string{"-i", "localhost,"}

	withoutFix, err := resolveTaskValues("/nonexistent/tasks/main.yml:1", taskSource, "localhost", rest, "", "")
	if err != nil {
		t.Fatalf("resolveTaskValues (no realPlaybookDir): %v", err)
	}
	if !strings.Contains(withoutFix, "tangsible-resolve-") {
		t.Fatalf("resolveTaskValues (no realPlaybookDir) = %q, want it to show the stub's own throwaway temp dir unchanged (sanity check on the test's own assumption)", withoutFix)
	}

	withFix, err := resolveTaskValues("/nonexistent/tasks/main.yml:1", taskSource, "localhost", rest, "", "/my/real/project")
	if err != nil {
		t.Fatalf("resolveTaskValues (realPlaybookDir set): %v", err)
	}
	if !strings.Contains(withFix, "/my/real/project") {
		t.Errorf("resolveTaskValues (realPlaybookDir set) = %q, want it to contain /my/real/project", withFix)
	}
	if strings.Contains(withFix, "tangsible-resolve-") {
		t.Errorf("resolveTaskValues (realPlaybookDir set) = %q, want the stub's own temp dir fully replaced", withFix)
	}
}
