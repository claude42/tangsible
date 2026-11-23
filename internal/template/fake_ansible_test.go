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
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeBin installs a shell script named name at the front of PATH (and
// points the callback plugin's extraction cache at a temp dir), so code that
// shells out to ansible-playbook/ansible-inventory runs against canned
// output instead of a real ansible.
func fakeBin(t *testing.T, name, script string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
}

const inventoryJSON = `{"_meta":{"hostvars":{}},"all":{"children":["web"]},"web":{"hosts":["h1","h2"]}}`

func TestRenderTemplate_OKReadsTheRenderedFile(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out")
	args := filepath.Join(t.TempDir(), "args")
	os.WriteFile(out, []byte("rendered!\n"), 0o600)
	fakeBin(t, "ansible-playbook", `echo "$@" > `+args+`
echo '{"_event":"v2_runner_on_ok","hosts":{"h1":{"changed":true}}}'`)

	res, err := RenderTemplate(context.Background(), "/stub.yml", out, "h1", []string{"-e", "x=1"})
	if err != nil || res.Failed || res.Content != "rendered!\n" {
		t.Fatalf("got %+v, %v", res, err)
	}
	got, _ := os.ReadFile(args)
	if strings.TrimSpace(string(got)) != "/stub.yml --limit h1 -e x=1" {
		t.Errorf("args = %q: must be stub, --limit host, then the passthrough args", got)
	}
}

func TestRenderTemplate_TaskFailureIsAResultNotAnError(t *testing.T) {
	fakeBin(t, "ansible-playbook", `echo '{"_event":"v2_runner_on_failed","hosts":{"h1":{"failed":true,"msg":"AnsibleUndefinedVariable: foo is undefined"}}}'
exit 2`)
	res, err := RenderTemplate(context.Background(), "/stub.yml", "/nonexistent/out", "h1", nil)
	if err != nil || !res.Failed || !strings.Contains(res.ErrMsg, "foo is undefined") {
		t.Fatalf("got %+v, %v", res, err)
	}
}

func TestRenderTemplate_UnreachableHostIsAFailedResult(t *testing.T) {
	fakeBin(t, "ansible-playbook", `echo '{"_event":"v2_runner_on_unreachable","hosts":{"h1":{"unreachable":true,"msg":"no route"}}}'`)
	res, err := RenderTemplate(context.Background(), "/stub.yml", "/nonexistent/out", "h1", nil)
	if err != nil || !res.Failed {
		t.Fatalf("got %+v, %v", res, err)
	}
}

func TestRenderTemplate_SuccessButUnreadableOutput(t *testing.T) {
	fakeBin(t, "ansible-playbook", `echo '{"_event":"v2_runner_on_ok","hosts":{"h1":{}}}'`)
	res, err := RenderTemplate(context.Background(), "/stub.yml", filepath.Join(t.TempDir(), "gone"), "h1", nil)
	if err != nil || !res.Failed || !strings.Contains(res.ErrMsg, "couldn't be read") {
		t.Fatalf("got %+v, %v", res, err)
	}
}

func TestRenderTemplate_OnlyTheNamedHostsEventCounts(t *testing.T) {
	fakeBin(t, "ansible-playbook", `echo '{"_event":"v2_runner_on_ok","hosts":{"other":{}}}'`)
	_, err := RenderTemplate(context.Background(), "/stub.yml", "/x", "h1", nil)
	if err == nil || !strings.Contains(err.Error(), `no result reported for host "h1"`) {
		t.Errorf("err = %v", err)
	}
}

func TestRenderTemplate_PreflightFailureSurfacesStderr(t *testing.T) {
	fakeBin(t, "ansible-playbook", `echo "ERROR! the inventory is broken" >&2; exit 4`)
	_, err := RenderTemplate(context.Background(), "/stub.yml", "/x", "h1", nil)
	if err == nil || !strings.Contains(err.Error(), "the inventory is broken") {
		t.Errorf("err = %v", err)
	}
}

func TestRenderTemplate_MissingBinary(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	_, err := RenderTemplate(context.Background(), "/stub.yml", "/x", "h1", nil)
	if err == nil || !strings.Contains(err.Error(), "ansible-playbook not found") {
		t.Errorf("err = %v", err)
	}
}

func TestRenderTemplate_CancelStopsTheRender(t *testing.T) {
	fakeBin(t, "ansible-playbook", `exec sleep 30`)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := RenderTemplate(ctx, "/stub.yml", "/x", "h1", nil)
		done <- err
	}()
	cancel()
	if err := <-done; err == nil {
		t.Error("a cancelled render with no result must be an error")
	}
}

