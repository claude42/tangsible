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

package template

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestParseTemplateArgs(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		wantPath string
		wantHost string
		wantRest []string
		wantOK   bool
	}{
		{
			name:     "path only",
			args:     []string{"foo.j2"},
			wantPath: "foo.j2",
			wantOK:   true,
		},
		{
			name:     "path and hostname",
			args:     []string{"foo.j2", "myhost"},
			wantPath: "foo.j2",
			wantHost: "myhost",
			wantOK:   true,
		},
		{
			name:     "path, hostname, and -e args",
			args:     []string{"foo.j2", "myhost", "-e", "x=1"},
			wantPath: "foo.j2",
			wantHost: "myhost",
			wantRest: []string{"-e", "x=1"},
			wantOK:   true,
		},
		{
			name:     "path and -e args, no hostname - the flag ends positional parsing",
			args:     []string{"foo.j2", "-e", "x=1"},
			wantPath: "foo.j2",
			wantRest: []string{"-e", "x=1"},
			wantOK:   true,
		},
		{
			name:     "a positional after a flag never becomes the hostname",
			args:     []string{"foo.j2", "-e", "x=1", "myhost"},
			wantPath: "foo.j2",
			wantRest: []string{"-e", "x=1", "myhost"},
			wantOK:   true,
		},
		{
			name:   "no args at all",
			args:   nil,
			wantOK: false,
		},
		{
			name:   "first arg looks like a flag - no path given",
			args:   []string{"-e", "x=1"},
			wantOK: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path, host, rest, ok := ParseTemplateArgs(c.args)
			if ok != c.wantOK {
				t.Fatalf("parseTemplateArgs(%v) ok = %v, want %v", c.args, ok, c.wantOK)
			}
			if !ok {
				return
			}
			if path != c.wantPath || host != c.wantHost || !slices.Equal(rest, c.wantRest) {
				t.Errorf("parseTemplateArgs(%v) = (%q, %q, %v), want (%q, %q, %v)",
					c.args, path, host, rest, c.wantPath, c.wantHost, c.wantRest)
			}
		})
	}
}

