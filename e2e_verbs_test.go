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

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Smoke tests for the standalone verbs that never build the live tree:
// template, host, hosts and inventory. Each starts the real binary against
// the real ansible, checks that its first screen shows something sensible,
// drives one or two keys, and quits with exit status 0 - enough to notice a
// verb whose wiring in session.Main broke, which no unit test sees.

const e2eVerbInventory = "[all]\nhost1 ansible_connection=local\nhost2 ansible_connection=local\n\n[web]\nhost1\n"

// startVerbSession runs "tangsible <args>" in workDir (holding files) under
// `sh run.sh`, which prints "exit=<status>" when tangsible ends. The command
// has the callback and gathering settings every e2e test needs.
func startVerbSession(t *testing.T, files map[string]string, args string) *tmuxSession {
	t.Helper()
	bin := buildE2EBinary(t)
	dir := t.TempDir()
	files["inventory.ini"] = e2eVerbInventory
	writeE2EFiles(t, dir, files)
	script := fmt.Sprintf("cd %s\nANSIBLE_GATHERING=explicit %s %s %s\necho \"exit=$?\"\n",
		shellQuote(dir), e2eCallbackEnv(t), shellQuote(bin), args)
	scriptPath := filepath.Join(t.TempDir(), "run.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	s := startTmuxSession(t)
	s.send("sh "+shellQuote(scriptPath), "Enter")
	return s
}

// quitWithStatus presses q and waits for the script's "exit=0".
func (s *tmuxSession) quitWithStatus(code int) {
	s.t.Helper()
	s.send("q")
	s.waitFor(fmt.Sprintf("exit=%d", code), 10*time.Second)
}

func TestE2E_Template_RendersAndShowsTheSource(t *testing.T) {
	requireE2ETools(t)
	s := startVerbSession(t, map[string]string{"t.j2": "Hello {{ inventory_hostname }}!\n"}, "template t.j2 host1 -i inventory.ini")
	s.waitFor("Hello host1!", 30*time.Second)
	s.send("Tab") // to the Source tab: the raw, unrendered file
	s.waitFor("{{ inventory_hostname }}", 5*time.Second)
	s.quitWithStatus(0)
}

func TestE2E_Template_RenderErrorIsShownInTheTab(t *testing.T) {
	requireE2ETools(t)
	s := startVerbSession(t, map[string]string{"t.j2": "{{ no_such_variable }}\n"}, "template t.j2 host1 -i inventory.ini")
	got := s.waitFor("undefined", 30*time.Second)
	if !strings.Contains(got, "no_such_variable") {
		t.Errorf("the error should name the variable:\n%s", got)
	}
	s.quitWithStatus(0)
}

func TestE2E_Template_GroupGivesATabPerHost(t *testing.T) {
	requireE2ETools(t)
	s := startVerbSession(t, map[string]string{"t.j2": "on {{ inventory_hostname }}\n"}, "template t.j2 all -i inventory.ini")
	got := s.waitFor("on host", 30*time.Second)
	for _, h := range []string{"host1", "host2"} {
		if !strings.Contains(got, h) {
			t.Errorf("no tab for %s:\n%s", h, got)
		}
	}
	s.quitWithStatus(0)
}

func TestE2E_Template_UnknownHostStopsBeforeTheTUI(t *testing.T) {
	requireE2ETools(t)
	s := startVerbSession(t, map[string]string{"t.j2": "x\n"}, "template t.j2 ghost -i inventory.ini")
	s.waitFor("not a known host or group", 30*time.Second)
	s.waitFor("exit=1", 5*time.Second)
}

func TestE2E_Host_ShowsSummaryAndGroups(t *testing.T) {
	requireE2ETools(t)
	s := startVerbSession(t, map[string]string{}, "host host1 -i inventory.ini")
	// "Plays" rather than "Summary": the latter is in the temp dir's name,
	// so it would match the command line before the TUI is up.
	got := s.waitFor("Plays", 30*time.Second)
	for _, tab := range []string{"Summary", "Facts", "Variables", "Groups"} {
		if !strings.Contains(got, tab) {
			t.Errorf("tab %q missing:\n%s", tab, got)
		}
	}
	s.waitFor("host1", 30*time.Second)
	s.send("Tab", "Tab", "Tab") // Facts, Variables, Groups
	s.waitFor("(direct)", 30*time.Second)
	s.quitWithStatus(0)
}

func TestE2E_Hosts_ListThenDetailAndBack(t *testing.T) {
	requireE2ETools(t)
	s := startVerbSession(t, map[string]string{}, "hosts -i inventory.ini")
	got := s.waitFor("host2", 30*time.Second)
	if !strings.Contains(got, "host1") {
		t.Errorf("host1 missing from the list:\n%s", got)
	}
	s.send("Enter")
	s.waitFor("Plays", 30*time.Second)
	s.send("Escape") // back to the list
	s.waitForAbsence("Plays", 5*time.Second)
	s.quitWithStatus(0)
}

func TestE2E_Inventory_TreeExpandsAndOpensAHost(t *testing.T) {
	requireE2ETools(t)
	s := startVerbSession(t, map[string]string{}, "inventory -i inventory.ini")
	got := s.waitFor("web", 30*time.Second)
	if !strings.Contains(got, "all") {
		t.Errorf("group tree lacks 'all':\n%s", got)
	}
	s.send("E") // expand every group
	s.waitFor("host2", 5*time.Second)
	s.send("G") // the last row of an expanded tree is a host
	s.send("Enter")
	s.waitFor("Plays", 30*time.Second)
	s.send("Escape")
	s.waitForAbsence("Plays", 5*time.Second)
	s.quitWithStatus(0)
}
