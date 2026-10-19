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
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// requirePython3Jinja skips the test if python3 or its jinja2 module
// isn't available - same pattern internal/debugrun's own
// requireAnsiblePlaybook already uses for a real-binary dependency.
func requirePython3Jinja(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not found in PATH, skipping condition-resolve test")
	}
	if err := exec.Command("python3", "-c", "import jinja2").Run(); err != nil {
		t.Skip("python3's jinja2 module not importable, skipping condition-resolve test")
	}
}

func TestExtractTaskConditions(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   []taskCondition
	}{
		{
			name: "a scalar when:",
			source: "- name: foo\n" +
				"  command: bar\n" +
				"  when: something != 0\n",
			want: []taskCondition{
				{key: "when", block: "  when: something != 0", exprs: []string{"something != 0"}, keyIndent: 2},
			},
		},
		{
			name: "a list-form when:, ANDed",
			source: "- name: foo\n" +
				"  command: bar\n" +
				"  when:\n" +
				"    - a == 1\n" +
				"    - b == 2\n" +
				"  register: result\n",
			want: []taskCondition{
				{key: "when", block: "  when:\n    - a == 1\n    - b == 2", exprs: []string{"a == 1", "b == 2"}, keyIndent: 2, isList: true, itemIndent: 4},
			},
		},
		{
			name: "when: and failed_when: on the same task",
			source: "- name: foo\n" +
				"  command: bar\n" +
				"  when: a == 1\n" +
				"  register: result\n" +
				"  failed_when: result.rc != 0\n",
			want: []taskCondition{
				{key: "when", block: "  when: a == 1", exprs: []string{"a == 1"}, keyIndent: 2},
				{key: "failed_when", block: "  failed_when: result.rc != 0", exprs: []string{"result.rc != 0"}, keyIndent: 2},
			},
		},
		{
			name: "no condition keys at all",
			source: "- name: foo\n" +
				"  command: bar\n",
			want: nil,
		},
		{
			name:   "not parseable as YAML",
			source: "{{{ not yaml",
			want:   nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := extractTaskConditions(c.source)
			if len(got) != len(c.want) {
				t.Fatalf("extractTaskConditions(%q) = %+v, want %+v", c.source, got, c.want)
			}
			for i := range got {
				g, w := got[i], c.want[i]
				if g.key != w.key || g.block != w.block || g.keyIndent != w.keyIndent || g.isList != w.isList || g.itemIndent != w.itemIndent {
					t.Errorf("condition %d = %+v, want %+v", i, g, w)
				}
				if len(g.exprs) != len(w.exprs) {
					t.Fatalf("condition %d exprs = %v, want %v", i, g.exprs, w.exprs)
				}
				for j := range g.exprs {
					if g.exprs[j] != w.exprs[j] {
						t.Errorf("condition %d expr %d = %q, want %q", i, j, g.exprs[j], w.exprs[j])
					}
				}
			}
		})
	}
}

func TestRebuildConditionBlock(t *testing.T) {
	scalar := taskCondition{key: "when", keyIndent: 2}
	if got := rebuildConditionBlock(scalar, []string{"1 != 0"}); got != "  when: 1 != 0" {
		t.Errorf("rebuildConditionBlock(scalar) = %q", got)
	}

	list := taskCondition{key: "when", keyIndent: 2, isList: true, itemIndent: 4}
	want := "  when:\n    - 1 == 1\n    - 2 == 2"
	if got := rebuildConditionBlock(list, []string{"1 == 1", "2 == 2"}); got != want {
		t.Errorf("rebuildConditionBlock(list) = %q, want %q", got, want)
	}
}

func TestResolveConditionExprs(t *testing.T) {
	requirePython3Jinja(t)

	snapshot, err := json.Marshal(map[string]interface{}{
		"something":         1,
		"ansible_os_family": "Debian",
		"result":            map[string]interface{}{"rc": 2},
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := resolveConditionExprs(snapshot, []string{
		"something != 0",
		`ansible_os_family == "Debian"`,
		"result.rc != 0",
		"undefined_var == 1",
	})
	if err != nil {
		t.Fatalf("resolveConditionExprs: %v", err)
	}
	want := []string{
		"1 != 0",
		"'Debian' == 'Debian'",
		"2 != 0",
		"undefined_var == 1",
	}
	if len(got) != len(want) {
		t.Fatalf("resolveConditionExprs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("resolveConditionExprs[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestResolveConditionsInText(t *testing.T) {
	requirePython3Jinja(t)

	taskSource := "- name: foo\n" +
		"  command: bar\n" +
		"  when: something != 0\n" +
		"  register: result\n"
	snapshot, err := json.Marshal(map[string]interface{}{"something": 1})
	if err != nil {
		t.Fatal(err)
	}

	got := resolveConditionsInText(taskSource, taskSource, snapshot)
	want := "- name: foo\n" +
		"  command: bar\n" +
		"  when: 1 != 0\n" +
		"  register: result\n"
	if got != want {
		t.Errorf("resolveConditionsInText = %q, want %q", got, want)
	}
}

func TestResolveConditionsInTextNoConditions(t *testing.T) {
	// No python3 dependency needed here: extractTaskConditions already
	// returns nil before anything would shell out.
	text := "- name: foo\n  command: bar\n"
	if got := resolveConditionsInText(text, text, nil); got != text {
		t.Errorf("resolveConditionsInText with no conditions = %q, want unchanged %q", got, text)
	}
}

// TestResolveConditionsInTextWhenIsLastKeyWithTrailingComments is a
// regression test for a real bug: a when: with no sibling key after it
// (the ordinary case - it's usually the task's last key) used to pull
// everything through end of taskSource into its own block, including
// unrelated trailing commented-out tasks. One of those comments here
// contains its own {{ unison_master }}, independently substituted by
// resolveTaskValues' own {{ }}-regex mechanism before
// resolveConditionsInText ever runs - so the old, too-wide block no
// longer matched anywhere in text, and the splice silently failed,
// leaving "when: unison_master == inventory_hostname" completely
// unresolved even though both its own variables were resolvable.
func TestResolveConditionsInTextWhenIsLastKeyWithTrailingComments(t *testing.T) {
	requirePython3Jinja(t)

	taskSource := "- name: Install uniall on unison master\n" +
		"  ansible.builtin.copy:\n" +
		"    src: uniall\n" +
		"    dest: /usr/local/sbin/uniall\n" +
		"  when: unison_master == inventory_hostname\n" +
		"\n" +
		"# - name: Print note\n" +
		"#  ansible.builtin.debug:\n" +
		"#    msg: \"from {{ unison_master }}.\"\n"

	// Mirrors what resolveTaskValues itself would have already done to
	// the trailing comment's own {{ }} expression, before
	// resolveConditionsInText ever sees the text - the exact
	// precondition that exposed the bug.
	text := strings.Replace(taskSource, "{{ unison_master }}", "charon", 1)

	snapshot, err := json.Marshal(map[string]interface{}{
		"unison_master":      "charon",
		"inventory_hostname": "charon",
	})
	if err != nil {
		t.Fatal(err)
	}

	got := resolveConditionsInText(text, taskSource, snapshot)
	if !strings.Contains(got, "when: 'charon' == 'charon'") {
		t.Errorf("resolveConditionsInText = %q, want it to contain the resolved when: line", got)
	}
	if !strings.Contains(got, "from charon.") {
		t.Errorf("resolveConditionsInText = %q, want the earlier {{ }} substitution left intact", got)
	}
}
