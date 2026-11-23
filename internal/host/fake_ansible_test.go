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

package host

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeBin installs shell scripts named after the ansible tools at the front
// of PATH (and points the plugin extraction cache at a temp dir), so the
// Fetch* functions run against canned output instead of a real ansible.
func fakeBin(t *testing.T, scripts map[string]string) {
	t.Helper()
	dir := t.TempDir()
	for name, script := range scripts {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
}

const fakeTree = `{"_meta":{"hostvars":{"h1":{"ansible_host":"10.0.0.1","from_file":"inv"}}},"all":{"children":["web"]},"web":{"children":["app"]},"app":{"hosts":["h1"]}}`

func TestFetchHostGroups(t *testing.T) {
	fakeBin(t, map[string]string{"ansible-inventory": "echo '" + fakeTree + "'"})
	got, err := FetchHostGroups("h1", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"app", "(direct)", "web", "(via app)", "all"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
	got, err = FetchHostGroups("stranger", nil)
	if err != nil || !strings.Contains(got, "is not a member of any inventory group") {
		t.Errorf("host outside every group: %q, %v", got, err)
	}
}

func TestFetchHostGroups_Errors(t *testing.T) {
	fakeBin(t, map[string]string{"ansible-inventory": `echo "no such inventory" >&2; exit 1`})
	if _, err := FetchHostGroups("h1", nil); err == nil || !strings.Contains(err.Error(), "no such inventory") {
		t.Errorf("err = %v", err)
	}
	fakeBin(t, map[string]string{"ansible-inventory": `echo nonsense`})
	if _, err := FetchHostGroups("h1", nil); err == nil || !strings.Contains(err.Error(), "valid JSON") {
		t.Errorf("err = %v", err)
	}
	t.Setenv("PATH", t.TempDir())
	if _, err := FetchHostGroups("h1", nil); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("err = %v", err)
	}
}

func TestFetchHostVars_FileWinsOverTheInventoryCopy(t *testing.T) {
	proj := t.TempDir()
	os.MkdirAll(filepath.Join(proj, "host_vars"), 0o755)
	os.WriteFile(filepath.Join(proj, "host_vars", "h1.yml"), []byte("from_file: filevalue\n"), 0o600)
	os.WriteFile(filepath.Join(proj, "hosts.ini"), []byte("h1\n"), 0o600)
	fakeBin(t, map[string]string{"ansible-inventory": "echo '" + fakeTree + "'"})

	got, err := FetchHostVars("h1", "", []string{"-i", filepath.Join(proj, "hosts.ini")})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "ansible_host") || !strings.Contains(got, "filevalue") {
		t.Errorf("inventory-only var and the file's content must both show:\n%s", got)
	}
	if strings.Contains(got, "from_file: inv") || strings.Contains(got, "inv\n") {
		t.Errorf("a key a host_vars file defines must not also show with the inventory's value:\n%s", got)
	}
}

func TestFetchHostVars_NothingFound(t *testing.T) {
	fakeBin(t, map[string]string{"ansible-inventory": `echo '{"_meta":{"hostvars":{}}}'`})
	got, err := FetchHostVars("h1", "", []string{"-i", filepath.Join(t.TempDir(), "hosts.ini")})
	if err != nil || !strings.Contains(got, `no host_vars files or inventory variables found for host "h1"`) {
		t.Errorf("got %q, %v", got, err)
	}
}

func TestFetchHostVars_InventoryErrorIsShownNotFatal(t *testing.T) {
	fakeBin(t, map[string]string{"ansible-inventory": `echo broken >&2; exit 1`})
	got, err := FetchHostVars("h1", "", []string{"-i", filepath.Join(t.TempDir(), "hosts.ini")})
	if err != nil || !strings.Contains(got, "couldn't read inventory variables") || !strings.Contains(got, "broken") {
		t.Errorf("got %q, %v", got, err)
	}
}

const listTasksOut = `
playbook: site.yml

  play #1 (all): First play	TAGS: []
    pattern: ['all']
    hosts (1):
      h1
    tasks:
      a task	TAGS: []
`

func TestFetchHostPlayEntries(t *testing.T) {
	args := filepath.Join(t.TempDir(), "args")
	fakeBin(t, map[string]string{"ansible-playbook": `echo "$@" > ` + args + `
cat <<'EOF'` + listTasksOut + `EOF`})

	entries, note, err := FetchHostPlayEntries("site.yml", []string{"-i", "x"}, "h1")
	if err != nil || note != "" || len(entries) != 1 || entries[0].Task != "a task" || entries[0].Play != "First play" {
		t.Fatalf("got %+v, %q, %v", entries, note, err)
	}
	if a, _ := os.ReadFile(args); strings.TrimSpace(string(a)) != "site.yml -i x --limit h1 --list-tasks --list-hosts" {
		t.Errorf("args = %q: the host must be narrowed with --limit alongside --list-hosts", a)
	}
}

