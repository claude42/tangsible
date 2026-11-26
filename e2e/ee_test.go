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

// End-to-end tests for execution environments
// (design-docs/ExecutionEnvironments.md), driving the real binary in a tmux
// pane like e2e/rerun_test.go. They need rootless podman and an image that
// is already pulled (nothing here pulls one), named by
// TANGSIBLE_EE_IMAGE - skipped otherwise:
//
//	TANGSIBLE_EE_IMAGE=ghcr.io/ansible-community/community-ee-base:latest \
//	    go test -tags e2e -run EE ./e2e
//
// Each test works in its own temp directory holding a copy of the playbook:
// the project mount root is where the working directory and the playbook
// meet, so a playbook elsewhere on the disk (the repo's testdata/, with the
// temp dir as cwd) would put the root at "/", which tangsible rejects.
package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func requireEE(t *testing.T) (image string) {
	t.Helper()
	requireE2ETools(t)
	image = os.Getenv("TANGSIBLE_EE_IMAGE")
	if image == "" {
		t.Skip("TANGSIBLE_EE_IMAGE not set - skipping execution environment e2e test")
	}
	if _, err := exec.LookPath("podman"); err != nil {
		t.Skip("podman not found on PATH - skipping execution environment e2e test")
	}
	if err := exec.Command("podman", "image", "exists", image).Run(); err != nil {
		t.Skipf("image %s is not pulled locally - skipping", image)
	}
	return image
}

// eeProject makes a temp working directory holding playbookName, a copy of
// the repo's testdata/<src>, and returns the directory.
func eeProject(t *testing.T, src, playbookName string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "testdata", src))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, playbookName), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// tangsibleContainers lists the names of tangsible's own containers still
// known to podman (running or not); --rm means none should survive a run.
func tangsibleContainers(t *testing.T) []string {
	t.Helper()
	out, err := exec.Command("podman", "ps", "-a", "--filter", "name=tangsible-", "--format", "{{.Names}}").Output()
	if err != nil {
		t.Fatalf("podman ps: %v", err)
	}
	return strings.Fields(string(out))
}

