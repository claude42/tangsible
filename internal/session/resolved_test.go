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
	"os"
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

	withFix, err := resolveTaskValues("/nonexistent/tasks/main.yml:1", taskSource, "localhost", rest, emptySnapshot(t), "/my/real/project")
	if err != nil {
		t.Fatalf("resolveTaskValues: %v", err)
	}
	if !strings.Contains(withFix, "/my/real/project") {
		t.Errorf("resolveTaskValues = %q, want it to contain /my/real/project", withFix)
	}
	if strings.Contains(withFix, "tangsible-resolve-") {
		t.Errorf("resolveTaskValues = %q, want the stub's own temp dir fully replaced", withFix)
	}
}

// TestResolveTaskValues_StripsTags_Live confirms the fix for a bug
// reported live: debug mode's own passthrough args (debugverb.
// PrepareSession's Rest) never go through config.ParsePassthroughArgs the
// way run/role's do, so an active -t/--tags that doesn't match the stub's
// one untagged task used to make it skip silently - no v2_runner_on_*
// event, raw stays nil, and whatever ansible happened to print to stderr
// (an unrelated warning, in the reported case) surfaced as if it were the
// real failure. resolveTaskValues now strips -t/--tags/--skip-tags/
// -l/--limit from rest unconditionally; this confirms a --tags that
// matches nothing real no longer breaks the resolve.
func TestResolveTaskValues_StripsTags_Live(t *testing.T) {
	requireAnsiblePlaybook(t)

	repoRoot, err := filepath.Abs("../../")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TANGSIBLE_CALLBACK_DIR", filepath.Join(repoRoot, "callback"))

	taskSource := "{{ 1 + 1 }}\n"
	rest := []string{"-i", "localhost,", "--tags", "this_tag_matches_nothing"}

	got, err := resolveTaskValues("/nonexistent/tasks/main.yml:1", taskSource, "localhost", rest, emptySnapshot(t), "/my/real/project")
	if err != nil {
		t.Fatalf("resolveTaskValues with an unmatched --tags: %v", err)
	}
	if strings.TrimSpace(got) != "2" {
		t.Errorf("resolveTaskValues with an unmatched --tags = %q, want %q", got, "2")
	}
}

// emptySnapshot writes an empty task_vars snapshot (a bare JSON object)
// for resolveTaskValues' required extraVarsFile.
func emptySnapshot(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "snapshot.json")
	if err := os.WriteFile(p, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}