func TestFetchHostPlayEntries_NoPlaybookNothingToRunAndFailure(t *testing.T) {
	_, note, err := FetchHostPlayEntries("", nil, "h1")
	if err != nil || !strings.Contains(note, "no playbook specified") {
		t.Errorf("no playbook: %q, %v", note, err)
	}
	fakeBin(t, map[string]string{"ansible-playbook": `echo "playbook: site.yml"`})
	entries, note, err := FetchHostPlayEntries("site.yml", nil, "h1")
	if err != nil || entries != nil || !strings.Contains(note, `no plays would run for host "h1"`) {
		t.Errorf("nothing runs: %+v, %q, %v", entries, note, err)
	}
	fakeBin(t, map[string]string{"ansible-playbook": `echo "ERROR! syntax" >&2; exit 4`})
	if _, _, err := FetchHostPlayEntries("site.yml", nil, "h1"); err == nil || !strings.Contains(err.Error(), "syntax") {
		t.Errorf("a failing probe must surface as an error, not an empty tab: %v", err)
	}
}

func TestFetchHostPing(t *testing.T) {
	cases := []struct {
		name, script string
		want         bool
	}{
		{"ok for the host", `echo '{"_event":"v2_runner_on_ok","hosts":{"h1":{"ping":"pong"}}}'`, true},
		{"unreachable", `echo '{"_event":"v2_runner_on_unreachable","hosts":{"h1":{}}}'; exit 4`, false},
		{"failed", `echo '{"_event":"v2_runner_on_failed","hosts":{"h1":{}}}'; exit 2`, false},
		{"ok, but for another host", `echo '{"_event":"v2_runner_on_ok","hosts":{"h2":{}}}'`, false},
		{"garbage then ok", "echo garbage\necho '{\"_event\":\"v2_runner_on_ok\",\"hosts\":{\"h1\":{}}}'", true},
		{"no output at all", `exit 1`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeBin(t, map[string]string{"ansible-playbook": c.script})
			if got := FetchHostPing("/stub.yml", "h1", nil); got != c.want {
				t.Errorf("= %v, want %v", got, c.want)
			}
		})
	}
	t.Setenv("PATH", t.TempDir())
	if FetchHostPing("/stub.yml", "h1", nil) {
		t.Error("a missing ansible-playbook must read as unreachable")
	}
}