// waitNoContainers polls until no tangsible container remains.
func waitNoContainers(t *testing.T, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		left := tangsibleContainers(t)
		if len(left) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("containers still present after %s: %v", timeout, left)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// shortImage mirrors the top bar's own shortening (ee.ShortImageName) for
// the only shape these tests use: registry/path/name:tag.
func shortImage(image string) string {
	if i := strings.LastIndex(image, "/"); i >= 0 {
		return image[i+1:]
	}
	return image
}

// tabsToEEField is how many Tabs from the dialog's first field reach the
// Execution environment field: Play, Tags, Skip tags, Hosts, then whichever
// of "Resume where failed" (between Play and Tags), "Only failed" and "Only
// unreachable" the dialog shows for the run just finished.
func tabsToEEField(dialog string) int {
	n := 4
	for _, box := range []string{"Resume where failed", "Only failed", "Only unreachable"} {
		if strings.Contains(dialog, box) {
			n++
		}
	}
	return n
}

func (s *tmuxSession) tabs(n int) {
	s.t.Helper()
	for i := 0; i < n; i++ {
		s.send("Tab")
	}
}

func TestE2E_EE_RunShowsIndicatorAndCompletes(t *testing.T) {
	image := requireEE(t)
	bin := buildE2EBinary(t)
	dir := eeProject(t, "outcomes.yml", "outcomes.yml")

	s := startTmuxSession(t)
	s.send(fmt.Sprintf("cd %s && %s %s run outcomes.yml -i localhost, --ee %s",
		shellQuote(dir), e2eCallbackEnv(t), shellQuote(bin), shellQuote(image)), "Enter")
	got := s.waitFor("Playbook completed successfully", 60*time.Second)

	if want := "(EE: " + shortImage(image) + ")"; !strings.Contains(lineContaining(got, "Playbook:"), want) {
		t.Errorf("top bar = %q, want it to contain %q", lineContaining(got, "Playbook:"), want)
	}
	// The same four buckets a host run reports for outcomes.yml - events
	// really did arrive over the FIFO.
	if line := lineContaining(got, "ok="); !strings.Contains(line, "changed=2") || !strings.Contains(line, "failed=1") {
		t.Errorf("summary = %q, want changed=2 and failed=1", line)
	}

	s.send("q")
	waitNoContainers(t, 10*time.Second)
}

func TestE2E_EE_RerunDialogSwitchesToHostAndBack(t *testing.T) {
	image := requireEE(t)
	bin := buildE2EBinary(t)
	dir := eeProject(t, "outcomes.yml", "outcomes.yml")

	s := startTmuxSession(t)
	s.send(fmt.Sprintf("cd %s && %s %s run outcomes.yml -i localhost, --ee %s",
		shellQuote(dir), e2eCallbackEnv(t), shellQuote(bin), shellQuote(image)), "Enter")
	s.waitFor("Playbook completed successfully", 60*time.Second)

	// Open the dialog; the EE field starts as the image this session runs in.
	s.send("r")
	dialog := s.waitFor("Re-run (enter: run, esc: cancel, F1: help)", 5*time.Second)
	s.waitForFieldValue("Execution environment:", "ghcr", 3*time.Second) // the field is narrower than the image

	// Move to the EE field, clear it, run on the host.
	s.tabs(tabsToEEField(dialog))
	s.send("C-u")
	s.waitForFieldValue("Execution environment:", "", 3*time.Second)
	s.send("Enter")
	// The previous generation's banner is still on screen until the new
	// generation clears the tree.
	s.waitForAbsence("Playbook completed successfully", 10*time.Second)
	got := s.waitFor("Playbook completed successfully", 30*time.Second)
	if strings.Contains(lineContaining(got, "Playbook:"), "(EE:") {
		t.Errorf("host re-run still shows the EE indicator: %q", lineContaining(got, "Playbook:"))
	}
	if len(tangsibleContainers(t)) != 0 {
		t.Errorf("a host re-run left containers: %v", tangsibleContainers(t))
	}

	// And back into the environment by typing the image.
	s.send("r")
	dialog = s.waitFor("Re-run (enter: run, esc: cancel, F1: help)", 5*time.Second)
	s.tabs(tabsToEEField(dialog))
	s.send(image)
	s.send("Enter")
	s.waitForAbsence("Playbook completed successfully", 10*time.Second)
	s.waitFor("(EE: "+shortImage(image)+")", 30*time.Second)
	s.waitFor("Playbook completed successfully", 30*time.Second)

	s.send("q")
	waitNoContainers(t, 10*time.Second)
}

// The environment each run used is recorded, so a bare "rerun" in a fresh
// process reuses it - and --no-ee overrides that.
func TestE2E_EE_RerunVerbReusesRecordedEnvironment(t *testing.T) {
	image := requireEE(t)
	bin := buildE2EBinary(t)
	dir := eeProject(t, "outcomes.yml", "outcomes.yml")
	cd := fmt.Sprintf("cd %s && %s ", shellQuote(dir), e2eCallbackEnv(t))

	s := startTmuxSession(t)
	s.send(cd+fmt.Sprintf("%s run outcomes.yml -i localhost, --ee %s", shellQuote(bin), shellQuote(image)), "Enter")
	s.waitFor("Playbook completed successfully", 60*time.Second)
	s.send("q")
	s.waitFor("$", 5*time.Second) // the shell prompt is back

	s.send("clear", "Enter")
	s.send(fmt.Sprintf("%s rerun --no-dialog", shellQuote(bin)), "Enter")
	got := s.waitFor("Playbook completed successfully", 60*time.Second)
	if !strings.Contains(lineContaining(got, "Playbook:"), "(EE: "+shortImage(image)+")") {
		t.Errorf("rerun lost the recorded environment: %q", lineContaining(got, "Playbook:"))
	}
	s.send("q")

	s.send("clear", "Enter")
	s.send(fmt.Sprintf("%s rerun --no-dialog --no-ee", shellQuote(bin)), "Enter")
	got = s.waitFor("Playbook completed successfully", 30*time.Second)
	if strings.Contains(lineContaining(got, "Playbook:"), "(EE:") {
		t.Errorf("--no-ee still ran in the environment: %q", lineContaining(got, "Playbook:"))
	}
	s.send("q")
	waitNoContainers(t, 10*time.Second)
}

// Ctrl-C reaches ansible inside the container, and the container goes away.
func TestE2E_EE_CtrlCStopsPlaybookAndRemovesContainer(t *testing.T) {
	image := requireEE(t)
	bin := buildE2EBinary(t)
	dir := t.TempDir()
	slow := "- hosts: localhost\n  gather_facts: false\n  connection: local\n  tasks:\n    - name: slow\n      command: sleep 120\n"
	if err := os.WriteFile(filepath.Join(dir, "slow.yml"), []byte(slow), 0o644); err != nil {
		t.Fatal(err)
	}

	s := startTmuxSession(t)
	s.send(fmt.Sprintf("cd %s && %s %s run slow.yml -i localhost, --ee %s",
		shellQuote(dir), e2eCallbackEnv(t), shellQuote(bin), shellQuote(image)), "Enter")
	// "Filter:" is in the top bar only: the typed command line also contains
	// "slow", and a Ctrl-C before the TUI exists would just kill the process.
	s.waitFor("Filter:", 60*time.Second)
	deadline := time.Now().Add(20 * time.Second)
	for len(tangsibleContainers(t)) == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("no container appeared; pane:\n%s", s.capture())
		}
		time.Sleep(200 * time.Millisecond)
	}

	s.send("C-c")
	s.waitFor("Playbook stopped", 20*time.Second)
	waitNoContainers(t, 10*time.Second)
	s.send("q")
}