func TestRoleVarsFiles(t *testing.T) {
	dir := t.TempDir()
	roleDir := filepath.Join(dir, "roles", "webserver")
	tplDir := filepath.Join(roleDir, "templates")
	if err := os.MkdirAll(tplDir, 0o755); err != nil {
		t.Fatal(err)
	}
	tplPath := filepath.Join(tplDir, "foo.conf.j2")
	if err := os.WriteFile(tplPath, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("neither defaults nor vars exist - nil", func(t *testing.T) {
		if got := RoleVarsFiles(tplPath); got != nil {
			t.Errorf("roleVarsFiles() = %v, want nil", got)
		}
	})

	t.Run("only defaults/main.yml exists", func(t *testing.T) {
		defaultsDir := filepath.Join(roleDir, "defaults")
		if err := os.MkdirAll(defaultsDir, 0o755); err != nil {
			t.Fatal(err)
		}
		defaultsFile := filepath.Join(defaultsDir, "main.yml")
		if err := os.WriteFile(defaultsFile, []byte("x: 1"), 0o644); err != nil {
			t.Fatal(err)
		}
		got := RoleVarsFiles(tplPath)
		if !slices.Equal(got, []string{defaultsFile}) {
			t.Errorf("roleVarsFiles() = %v, want [%s]", got, defaultsFile)
		}
	})

	t.Run("both defaults and vars exist, in that order", func(t *testing.T) {
		varsDir := filepath.Join(roleDir, "vars")
		if err := os.MkdirAll(varsDir, 0o755); err != nil {
			t.Fatal(err)
		}
		varsFile := filepath.Join(varsDir, "main.yml")
		if err := os.WriteFile(varsFile, []byte("y: 2"), 0o644); err != nil {
			t.Fatal(err)
		}
		got := RoleVarsFiles(tplPath)
		wantDefaults := filepath.Join(roleDir, "defaults", "main.yml")
		if !slices.Equal(got, []string{wantDefaults, varsFile}) {
			t.Errorf("roleVarsFiles() = %v, want [%s %s]", got, wantDefaults, varsFile)
		}
	})

	t.Run("a template outside any role - nil", func(t *testing.T) {
		plainPath := filepath.Join(dir, "plain.j2")
		if err := os.WriteFile(plainPath, []byte("hi"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := RoleVarsFiles(plainPath); got != nil {
			t.Errorf("roleVarsFiles() = %v, want nil", got)
		}
	})
}

func TestWriteTemplateStub(t *testing.T) {
	dir := t.TempDir()
	tplPath := filepath.Join(dir, "foo.j2")
	if err := os.WriteFile(tplPath, []byte("hi {{ x }}"), 0o644); err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(dir, "out.txt")

	stubPath, err := WriteTemplateStub(tplPath, outputPath)
	if err != nil {
		t.Fatalf("writeTemplateStub() error: %v", err)
	}
	defer os.Remove(stubPath)

	data, err := os.ReadFile(stubPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	if !strings.Contains(content, "hosts: all") {
		t.Errorf("stub content = %q, want it to always target hosts: all - the actual host is narrowed via --limit at render time, not baked into the stub", content)
	}
	if !strings.Contains(content, "src: "+tplPath) {
		t.Errorf("stub content = %q, want src: %s", content, tplPath)
	}
	if !strings.Contains(content, "dest: "+outputPath) {
		t.Errorf("stub content = %q, want dest: %s", content, outputPath)
	}
	if !strings.Contains(content, "delegate_to: localhost") {
		t.Errorf("stub content = %q, want the template task to delegate_to: localhost - without it the rendered file is written on whatever host the play targets, never locally where tangsible can read it back", content)
	}
	if !strings.Contains(content, "ignore_unreachable: true") {
		t.Errorf("stub content = %q, want ignore_unreachable: true so an unreachable host doesn't abort the render", content)
	}
	if strings.Contains(content, "roles:") {
		t.Error("stub content unexpectedly contains \"roles:\" - a template task must never invoke the role itself, only its vars_files (see roleVarsFiles)")
	}
	if strings.Contains(content, "vars_files:") {
		t.Error("stub content unexpectedly contains vars_files: for a template outside any role")
	}
}

func TestSplitHostTokens(t *testing.T) {
	cases := []struct {
		spec string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"web1", []string{"web1"}},
		{"web1,web2", []string{"web1", "web2"}},
		{"web1, web2 , all", []string{"web1", "web2", "all"}},
		{"web1,,web2", []string{"web1", "web2"}},
	}
	for _, c := range cases {
		if got := SplitHostTokens(c.spec); !slices.Equal(got, c.want) {
			t.Errorf("SplitHostTokens(%q) = %v, want %v", c.spec, got, c.want)
		}
	}
}

func TestResolveHostTokens(t *testing.T) {
	raw := map[string]json.RawMessage{
		"all": json.RawMessage(`{"children": ["web", "db"]}`),
		"web": json.RawMessage(`{"hosts": ["web1", "web2"]}`),
		"db":  json.RawMessage(`{"hosts": ["db1"]}`),
	}
	allHosts := []string{"db1", "web1", "web2"}

	t.Run("a literal host resolves to itself", func(t *testing.T) {
		got, err := resolveHostTokens([]string{"web1"}, raw, allHosts)
		if err != nil {
			t.Fatalf("resolveHostTokens() error: %v", err)
		}
		if !slices.Equal(got, []string{"web1"}) {
			t.Errorf("resolveHostTokens(web1) = %v, want [web1]", got)
		}
	})

	t.Run("a group name expands to its own hosts", func(t *testing.T) {
		got, err := resolveHostTokens([]string{"web"}, raw, allHosts)
		if err != nil {
			t.Fatalf("resolveHostTokens() error: %v", err)
		}
		if !slices.Equal(got, []string{"web1", "web2"}) {
			t.Errorf("resolveHostTokens(web) = %v, want [web1 web2]", got)
		}
	})

	t.Run(`the "all" keyword needs no special-casing - it's already a real group`, func(t *testing.T) {
		got, err := resolveHostTokens([]string{"all"}, raw, allHosts)
		if err != nil {
			t.Fatalf("resolveHostTokens() error: %v", err)
		}
		if !slices.Equal(got, allHosts) {
			t.Errorf("resolveHostTokens(all) = %v, want %v", got, allHosts)
		}
	})

	t.Run("mixing hosts, a group, and all is a harmless deduplicated union", func(t *testing.T) {
		got, err := resolveHostTokens([]string{"web1", "db", "all"}, raw, allHosts)
		if err != nil {
			t.Fatalf("resolveHostTokens() error: %v", err)
		}
		if !slices.Equal(got, []string{"web1", "db1", "web2"}) {
			t.Errorf("resolveHostTokens(web1,db,all) = %v, want [web1 db1 web2] (first-appearance order, deduplicated)", got)
		}
	})

	t.Run("an unknown token is a usage error", func(t *testing.T) {
		if _, err := resolveHostTokens([]string{"nosuchhost"}, raw, allHosts); err == nil {
			t.Error("resolveHostTokens(nosuchhost) error = nil, want an error")
		}
	})
}

func TestConfirmManyHosts(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  bool
	}{
		{"explicit yes", "y\n", true},
		{"explicit yes, full word, any case", "YES\n", true},
		{"empty line defaults to no", "\n", false},
		{"anything else is no", "sure\n", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var out strings.Builder
			got := ConfirmManyHosts(12, strings.NewReader(c.input), &out)
			if got != c.want {
				t.Errorf("ConfirmManyHosts(12, %q) = %v, want %v", c.input, got, c.want)
			}
			if !strings.Contains(out.String(), "12") {
				t.Errorf("ConfirmManyHosts() prompt = %q, want it to mention the host count", out.String())
			}
		})
	}
}

func TestPreferredEditor(t *testing.T) {
	t.Run("VISUAL wins over EDITOR", func(t *testing.T) {
		t.Setenv("VISUAL", "myvisual")
		t.Setenv("EDITOR", "myeditor")
		if got := PreferredEditor(); got != "myvisual" {
			t.Errorf("preferredEditor() = %q, want myvisual", got)
		}
	})
	t.Run("falls back to EDITOR", func(t *testing.T) {
		t.Setenv("VISUAL", "")
		t.Setenv("EDITOR", "myeditor")
		if got := PreferredEditor(); got != "myeditor" {
			t.Errorf("preferredEditor() = %q, want myeditor", got)
		}
	})
	t.Run("falls back to vi when neither is set", func(t *testing.T) {
		t.Setenv("VISUAL", "")
		t.Setenv("EDITOR", "")
		if got := PreferredEditor(); got != "vi" {
			t.Errorf("preferredEditor() = %q, want vi", got)
		}
	})
}
