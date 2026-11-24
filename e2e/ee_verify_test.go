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

// End-to-end tests for `tangsible ee verify` and `tangsible ee build`
// (design-docs/EEInit.md, "Verifying a definition" and "Building a
// definition"). It really builds an image, so it needs podman,
// ansible-builder, ansible-doc on the host, network access for the build and
// a minute or two; it is opt in on top of the usual e2e tag:
//
//	TANGSIBLE_EE_VERIFY=1 TANGSIBLE_EE_IMAGE=ghcr.io/ansible-community/community-ee-base:latest \
//	    go test -tags e2e -run "EEVerify|EEBuild" ./e2e
//
// The image named by TANGSIBLE_EE_IMAGE (already pulled) is the base of the
// definition `ee init` drafts for it.
package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func requireEEVerify(t *testing.T) (image string) {
	t.Helper()
	if os.Getenv("TANGSIBLE_EE_VERIFY") == "" {
		t.Skip("TANGSIBLE_EE_VERIFY not set - skipping the (slow) ee verify e2e test")
	}
	image = os.Getenv("TANGSIBLE_EE_IMAGE")
	if image == "" {
		t.Skip("TANGSIBLE_EE_IMAGE not set - skipping ee verify e2e test")
	}
	for _, tool := range []string{"podman", "ansible-builder", "ansible-doc"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found on PATH - skipping ee verify e2e test", tool)
		}
	}
	if err := exec.Command("podman", "image", "exists", image).Run(); err != nil {
		t.Skipf("image %s is not pulled locally - skipping", image)
	}
	return image
}

// verifyImages lists the throw-away images ee verify may have left behind.
func verifyImages(t *testing.T) []string {
	t.Helper()
	out, err := exec.Command("podman", "images", "--format", "{{.Repository}}:{{.Tag}}").Output()
	if err != nil {
		t.Fatalf("podman images: %v", err)
	}
	var left []string
	for _, l := range strings.Fields(string(out)) {
		if strings.HasPrefix(l, "localhost/tangsible-ee-verify:") {
			left = append(left, l)
		}
	}
	return left
}

func TestEEVerify(t *testing.T) {
	image := requireEEVerify(t)
	bin := buildE2EBinary(t)

	run := func(dir string, args ...string) (string, int) {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		code := 0
		if err != nil {
			ee, ok := err.(*exec.ExitError)
			if !ok {
				t.Fatalf("%v", err)
			}
			code = ee.ExitCode()
		}
		return string(out), code
	}
	project := func(playbook string) string {
		t.Helper()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "site.yml"), []byte(playbook), 0o644); err != nil {
			t.Fatal(err)
		}
		// the build (and the checks) may need DNS from the container
		if err := os.MkdirAll(filepath.Join(dir, ".tangsible"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ".tangsible", "config.toml"), []byte("[execution_environment]\nnetwork = \"host\"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	before := len(verifyImages(t))

	// everything the playbook uses is in the image: exit 0
	dir := project("- hosts: localhost\n  tasks:\n    - ansible.builtin.debug: {msg: \"{{ lookup('env', 'HOME') }}\"}\n    - command: ls /tmp\n      delegate_to: localhost\n")
	if out, code := run(dir, "ee", "init", "site.yml", "--base", image); code != 0 {
		t.Fatalf("ee init: exit %d\n%s", code, out)
	}
	out, code := run(dir, "ee", "verify", "site.yml", "-i", "localhost,")
	if code != 0 {
		t.Fatalf("ee verify: exit %d, want 0\n%s", code, out)
	}
	for _, want := range []string{"Build:           ok", "Syntax check:    ok", "Modules:         2 of 2 found", "Lookups:         1 of 1 found", "Binaries:        1 of 1 found"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".tangsible", "ee-verify.log")); err != nil {
		t.Errorf("no build log: %v", err)
	}

	// a module the image lacks: exit 1, listed; nothing is left behind
	dir = project("- hosts: localhost\n  tasks:\n    - community.docker.docker_container: {name: x, image: y}\n")
	if out, code := run(dir, "ee", "init", "site.yml", "--base", image); code != 0 {
		t.Fatalf("ee init: exit %d\n%s", code, out)
	}
	// drop the collection the draft added, so the built image lacks it
	def, err := os.ReadFile(filepath.Join(dir, "execution-environment.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var kept []string
	skip := 0
	for _, l := range strings.Split(string(def), "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "- name: community.docker") {
			skip = 2 // the entry's name and version lines
		}
		if skip > 0 {
			skip--
			continue
		}
		kept = append(kept, l)
	}
	rewritten := strings.Replace(strings.Join(kept, "\n"), "    collections:\n", "    collections: []\n", 1)
	if err := os.WriteFile(filepath.Join(dir, "execution-environment.yml"), []byte(rewritten), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code = run(dir, "ee", "verify", "site.yml", "-i", "localhost,")
	if code != 1 {
		t.Fatalf("ee verify: exit %d, want 1\n%s", code, out)
	}
	for _, want := range []string{"Syntax check:    FAILED", "not found: community.docker.docker_container (site.yml:3)"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if left := verifyImages(t); len(left) != before {
		t.Errorf("throw-away images left behind: %v", left)
	}
}

func TestEEBuild(t *testing.T) {
	image := requireEEVerify(t)
	bin := buildE2EBinary(t)

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".tangsible"), 0o755); err != nil {
		t.Fatal(err)
	}
	// the build may need DNS from the container
	if err := os.WriteFile(filepath.Join(dir, ".tangsible", "config.toml"), []byte("[execution_environment]\nnetwork = \"host\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	def := "---\nversion: 3\nimages:\n  base_image:\n    name: " + image + "\n"
	if err := os.WriteFile(filepath.Join(dir, "execution-environment.yml"), []byte(def), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (string, int) {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		code := 0
		if err != nil {
			ee, ok := err.(*exec.ExitError)
			if !ok {
				t.Fatalf("%v", err)
			}
			code = ee.ExitCode()
		}
		return string(out), code
	}

	// No name anywhere: refused before any build.
	if out, code := run("ee", "build"); code != 1 || !strings.Contains(out, "names no image") {
		t.Fatalf("no tag: exit %d\n%s", code, out)
	}

	tag := "localhost/tangsible-ee-build-test:e2e"
	t.Cleanup(func() { _ = exec.Command("podman", "rmi", "-f", tag).Run() })
	out, code := run("ee", "build", "-t", tag)
	if code != 0 {
		t.Fatalf("ee build: exit %d\n%s", code, out)
	}
	if !strings.Contains(out, tag) || !strings.Contains(out, "image = ") {
		t.Errorf("report lacks the tag or the config line:\n%s", out)
	}
	if err := exec.Command("podman", "image", "exists", tag).Run(); err != nil {
		t.Errorf("image %s was not kept", tag)
	}
	if _, err := os.Stat(filepath.Join(dir, "context")); err == nil {
		t.Errorf("a context/ directory was left in the project")
	}
	if _, err := os.Stat(filepath.Join(dir, ".tangsible", "ee-build.log")); err != nil {
		t.Errorf("no build log: %v", err)
	}
}