// An inventory file outside the project: Ansible reads group_vars/host_vars
// from the inventory file's own directory, so the container must get them
// too. (Silently undefined before they were mounted - a host run had them.)
func TestE2E_EE_InventoryOutsideProjectKeepsItsGroupVars(t *testing.T) {
	image := requireEE(t)
	bin := buildE2EBinary(t)
	work := t.TempDir()
	inv := t.TempDir()
	for path, content := range map[string]string{
		filepath.Join(work, "p.yml"): "- hosts: all\n  gather_facts: false\n  tasks:\n" +
			"    - name: record what the inventory's neighbours provide\n" +
			"      ansible.builtin.copy:\n" +
			"        content: \"group={{ marker | default('UNDEFINED') }} host={{ hostmarker | default('UNDEFINED') }}\\n\"\n" +
			"        dest: \"{{ playbook_dir }}/out.txt\"\n",
		filepath.Join(inv, "inventory.ini"):              "[all]\nlocalhost ansible_connection=local\n",
		filepath.Join(inv, "group_vars", "all.yml"):      "marker: from-group-vars\n",
		filepath.Join(inv, "host_vars", "localhost.yml"): "hostmarker: from-host-vars\n",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	s := startTmuxSession(t)
	s.send(fmt.Sprintf("cd %s && %s %s run p.yml -i %s --ee %s",
		shellQuote(work), e2eCallbackEnv(t), shellQuote(bin), shellQuote(filepath.Join(inv, "inventory.ini")), shellQuote(image)), "Enter")
	s.waitFor("Playbook completed successfully", 60*time.Second)
	got, err := os.ReadFile(filepath.Join(work, "out.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "group=from-group-vars host=from-host-vars\n"; string(got) != want {
		t.Errorf("the container saw %q, want %q", got, want)
	}
	s.send("q")
	waitNoContainers(t, 10*time.Second)
}

// A path option written with a variable is expanded on the host, as Ansible
// does there; the container knows neither the host's variables nor its home,
// so it would otherwise look for a literal "$NAME/...". (The same code expands
// a leading "~" - tested in internal/ee; an e2e test would have to write into
// the real home directory.) The variable exists only in tangsible's own
// environment.
func TestE2E_EE_VariableInPathOptionIsExpandedOnTheHost(t *testing.T) {
	image := requireEE(t)
	bin := buildE2EBinary(t)
	work := t.TempDir()
	vars := t.TempDir() // outside the project: it has to be mounted as well
	if err := os.WriteFile(filepath.Join(vars, "vars.yml"), []byte("marker: from-a-file-named-with-a-variable\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	play := "- hosts: localhost\n  gather_facts: false\n  connection: local\n  tasks:\n" +
		"    - ansible.builtin.copy:\n" +
		"        content: \"marker={{ marker | default('UNDEFINED') }}\\n\"\n" +
		"        dest: \"{{ playbook_dir }}/out.txt\"\n"
	if err := os.WriteFile(filepath.Join(work, "p.yml"), []byte(play), 0o644); err != nil {
		t.Fatal(err)
	}

	s := startTmuxSession(t)
	s.send(fmt.Sprintf("cd %s && VARSDIR=%s %s %s run p.yml -i localhost, -e '@$VARSDIR/vars.yml' --ee %s",
		shellQuote(work), shellQuote(vars), e2eCallbackEnv(t), shellQuote(bin), shellQuote(image)), "Enter")
	s.waitFor("Completed", 60*time.Second)
	got, err := os.ReadFile(filepath.Join(work, "out.txt"))
	if err != nil {
		t.Fatalf("the playbook did not run (no out.txt); pane:\n%s", s.capture())
	}
	if want := "marker=from-a-file-named-with-a-variable\n"; string(got) != want {
		t.Errorf("the container saw %q, want %q", got, want)
	}
	s.send("q")
	waitNoContainers(t, 10*time.Second)
}

// The pull policy decides before anything starts, and an image that cannot be
// had stops tangsible with a hint instead of a registry error. Needs the
// podman binary (it asks it whether the image exists) but no image: nothing
// here exists, and nothing may be pulled.
func TestE2E_EE_PullPolicyStopsWithAHint(t *testing.T) {
	requireE2ETools(t)
	if _, err := exec.LookPath("podman"); err != nil {
		t.Skip("podman not found on PATH")
	}
	bin := buildE2EBinary(t)
	work := t.TempDir()
	if err := os.MkdirAll(filepath.Join(work, ".tangsible"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "p.yml"), []byte("- hosts: localhost\n  gather_facts: false\n  tasks: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct{ name, image, extra, want string }{
		{"a local-only image that is not there", "localhost/tangsible-e2e-no-such-image:1", "", "images under localhost/ exist only locally"},
		{"never, and the image is not there", "example.invalid/tangsible/none:1", `pull_policy = "never"`, `pull_policy is "never"`},
	}
	for _, c := range cases {
		cfg := fmt.Sprintf("[execution_environment]\nimage = %q\n%s\n", c.image, c.extra)
		if err := os.WriteFile(filepath.Join(work, ".tangsible", "config.toml"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(bin, "run", "p.yml", "-i", "localhost,")
		cmd.Dir = work
		out, err := cmd.CombinedOutput()
		if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 1 {
			t.Errorf("%s: exit = %v, want 1\n%s", c.name, err, out)
		}
		if !strings.Contains(string(out), c.want) || !strings.Contains(string(out), "hint:") {
			t.Errorf("%s: output %q lacks %q and a hint", c.name, out, c.want)
		}
		if strings.Contains(string(out), "pulling") || strings.Contains(string(out), "Trying to pull") {
			t.Errorf("%s: it tried to pull: %s", c.name, out)
		}
	}
}

// A slow start of the container is not silent: before the TUI exists, a line
// says what is being waited for, with the engine command to run by hand; the
// raw-output view then shows that command first. A stand-in "podman" first on
// PATH delays only the "run" by five seconds, like a first start of a new
// image does.
func TestE2E_EE_SlowStartIsReported(t *testing.T) {
	image := requireEE(t)
	real, err := exec.LookPath("podman")
	if err != nil {
		t.Skip("podman not found")
	}
	bin := buildE2EBinary(t)
	work := t.TempDir()
	shim := t.TempDir()
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = run ]; then sleep 5; fi\nexec %s \"$@\"\n", real)
	if err := os.WriteFile(filepath.Join(shim, "podman"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "p.yml"), []byte("- hosts: localhost\n  gather_facts: false\n  connection: local\n  tasks:\n    - ansible.builtin.ping:\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := startTmuxSession(t)
	s.send(fmt.Sprintf("cd %s && env PATH=%s %s %s run p.yml -i localhost, --ee %s",
		shellQuote(work), shellQuote(shim+":"+os.Getenv("PATH")), e2eCallbackEnv(t), shellQuote(bin), shellQuote(image)), "Enter")
	// After two seconds the TUI comes up with a waiting row; the engine's
	// notices go to the o view, nothing is printed to the terminal.
	s.waitFor("Waiting for ansible-playbook to start in the container", 20*time.Second)
	s.send("o")
	got := s.waitFor("first start of a newly pulled or built image can take a while", 10*time.Second)
	if !strings.Contains(got, "waiting for ansible-playbook to start in the container") {
		t.Errorf("the notice should say what it waits for:\n%s", got)
	}
	got = s.waitFor("to try the image on its own", 5*time.Second)
	if !strings.Contains(got, "run --rm --userns=keep-id "+image+" true") {
		t.Errorf("the hint should be a bare command for the image:\n%s", got)
	}
	s.send("Escape")
	s.waitFor("Playbook completed successfully", 60*time.Second) // it did start, after the delay

	s.send("o")
	view := s.waitFor("tangsible: running: podman run --rm -i --name tangsible-", 5*time.Second)
	if !strings.Contains(view, image+" ansible-playbook p.yml") {
		t.Errorf("the raw-output view should open with the command:\n%s", view)
	}
	s.send("Escape")
	s.send("q")
	waitNoContainers(t, 10*time.Second)
}

// A slow start that ends without any event (--list-tasks): the TUI came up
// for the waiting row, then leaves by itself and the plain output is printed.
func TestE2E_EE_SlowListTasksLeavesTheTUIAndPrints(t *testing.T) {
	image := requireEE(t)
	real, err := exec.LookPath("podman")
	if err != nil {
		t.Skip("podman not found")
	}
	bin := buildE2EBinary(t)
	work := t.TempDir()
	shim := t.TempDir()
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = run ]; then sleep 4; fi\nexec %s \"$@\"\n", real)
	if err := os.WriteFile(filepath.Join(shim, "podman"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "p.yml"), []byte("- name: The only play\n  hosts: localhost\n  gather_facts: false\n  tasks:\n    - name: A listed task\n      ansible.builtin.ping:\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := startTmuxSession(t)
	s.send(fmt.Sprintf("cd %s && env PATH=%s %s %s run p.yml -i localhost, --ee %s --list-tasks",
		shellQuote(work), shellQuote(shim+":"+os.Getenv("PATH")), e2eCallbackEnv(t), shellQuote(bin), shellQuote(image)), "Enter")
	s.waitFor("Waiting for ansible-playbook to start in the container", 20*time.Second)
	got := s.waitFor("A listed task", 60*time.Second)
	if strings.Contains(got, "Waiting for ansible-playbook") {
		t.Errorf("the TUI should be gone, leaving only the printed list:\n%s", got)
	}
	waitNoContainers(t, 10*time.Second)
}

// A fact cache and a log file that ansible.cfg puts outside the project are
// reachable in the container at their host paths, so the cache is filled and
// the log written; without it every container run re-gathers facts and loses
// its log.
func TestE2E_EE_FactCacheAndLogOutsideProjectAreShared(t *testing.T) {
	image := requireEE(t)
	bin := buildE2EBinary(t)
	work := t.TempDir()
	state := t.TempDir()
	cacheDir := filepath.Join(state, "facts") // does not exist yet: created like ansible would
	logFile := filepath.Join(state, "ansible.log")
	cfg := fmt.Sprintf("[defaults]\ngathering = smart\nfact_caching = jsonfile\nfact_caching_connection = %s\nlog_path = %s\n", cacheDir, logFile)
	if err := os.WriteFile(filepath.Join(work, "ansible.cfg"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "p.yml"), []byte("- hosts: localhost\n  connection: local\n  tasks:\n    - ansible.builtin.ping:\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := startTmuxSession(t)
	s.send(fmt.Sprintf("cd %s && %s %s run p.yml -i localhost, --ee %s",
		shellQuote(work), e2eCallbackEnv(t), shellQuote(bin), shellQuote(image)), "Enter")
	s.waitFor("Playbook completed successfully", 90*time.Second)
	s.send("q")
	waitNoContainers(t, 10*time.Second)

	if entries, err := os.ReadDir(cacheDir); err != nil || len(entries) == 0 {
		t.Errorf("the fact cache %s should hold localhost's facts (err %v, %d entries)", cacheDir, err, len(entries))
	}
	// (Host-side helper runs write to the log too; only a real run logs the recap.)
	if b, err := os.ReadFile(logFile); err != nil || !strings.Contains(string(b), "PLAY RECAP") {
		t.Errorf("the log %s should hold the run's recap (err %v):\n%s", logFile, err, b)
	}
}

// A host-path variable that is not passed into the container is said so after
// exit, with the way to pass it anyway; it used to vanish silently.
func TestE2E_EE_DroppedHostPathVariableIsReported(t *testing.T) {
	image := requireEE(t)
	bin := buildE2EBinary(t)
	work := t.TempDir()
	if err := os.WriteFile(filepath.Join(work, "p.yml"), []byte("- hosts: localhost\n  gather_facts: false\n  connection: local\n  tasks:\n    - ansible.builtin.ping:\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := startTmuxSession(t)
	s.send(fmt.Sprintf("cd %s && ANSIBLE_ROLES_PATH=/nonexistent/roles %s %s run p.yml -i localhost, --ee %s",
		shellQuote(work), e2eCallbackEnv(t), shellQuote(bin), shellQuote(image)), "Enter")
	s.waitFor("Playbook completed successfully", 90*time.Second)
	s.send("q")
	got := s.waitFor("not passed into the container", 10*time.Second)
	if !strings.Contains(got, "ANSIBLE_ROLES_PATH") || !strings.Contains(got, "extra_mounts") {
		t.Errorf("the message should name the variable and the remedy:\n%s", got)
	}
}

// pass_env and set_env: a listed variable (or prefix) reaches the container, an
// unlisted one does not, and a set value arrives. The playbook reads them with
// the env lookup, which runs in the container.
func TestE2E_EE_PassEnvAndSetEnvReachTheContainer(t *testing.T) {
	image := requireEE(t)
	bin := buildE2EBinary(t)
	work := t.TempDir()
	if err := os.MkdirAll(filepath.Join(work, ".tangsible"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := fmt.Sprintf("[execution_environment]\nimage = %q\npass_env = [\"E2E_PASS_*\", \"E2E_SINGLE\", \"E2E_NOT_SET\"]\n\n[execution_environment.set_env]\nE2E_FIXED = \"fixed-value\"\n", image)
	if err := os.WriteFile(filepath.Join(work, ".tangsible", "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	var tpl []string
	for _, name := range []string{"E2E_PASS_A", "E2E_PASS_B", "E2E_SINGLE", "E2E_FIXED", "E2E_NOT_LISTED", "E2E_NOT_SET"} {
		tpl = append(tpl, fmt.Sprintf("%s={{ lookup('env', '%s') | default('UNSET', true) }}", name, name))
	}
	play := "- hosts: localhost\n  gather_facts: false\n  connection: local\n  tasks:\n" +
		"    - ansible.builtin.copy:\n" +
		"        content: \"" + strings.Join(tpl, " ") + "\\n\"\n" +
		"        dest: \"{{ playbook_dir }}/out.txt\"\n"
	if err := os.WriteFile(filepath.Join(work, "p.yml"), []byte(play), 0o644); err != nil {
		t.Fatal(err)
	}

	s := startTmuxSession(t)
	s.send(fmt.Sprintf("cd %s && E2E_PASS_A=a E2E_PASS_B=b E2E_SINGLE=single E2E_NOT_LISTED=hidden %s %s run p.yml -i localhost,",
		shellQuote(work), e2eCallbackEnv(t), shellQuote(bin)), "Enter")
	s.waitFor("Completed", 60*time.Second)
	got, err := os.ReadFile(filepath.Join(work, "out.txt"))
	if err != nil {
		t.Fatalf("the playbook did not run (no out.txt); pane:\n%s", s.capture())
	}
	want := "E2E_PASS_A=a E2E_PASS_B=b E2E_SINGLE=single E2E_FIXED=fixed-value E2E_NOT_LISTED=UNSET E2E_NOT_SET=UNSET\n"
	if string(got) != want {
		t.Errorf("the container saw\n  %q\nwant\n  %q", got, want)
	}
	s.send("q")
	waitNoContainers(t, 10*time.Second)
}

// container_options reach `podman run`: a --hostname is visible to the playbook.
func TestE2E_EE_ContainerOptionsReachTheEngine(t *testing.T) {
	image := requireEE(t)
	bin := buildE2EBinary(t)
	work := t.TempDir()
	if err := os.MkdirAll(filepath.Join(work, ".tangsible"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := fmt.Sprintf("[execution_environment]\nimage = %q\ncontainer_options = [\"--hostname=e2e-box\"]\n", image)
	if err := os.WriteFile(filepath.Join(work, ".tangsible", "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	play := "- hosts: localhost\n  gather_facts: false\n  connection: local\n  tasks:\n" +
		"    - ansible.builtin.copy:\n" +
		"        content: \"{{ lookup('env', 'HOSTNAME') }}\\n\"\n" +
		"        dest: \"{{ playbook_dir }}/out.txt\"\n"
	if err := os.WriteFile(filepath.Join(work, "p.yml"), []byte(play), 0o644); err != nil {
		t.Fatal(err)
	}
	s := startTmuxSession(t)
	s.send(fmt.Sprintf("cd %s && %s %s run p.yml -i localhost,", shellQuote(work), e2eCallbackEnv(t), shellQuote(bin)), "Enter")
	s.waitFor("Completed", 60*time.Second)
	got, err := os.ReadFile(filepath.Join(work, "out.txt"))
	if err != nil || strings.TrimSpace(string(got)) != "e2e-box" {
		t.Errorf("the container's hostname = %q (err %v), want e2e-box", got, err)
	}
	s.send("q")
	waitNoContainers(t, 10*time.Second)
}

// A container_options entry that fights tangsible's own flags stops it at startup.
func TestE2E_EE_ForbiddenContainerOptionStopsAtStartup(t *testing.T) {
	requireE2ETools(t)
	bin := buildE2EBinary(t)
	work := t.TempDir()
	if err := os.MkdirAll(filepath.Join(work, ".tangsible"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "p.yml"), []byte("- hosts: localhost\n  gather_facts: false\n  tasks: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := "[execution_environment]\nimage = \"example.invalid/none:1\"\ncontainer_options = [\"-v\", \"/a:/b\"]\n"
	if err := os.WriteFile(filepath.Join(work, ".tangsible", "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "run", "p.yml", "-i", "localhost,")
	cmd.Dir = work
	out, err := cmd.CombinedOutput()
	if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 2 {
		t.Errorf("exit = %v, want 2\n%s", err, out)
	}
	if !strings.Contains(string(out), "container_options: -v is not allowed: use extra_mounts") {
		t.Errorf("the refusal should say what to use instead:\n%s", out)
	}
	if strings.Contains(string(out), "pulling") {
		t.Errorf("it got as far as pulling:\n%s", out)
	}
}

// Bad pass_env/set_env entries stop tangsible at startup, naming each one.
func TestE2E_EE_BadEnvSettingsStopAtStartup(t *testing.T) {
	requireE2ETools(t)
	bin := buildE2EBinary(t)
	work := t.TempDir()
	if err := os.MkdirAll(filepath.Join(work, ".tangsible"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "p.yml"), []byte("- hosts: localhost\n  gather_facts: false\n  tasks: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := "[execution_environment]\nimage = \"example.invalid/none:1\"\npass_env = [\"*\", \"HOME\", \"1BAD\"]\n\n[execution_environment.set_env]\nSSH_AUTH_SOCK = \"x\"\n"
	if err := os.WriteFile(filepath.Join(work, ".tangsible", "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "run", "p.yml", "-i", "localhost,")
	cmd.Dir = work
	out, err := cmd.CombinedOutput()
	if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 2 {
		t.Errorf("exit = %v, want 2\n%s", err, out)
	}
	for _, want := range []string{`pass_env entry "*"`, `pass_env entry "HOME"`, `pass_env entry "1BAD"`, `set_env entry "SSH_AUTH_SOCK"`, "hint:"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(string(out), "pulling") {
		t.Errorf("it got as far as pulling:\n%s", out)
	}
}

// A mistake in extra_mounts stops tangsible at startup - before an image is
// pulled (the image here does not exist anywhere) or a TUI is built - with a
// message naming the entry.
func TestE2E_EE_BadExtraMountStopsBeforeAnythingRuns(t *testing.T) {
	requireE2ETools(t)
	bin := buildE2EBinary(t)
	work := t.TempDir()
	if err := os.MkdirAll(filepath.Join(work, ".tangsible"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "p.yml"), []byte("- hosts: localhost\n  gather_facts: false\n  tasks: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct{ entry, want string }{
		{work + "/no-such-dir:/x:ro", "does not exist"},
		{work + ":/x:readonly", `unrecognized option "readonly"`},
		{work + ":relative", "must be an absolute path"},
		{work + ":/a:ro,rw", "contradict"},
	}
	for _, c := range cases {
		cfg := fmt.Sprintf("[execution_environment]\nimage = \"example.invalid/none:1\"\nextra_mounts = [%q]\n", c.entry)
		if err := os.WriteFile(filepath.Join(work, ".tangsible", "config.toml"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(bin, "run", "p.yml", "-i", "localhost,")
		cmd.Dir = work
		out, err := cmd.CombinedOutput()
		if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 2 {
			t.Errorf("%s: exit = %v, want 2\n%s", c.entry, err, out)
		}
		if !strings.Contains(string(out), "extra_mounts entry") || !strings.Contains(string(out), c.want) {
			t.Errorf("%s: output %q lacks the entry and %q", c.entry, out, c.want)
		}
		if strings.Contains(string(out), "pulling") {
			t.Errorf("%s: it got as far as pulling: %s", c.entry, out)
		}
	}
}

// A valid entry - with a variable in the source and an option list - is
// visible in the container, and read-only there.
func TestE2E_EE_ExtraMountIsVisibleAndReadOnly(t *testing.T) {
	image := requireEE(t)
	bin := buildE2EBinary(t)
	work := t.TempDir()
	probe := t.TempDir()
	if err := os.WriteFile(filepath.Join(probe, "hello.txt"), []byte("visible-in-the-container\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(work, ".tangsible"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := fmt.Sprintf("[execution_environment]\nimage = %q\nextra_mounts = [\"$PROBE_DIR:/probe:ro,z\"]\n", image)
	if err := os.WriteFile(filepath.Join(work, ".tangsible", "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	play := "- hosts: localhost\n  gather_facts: false\n  connection: local\n  tasks:\n" +
		"    - ansible.builtin.slurp:\n        src: /probe/hello.txt\n      register: r\n" +
		"    - ansible.builtin.file:\n        path: /probe/new-file\n        state: touch\n      ignore_errors: true\n      register: w\n" +
		"    - ansible.builtin.copy:\n" +
		"        content: \"{{ r.content | b64decode | trim }} write_failed={{ w is failed }}\\n\"\n" +
		"        dest: \"{{ playbook_dir }}/out.txt\"\n"
	if err := os.WriteFile(filepath.Join(work, "p.yml"), []byte(play), 0o644); err != nil {
		t.Fatal(err)
	}

	s := startTmuxSession(t)
	s.send(fmt.Sprintf("cd %s && PROBE_DIR=%s %s %s run p.yml -i localhost,", shellQuote(work), shellQuote(probe), e2eCallbackEnv(t), shellQuote(bin)), "Enter")
	s.waitFor("Completed", 60*time.Second)
	got, err := os.ReadFile(filepath.Join(work, "out.txt"))
	if err != nil {
		t.Fatalf("the playbook did not run (no out.txt); pane:\n%s", s.capture())
	}
	if want := "visible-in-the-container write_failed=True\n"; string(got) != want {
		t.Errorf("the container saw %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(probe, "new-file")); err == nil {
		t.Error("the read-only mount let the container create a file on the host")
	}
	s.send("q")
	waitNoContainers(t, 10*time.Second)
}

// SIGTERM must take the container down with it and leave no scratch
// directory behind (tangsible's TMPDIR is a directory of the test's own).
func TestE2E_EE_SIGTERMRemovesContainerAndScratch(t *testing.T) {
	image := requireEE(t)
	tmp := t.TempDir()
	s, pid := startSlowSession(t, tmp, "--ee", image)
	deadline := time.Now().Add(30 * time.Second)
	for len(tangsibleContainers(t)) == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("no container appeared; pane:\n%s", s.capture())
		}
		time.Sleep(200 * time.Millisecond)
	}
	if entries, _ := os.ReadDir(tmp); len(entries) == 0 {
		t.Fatal("expected the event FIFO's scratch directory while the run is live")
	}

	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	s.waitFor("exit=143", 30*time.Second)
	waitNoContainers(t, 15*time.Second)
	if entries, _ := os.ReadDir(tmp); len(entries) != 0 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("scratch files left behind: %v", names)
	}
}

// The flags' usage errors need no TTY or container: they fail before either.
func TestE2E_EE_FlagUsageErrors(t *testing.T) {
	requireE2ETools(t)
	bin := buildE2EBinary(t)
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"run", "x.yml", "--ee"}, "--ee needs an image"},
		{[]string{"run", "x.yml", "--ee", "img", "--no-ee"}, "cannot be combined"},
		{[]string{"host", "h", "--ee", "img"}, "only supported for run, rerun, role, and view"},
		{[]string{"debug", "x.yml", "--ee", "img"}, "debug does not support execution environments"},
	}
	for _, c := range cases {
		out, err := exec.Command(bin, c.args...).CombinedOutput()
		ee, ok := err.(*exec.ExitError)
		if !ok || ee.ExitCode() != 2 {
			t.Errorf("%v: exit = %v, want 2\n%s", c.args, err, out)
		}
		if !strings.Contains(string(out), c.want) {
			t.Errorf("%v: output %q lacks %q", c.args, out, c.want)
		}
	}
}
