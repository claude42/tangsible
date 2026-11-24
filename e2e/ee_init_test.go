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

//go:build e2e

// End-to-end tests for `tangsible ee init` (design-docs/EEInit.md). The
// container engine is a fake `podman` on PATH that answers the two calls init
// makes (does the image exist, and the probe script) and logs them, so no
// podman or image is needed; the host's ansible-doc is real. Nothing here
// touches the network (--offline) and no TUI is involved, so no tmux either.
package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakePodman answers `image exists` with success and the probe (`run ...
// probe <binaries>`) with a Fedora-like image that lacks every binary asked
// about except `ls`; every call is appended to the log.
const fakePodman = `#!/bin/sh
echo "$*" >> "$FAKE_PODMAN_LOG"
case "$1" in
image) exit 0 ;;
run)
  echo "os=Fedora Linux 44"
  echo "pkg=/usr/bin/microdnf"
  echo "python=/usr/bin/python3"
  echo "python_real=/usr/bin/python3.14"
  echo "python_pkg=python3"
  echo "python_version=3.14.8"
  echo "pip=yes"
  echo "ansible_core=2.21.5"
  seen=
  for a in "$@"; do
    if [ -n "$seen" ]; then
      case "$a" in ls) ;; *) echo "missing=$a" ;; esac
    fi
    if [ "$a" = probe ]; then seen=1; fi
  done
  ;;
esac
exit 0
`

// eeInitEnv is a project directory plus a runner for `tangsible ee init`
// with the fake podman first on PATH and an empty config and cache.
type eeInitEnv struct {
	t    *testing.T
	dir  string
	log  string
	bin  string
	path string
}

func newEEInitEnv(t *testing.T, playbook string) *eeInitEnv {
	t.Helper()
	if _, err := exec.LookPath("ansible-doc"); err != nil {
		t.Skip("ansible-doc not found on PATH - skipping ee init e2e test")
	}
	root := t.TempDir()
	fakeBin := filepath.Join(root, "bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fakeBin, "podman"), []byte(fakePodman), 0o755); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "project")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "site.yml"), []byte(playbook), 0o644); err != nil {
		t.Fatal(err)
	}
	return &eeInitEnv{
		t: t, dir: dir, log: filepath.Join(root, "podman.log"), bin: buildE2EBinary(t),
		path: fakeBin + string(os.PathListSeparator) + os.Getenv("PATH"),
	}
}

// run returns stdout, stderr and the exit code.
func (e *eeInitEnv) run(args ...string) (stdout, stderr string, code int) {
	e.t.Helper()
	cmd := exec.Command(e.bin, append([]string{"ee", "init"}, args...)...)
	cmd.Dir = e.dir
	home := filepath.Join(filepath.Dir(e.dir), "home")
	cmd.Env = append(os.Environ(),
		"PATH="+e.path, "FAKE_PODMAN_LOG="+e.log,
		"HOME="+home, "XDG_CONFIG_HOME="+filepath.Join(home, "config"), "XDG_CACHE_HOME="+filepath.Join(home, "cache"),
		"TANGSIBLE_PLAYBOOK=", "ANSIBLE_HOME="+filepath.Join(home, "ansible"),
	)
	var so, se strings.Builder
	cmd.Stdout, cmd.Stderr = &so, &se
	if err := cmd.Run(); err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			e.t.Fatal(err)
		}
		code = ee.ExitCode()
	}
	return so.String(), se.String(), code
}

func (e *eeInitEnv) draft() string {
	e.t.Helper()
	b, err := os.ReadFile(filepath.Join(e.dir, "execution-environment.yml"))
	if err != nil {
		e.t.Fatal(err)
	}
	return string(b)
}

func (e *eeInitEnv) podmanLog() string {
	b, _ := os.ReadFile(e.log)
	return string(b)
}

const eeInitPlaybook = `- hosts: localhost
  gather_facts: false
  tasks:
    - ansible.builtin.debug: {msg: hi}
    - name: controller-side
      command: jq .
      delegate_to: localhost
`

