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

// End-to-end test for `tangsible ee push` (design-docs/EEInit.md, "Pushing an
// image"). It pushes to a throw-away registry:2 container on a free local port,
// never to a real registry; it needs podman, an already pulled registry image
// and an already pulled image to push (retagged, nothing is built):
//
//	TANGSIBLE_EE_PUSH=1 TANGSIBLE_EE_IMAGE=ghcr.io/ansible-community/community-ee-base:latest \
//	    TANGSIBLE_EE_REGISTRY_IMAGE=docker.io/library/registry:2 \
//	    go test -tags e2e -run EEPush ./e2e
package e2e

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestEEPush(t *testing.T) {
	image, regImage := os.Getenv("TANGSIBLE_EE_IMAGE"), os.Getenv("TANGSIBLE_EE_REGISTRY_IMAGE")
	if os.Getenv("TANGSIBLE_EE_PUSH") == "" || image == "" || regImage == "" {
		t.Skip("set TANGSIBLE_EE_PUSH=1, TANGSIBLE_EE_IMAGE and TANGSIBLE_EE_REGISTRY_IMAGE (pulled images) to run")
	}
	bin := buildE2EBinary(t)

	const container = "tangsible-ee-push-test-registry"
	_ = exec.Command("podman", "rm", "-f", container).Run()
	if out, err := exec.Command("podman", "run", "-d", "--name", container, "-p", "127.0.0.1::5000", regImage).CombinedOutput(); err != nil {
		t.Fatalf("starting the registry: %v\n%s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("podman", "rm", "-f", container).Run() })
	port, err := exec.Command("podman", "port", container, "5000/tcp").Output()
	if err != nil {
		t.Fatal(err)
	}
	addr := "127.0.0.1:" + strings.TrimSpace(string(port))[strings.LastIndex(strings.TrimSpace(string(port)), ":")+1:]

	name := addr + "/tangsible/push-test"
	for _, tag := range []string{name + ":1", name + ":latest"} {
		if out, err := exec.Command("podman", "tag", image, tag).CombinedOutput(); err != nil {
			t.Fatalf("tagging: %v\n%s", err, out)
		}
		tag := tag
		t.Cleanup(func() { _ = exec.Command("podman", "rmi", tag).Run() })
	}
	if out, err := exec.Command("podman", "tag", image, "push-test-local:1").CombinedOutput(); err != nil {
		t.Fatalf("tagging: %v\n%s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("podman", "rmi", "localhost/push-test-local:1").Run() })

	dir := t.TempDir()
	// a local-only name next to the registry's: left out, not an error
	def := "---\nversion: 3\noptions:\n  tags:\n    - push-test-local:1\n    - " + name + ":1\n    - " + name + ":latest\n"
	if err := os.WriteFile(filepath.Join(dir, "execution-environment.yml"), []byte(def), 0o644); err != nil {
		t.Fatal(err)
	}
	conf := filepath.Join(dir, "registries.conf")
	if err := os.WriteFile(conf, []byte("[[registry]]\nlocation = \""+addr+"\"\ninsecure = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (string, int) {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "CONTAINERS_REGISTRIES_CONF="+conf, "REGISTRY_AUTH_FILE="+filepath.Join(dir, "auth.json"))
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

	// No terminal, no --yes: refused before any push.
	if out, code := run("ee", "push"); code != 1 || !strings.Contains(out, "--yes") {
		t.Fatalf("no --yes: exit %d\n%s", code, out)
	}
	out, code := run("ee", "push", "--yes")
	if code != 0 {
		t.Fatalf("ee push: exit %d\n%s", code, out)
	}
	for _, want := range []string{"Not pushed (no registry, local only): push-test-local:1", "Pushed " + name + ":1 (sha256:", "Pushed " + name + ":latest (sha256:"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	resp, err := http.Get("http://" + addr + "/v2/tangsible/push-test/tags/list")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var list struct{ Tags []string }
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if len(list.Tags) != 2 {
		t.Errorf("registry holds tags %v, want 1 and latest", list.Tags)
	}
}