func TestRunHostStubEvents_Diagnostics(t *testing.T) {
	t.Run("stderr wins", func(t *testing.T) {
		fakeBin(t, map[string]string{"ansible-playbook": `echo "ERROR! bad host pattern" >&2; exit 4`})
		_, err := runHostStubEvents("/stub.yml", "h1", nil, nil)
		if err == nil || !strings.Contains(err.Error(), "bad host pattern") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("events only for other hosts are named", func(t *testing.T) {
		fakeBin(t, map[string]string{"ansible-playbook": `echo '{"_event":"v2_runner_on_ok","hosts":{"zed":{}}}'; echo junk`})
		_, err := runHostStubEvents("/stub.yml", "h1", nil, nil)
		if err == nil || !strings.Contains(err.Error(), "only ever for: zed") || !strings.Contains(err.Error(), "1 non-JSON") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("no events at all", func(t *testing.T) {
		fakeBin(t, map[string]string{"ansible-playbook": `true`})
		_, err := runHostStubEvents("/stub.yml", "h1", nil, nil)
		if err == nil || !strings.Contains(err.Error(), "0 jsonl events were parsed") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("extra env reaches the child and every host event is kept in order", func(t *testing.T) {
		fakeBin(t, map[string]string{"ansible-playbook": `echo "{\"_event\":\"v2_runner_on_ok\",\"hosts\":{\"h1\":{\"n\":\"$EXTRA\"}}}"
echo '{"_event":"v2_runner_on_ok","hosts":{"h1":{"n":"second"}}}'`})
		ev, err := runHostStubEvents("/stub.yml", "h1", nil, []string{"EXTRA=first"})
		if err != nil || len(ev) != 2 || !strings.Contains(string(ev[0]), "first") || !strings.Contains(string(ev[1]), "second") {
			t.Errorf("events %s, err %v", ev, err)
		}
	})
}

const setupOK = `echo '{"_event":"v2_runner_on_ok","hosts":{"h1":{"ansible_facts":{"ansible_fqdn":"h1.example.org","ansible_architecture":"x86_64"}}}}'`

func TestFetchHostFacts_LivePath(t *testing.T) {
	fakeBin(t, map[string]string{
		"ansible-inventory": `exit 1`, // no usable fact cache
		"ansible-playbook":  setupOK,
		"ansible-doc":       `exit 1`,
	})
	res, err := FetchHostFacts("/stub.yml", "h1", nil, false)
	if err != nil || res.prefix != "" || res.facts["ansible_fqdn"] != "h1.example.org" || !strings.Contains(res.characteristics, "h1.example.org") {
		t.Errorf("got %+v, %v", res, err)
	}
}

func TestFetchHostFacts_LiveOutcomesThatAreContentNotErrors(t *testing.T) {
	for _, c := range []struct{ event, want string }{
		{`{"_event":"v2_runner_on_unreachable","hosts":{"h1":{"unreachable":true,"msg":"no route <here>"}}}`, "Unreachable"},
		{`{"_event":"v2_runner_on_failed","hosts":{"h1":{"failed":true,"msg":"denied"}}}`, "Failed"},
	} {
		fakeBin(t, map[string]string{"ansible-inventory": `exit 1`, "ansible-playbook": "echo '" + c.event + "'"})
		res, err := FetchHostFacts("/stub.yml", "h1", nil, false)
		if err != nil || !strings.Contains(res.characteristics, c.want) || res.facts != nil {
			t.Errorf("%s: got %+v, %v", c.want, res, err)
		}
	}
}

func TestFetchHostFacts_LiveErrorIsAnError(t *testing.T) {
	fakeBin(t, map[string]string{"ansible-inventory": `exit 1`, "ansible-playbook": `echo "ERROR! no inventory" >&2; exit 4`})
	if _, err := FetchHostFacts("/stub.yml", "h1", nil, false); err == nil || !strings.Contains(err.Error(), "no inventory") {
		t.Errorf("err = %v", err)
	}
}

func TestFetchHostFacts_SkipLiveGatherNeverConnects(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "connected")
	fakeBin(t, map[string]string{"ansible-inventory": `exit 1`, "ansible-playbook": `touch ` + marker})
	res, err := FetchHostFacts("/stub.yml", "h1", nil, true)
	if err != nil || !strings.Contains(res.characteristics, "Host unavailable") {
		t.Errorf("got %+v, %v", res, err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("skipLiveGather still ran ansible-playbook")
	}
}

// A fresh fact cache is used without connecting, inventory-level variables
// that --host merges in are subtracted, and (with mount_points already in
// the cache) nothing else is asked of the host.
func TestFetchHostFacts_CacheHit(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "connected")
	fakeBin(t, map[string]string{
		"ansible-inventory": `if [ "$ANSIBLE_CACHE_PLUGIN" = memory ]; then echo '{"ansible_host":"10.0.0.1"}'
else echo '{"ansible_architecture":"x86_64","ansible_fqdn":"cached.example.org","ansible_host":"10.0.0.1","mount_points":{"/":{"mount":"/","size_total":1024}}}'; fi`,
		"ansible-playbook": `touch ` + marker,
	})
	res, err := FetchHostFacts("/stub.yml", "h1", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.prefix, "from fact cache") || res.facts["ansible_fqdn"] != "cached.example.org" {
		t.Errorf("got %+v", res)
	}
	if _, isVar := res.facts["ansible_host"]; isVar {
		t.Error("an inventory variable must not be shown as a fact")
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("a complete cache entry must not trigger a live gather")
	}
}

func TestFetchHostFacts_CacheMissingCanaryFallsThrough(t *testing.T) {
	fakeBin(t, map[string]string{"ansible-inventory": `echo '{"unrelated":1}'`, "ansible-playbook": setupOK, "ansible-doc": `exit 1`})
	res, err := FetchHostFacts("/stub.yml", "h1", nil, false)
	if err != nil || res.prefix != "" || res.facts["ansible_fqdn"] != "h1.example.org" {
		t.Errorf("got %+v, %v", res, err)
	}
}

func TestFetchHostVariables(t *testing.T) {
	fakeBin(t, map[string]string{"ansible-playbook": `echo '{"_event":"v2_runner_on_ok","hosts":{"h1":{"tangsible_variables":{"variables":{"b":2,"a":"x"},"origins":{}}}}}'`})
	got, err := FetchHostVariables("h1", nil)
	if err != nil || !strings.Contains(got, "Inventory-level variables") || strings.Index(got, "a:") > strings.Index(got, "b:") {
		t.Errorf("got %q, %v", got, err)
	}
	fakeBin(t, map[string]string{"ansible-playbook": `echo '{"_event":"v2_runner_on_failed","hosts":{"h1":{"failed":true,"msg":"plugin missing"}}}'`})
	if _, err := FetchHostVariables("h1", nil); err == nil || !strings.Contains(err.Error(), "plugin missing") {
		t.Errorf("err = %v", err)
	}
}

func TestHostStubs(t *testing.T) {
	for name, f := range map[string]func() (string, error){"summary": WriteHostSummaryStub, "ping": WriteHostPingStub} {
		p, err := f()
		if err != nil {
			t.Fatal(err)
		}
		defer os.Remove(p)
		b, _ := os.ReadFile(p)
		if !strings.Contains(string(b), "hosts:") || !strings.Contains(filepath.Base(p), name) {
			t.Errorf("%s stub %s: %s", name, p, b)
		}
	}
}