func TestE2E_EEInit_WritesADraftFromTheFakeImage(t *testing.T) {
	e := newEEInitEnv(t, eeInitPlaybook)
	out, errOut, code := e.run("--offline", "--base", "fake.example/base:1", "-t", "my-ee:1", "-t", "my-ee:latest", "site.yml")
	if code != 0 {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, out, errOut)
	}
	if !strings.Contains(out, "Created execution-environment.yml.") {
		t.Errorf("summary:\n%s", out)
	}
	def := e.draft()
	for _, want := range []string{
		"name: fake.example/base:1",           // the --base image
		"    - my-ee:1\n    - my-ee:latest\n", // -t: a real tags list
		"ansible-core 2.21.5",                 // what the probe reported
		"jq",                                  // the binary the base lacks
	} {
		if !strings.Contains(def, want) {
			t.Errorf("draft lacks %q:\n%s", want, def)
		}
	}
	if strings.Contains(def, "built-in default") {
		t.Errorf("an explicit --base is not the built-in default:\n%s", def)
	}
	if log := e.podmanLog(); !strings.Contains(log, "image exists fake.example/base:1") || !strings.Contains(log, "probe jq") {
		t.Errorf("podman calls:\n%s", log)
	}
}

func TestE2E_EEInit_OfflineUsesTheBuiltInDefault(t *testing.T) {
	e := newEEInitEnv(t, eeInitPlaybook)
	if out, errOut, code := e.run("--offline", "site.yml"); code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	def := e.draft()
	if !strings.Contains(def, "built-in default") || !strings.Contains(def, "ghcr.io/ansible-community/community-ee-base:") {
		t.Errorf("draft:\n%s", def)
	}
	if strings.Contains(def, "# TODO: could not list") {
		t.Errorf("--offline is not a failed lookup:\n%s", def)
	}
}

func TestE2E_EEInit_RerunBacksUpAndLeavesIdenticalAlone(t *testing.T) {
	e := newEEInitEnv(t, eeInitPlaybook)
	args := []string{"--offline", "--base", "b:1", "site.yml"}
	if _, errOut, code := e.run(args...); code != 0 {
		t.Fatalf("first: %d %s", code, errOut)
	}

	out, _, code := e.run(args...)
	if code != 0 || !strings.Contains(out, "execution-environment.yml is unchanged.") {
		t.Errorf("identical rerun: %d\n%s", code, out)
	}
	if m, _ := filepath.Glob(filepath.Join(e.dir, "execution-environment.yml.*")); len(m) != 0 {
		t.Errorf("an unchanged draft made backups: %v", m)
	}

	out, _, code = e.run("--offline", "--base", "b:2", "site.yml")
	if code != 0 || !strings.Contains(out, "The previous file is saved as execution-environment.yml.1.") {
		t.Errorf("changed rerun: %d\n%s", code, out)
	}
	old, err := os.ReadFile(filepath.Join(e.dir, "execution-environment.yml.1"))
	if err != nil || !strings.Contains(string(old), "name: b:1") || !strings.Contains(e.draft(), "name: b:2") {
		t.Errorf("backup %v\n%s", err, old)
	}

	out, _, code = e.run("--offline", "--base", "b:3", "--no-backup", "site.yml")
	if code != 0 || !strings.Contains(out, "replaced without a backup") {
		t.Errorf("--no-backup: %d\n%s", code, out)
	}
	if m, _ := filepath.Glob(filepath.Join(e.dir, "execution-environment.yml.*")); len(m) != 1 {
		t.Errorf("--no-backup touched the existing backups: %v", m)
	}
}

func TestE2E_EEInit_UsageErrorsAndMissingPlaybook(t *testing.T) {
	e := newEEInitEnv(t, eeInitPlaybook)
	if _, errOut, code := e.run("--offline", "--latest"); code != 2 || !strings.Contains(errOut, "contradict") {
		t.Errorf("--offline --latest: %d\n%s", code, errOut)
	}
	if _, errOut, code := e.run("--no-galaxy"); code != 2 || !strings.Contains(errOut, "unknown option --no-galaxy") {
		t.Errorf("the old flag: %d\n%s", code, errOut)
	}
	if _, errOut, code := e.run("--offline", "nope.yml"); code != 1 || !strings.Contains(errOut, "nope.yml") {
		t.Errorf("missing playbook: %d\n%s", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(e.dir, "execution-environment.yml")); err == nil {
		t.Error("a failed run wrote a draft")
	}
}
