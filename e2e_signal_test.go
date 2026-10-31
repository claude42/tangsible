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

// End-to-end tests for SIGTERM/SIGHUP handling in the live session
// (run/rerun/role/debug): the signal must take the normal quit path -
// the running ansible-playbook (or container) is stopped, scratch files
// are removed, and tangsible exits 128+signal - instead of ending the
// process on the spot and orphaning its child.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// pgrepChildren returns pid's direct children.
func pgrepChildren(pid int) []int {
	out, _ := exec.Command("pgrep", "-P", strconv.Itoa(pid)).Output()
	var pids []int
	for _, f := range strings.Fields(string(out)) {
		if n, err := strconv.Atoi(f); err == nil {
			pids = append(pids, n)
		}
	}
	return pids
}

func procComm(pid int) string {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// findDescendant walks the process tree under root (breadth first) for the
// first process whose comm starts with prefix (comm is cut at 15 chars).
func findDescendant(root int, prefix string) int {
	queue := []int{root}
	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]
		for _, c := range pgrepChildren(pid) {
			if strings.HasPrefix(procComm(c), prefix) {
				return c
			}
			queue = append(queue, c)
		}
	}
	return 0
}

func (s *tmuxSession) panePID() int {
	s.t.Helper()
	out, err := exec.Command("tmux", "list-panes", "-t", s.name, "-F", "#{pane_pid}").Output()
	if err != nil {
		s.t.Fatalf("tmux list-panes: %v", err)
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return n
}

// waitProc polls until a descendant of root with the comm prefix exists.
func waitProc(t *testing.T, root int, prefix string, timeout time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if pid := findDescendant(root, prefix); pid != 0 {
			return pid
		}
		if time.Now().After(deadline) {
			t.Fatalf("no %q process under %d after %s", prefix, root, timeout)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func waitGone(t *testing.T, pid int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			t.Fatalf("process %d (%s) still alive after %s", pid, procComm(pid), timeout)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// startSlowSession runs a playbook that sleeps for two minutes under
// `sh run.sh` in a tmux pane, which prints "exit=<status>" when tangsible
// ends. tmpDir is TMPDIR for tangsible alone. It returns the session and
// tangsible's pid, once its TUI is up.
func startSlowSession(t *testing.T, tmpDir string, extraArgs ...string) (*tmuxSession, int) {
	t.Helper()
	bin := buildE2EBinary(t)
	dir := t.TempDir()
	slow := "- hosts: localhost\n  gather_facts: false\n  connection: local\n  tasks:\n    - name: slow\n      command: sleep 120\n"
	if err := os.WriteFile(filepath.Join(dir, "slow.yml"), []byte(slow), 0o644); err != nil {
		t.Fatal(err)
	}
	args := ""
	for _, a := range extraArgs {
		args += " " + shellQuote(a)
	}
	script := fmt.Sprintf("cd %s\nTMPDIR=%s %s %s run slow.yml -i localhost,%s\necho \"exit=$?\"\n",
		shellQuote(dir), shellQuote(tmpDir), e2eCallbackEnv(t), shellQuote(bin), args)
	scriptPath := filepath.Join(t.TempDir(), "run.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	s := startTmuxSession(t)
	s.send("sh "+shellQuote(scriptPath), "Enter")
	s.waitFor("Filter:", 60*time.Second) // the top bar: the TUI is up
	return s, waitProc(t, s.panePID(), "tangsible", 5*time.Second)
}

func TestE2E_Signals_StopTheRunningPlaybook(t *testing.T) {
	requireE2ETools(t)
	for _, c := range []struct {
		sig  syscall.Signal
		code int
	}{{syscall.SIGTERM, 143}, {syscall.SIGHUP, 129}} {
		t.Run(c.sig.String(), func(t *testing.T) {
			s, pid := startSlowSession(t, t.TempDir())
			ansible := waitProc(t, pid, "ansible-playbo", 30*time.Second)

			if err := syscall.Kill(pid, c.sig); err != nil {
				t.Fatal(err)
			}
			s.waitFor(fmt.Sprintf("exit=%d", c.code), 20*time.Second)
			waitGone(t, ansible, 10*time.Second)
		})
	}
}