func TestResolveHostTokens_ViaInventory(t *testing.T) {
	fakeBin(t, "ansible-inventory", "echo '"+inventoryJSON+"'")
	got, err := ResolveHostTokens([]string{"web"}, nil)
	if err != nil || strings.Join(got, ",") != "h1,h2" {
		t.Errorf("got %v, %v", got, err)
	}
	if _, err := ResolveHostTokens([]string{"nope"}, nil); err == nil {
		t.Error("an unknown token must be an error")
	}
}

func TestResolveHostTokens_InventoryFailure(t *testing.T) {
	fakeBin(t, "ansible-inventory", `echo "boom" >&2; exit 1`)
	if _, err := ResolveHostTokens([]string{"web"}, nil); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("err = %v", err)
	}
}

func TestRemoveScratchDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "scratch")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "out"), []byte("x"), 0o600)
	os.WriteFile(filepath.Join(dir, "stub-1.yml"), []byte("x"), 0o600)
	removeScratchDir(dir)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("scratch dir survived: %v", err)
	}
	removeScratchDir(dir) // already gone: no panic, no hang
}

// A write after the directory is gone must fail instead of recreating it:
// that is what keeps an interrupted render from leaking its output file.
func TestScratchDirGoneMeansLateWriteFails(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "scratch")
	os.MkdirAll(dir, 0o700)
	removeScratchDir(dir)
	if err := os.WriteFile(filepath.Join(dir, "out"), []byte("late"), 0o600); err == nil {
		t.Error("late write into a removed scratch dir succeeded")
	}
}

// captureStderr runs f with os.Stderr redirected, returning what it wrote.
func captureStderr(t *testing.T, f func()) string {
	t.Helper()
	r, w, _ := os.Pipe()
	old := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = old }()
	f()
	w.Close()
	var b strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		b.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return b.String()
}

func TestRunTemplateVerb_FailuresBeforeTheTUI(t *testing.T) {
	tmpl := filepath.Join(t.TempDir(), "t.j2")
	os.WriteFile(tmpl, []byte("hi"), 0o600)
	cwd, _ := os.Getwd()
	t.Chdir(t.TempDir()) // no project config
	defer os.Chdir(cwd)

	cases := []struct {
		name string
		args []string
		inv  string // ansible-inventory script; "" = none installed
		code int
		msg  string
	}{
		{"no arguments", nil, "", 2, ""},
		{"template missing", []string{"/nonexistent.j2"}, "", 1, "couldn't read template"},
		{"inventory fails", []string{tmpl}, `echo nope >&2; exit 1`, 1, "couldn't resolve a host"},
		{"empty inventory", []string{tmpl}, `echo '{"_meta":{"hostvars":{}},"all":{"children":[]}}'`, 1, "no hosts found"},
		{"unknown host", []string{tmpl, "ghost"}, "echo '" + inventoryJSON + "'", 1, "not a known host or group"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.inv != "" {
				fakeBin(t, "ansible-inventory", c.inv)
			}
			var code int
			msg := captureStderr(t, func() { code = RunTemplateVerb(c.args) })
			if code != c.code || !strings.Contains(msg, c.msg) {
				t.Errorf("code %d (want %d), stderr %q (want %q)", code, c.code, msg, c.msg)
			}
		})
	}
}

func TestRunTemplateVerb_ManyHostsNeedConfirmation(t *testing.T) {
	tmpl := filepath.Join(t.TempDir(), "t.j2")
	os.WriteFile(tmpl, []byte("hi"), 0o600)
	t.Chdir(t.TempDir())
	var hosts []string
	for i := 0; i < 11; i++ { // the default limit is 10
		hosts = append(hosts, `"h`+string(rune('a'+i))+`"`)
	}
	fakeBin(t, "ansible-inventory", `echo '{"_meta":{"hostvars":{}},"all":{"children":["g"]},"g":{"hosts":[`+strings.Join(hosts, ",")+`]}}'`)

	in, w, _ := os.Pipe()
	w.WriteString("n\n")
	w.Close()
	oldIn := os.Stdin
	os.Stdin = in
	defer func() { os.Stdin = oldIn }()

	var code int
	msg := captureStderr(t, func() { code = RunTemplateVerb([]string{tmpl, "g"}) })
	if code != 0 || !strings.Contains(msg, "cancelled") {
		t.Errorf("declining must cancel with exit 0: code %d, stderr %q", code, msg)
	}
}
