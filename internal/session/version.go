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
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"runtime/debug"
	"strings"

	"code.aw.net/claude/tangsible/internal/runner"
)

// BuildInfo carries the version stamps the root main package injects at
// release time via -ldflags -X (see .goreleaser.yaml). Every field is the
// zero value for a plain `go build`; RunVersion then fills the gaps from
// the VCS metadata the Go toolchain embeds into the binary automatically.
type BuildInfo struct {
	Version string // semver tag, e.g. "0.1.0"; "dev" for an un-stamped build
	Commit  string // full git SHA
	Date    string // commit timestamp, RFC3339
}

// RunVersion implements the "version" verb: print the build stamps plus
// the versions of the ansible components tangsible shells out to, then
// exit. No TTY, no inventory, no config - it runs before any of that is
// set up, same as "vault". Every external lookup is best-effort: a missing
// binary or collection prints a note, never an error, and never changes
// the exit code - the point is a copy-pasteable environment summary for a
// bug report.
func RunVersion(b BuildInfo) int {
	version := b.Version
	if version == "" {
		version = "dev"
	}
	commit, date := b.Commit, b.Date
	dirty := false

	// Fill anything the ldflags didn't set from the toolchain's own VCS
	// stamps - present for any `go build` inside a git work tree, absent
	// for a `go build` of an extracted tarball (nothing to fall back to
	// then, which is fine).
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				if commit == "" {
					commit = s.Value
				}
			case "vcs.time":
				if date == "" {
					date = s.Value
				}
			case "vcs.modified":
				dirty = s.Value == "true"
			}
		}
	}

	fmt.Printf("tangsible %s\n", version)
	if commit != "" {
		if dirty {
			commit += " (dirty)"
		}
		fmt.Printf("commit:   %s\n", commit)
	}
	if date != "" {
		fmt.Printf("built:    %s\n", date)
	}
	fmt.Printf("go:       %s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	if osd := osDescription(); osd != "" {
		fmt.Printf("os:       %s\n", osd)
	}

	fmt.Println()
	printAnsibleEnv()
	return 0
}

// printAnsibleEnv reports the ansible-playbook version tangsible would
// actually use, plus whether its own bundled callback plugin
// (design-docs/OwnCallbackPlugin.md) actually resolves - since decision 3
// dropped the ansible.posix dependency entirely, there's no longer a
// separate collection to version-check; ResolveCallbackPluginDir's own
// search (dev override, sibling-to-binary, XDG data dir) is now the one
// thing that can actually go wrong, so this reuses its exact error message
// (which already names every location tried) rather than inventing a
// second diagnostic message that could drift from it.
func printAnsibleEnv() {
	if path, err := exec.LookPath("ansible-playbook"); err != nil {
		fmt.Println("ansible-playbook: not found on PATH")
	} else if out, err := exec.Command(path, "--version").Output(); err != nil {
		fmt.Printf("ansible-playbook (%s): could not run --version: %v\n", path, err)
	} else {
		fmt.Printf("ansible-playbook (%s):\n", path)
		for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
			fmt.Printf("  %s\n", line)
		}
	}

	fmt.Println()
	if dir, err := runner.ResolveCallbackPluginDir(); err == nil {
		fmt.Printf("tangsible callback plugin: found in %s\n", dir)
	} else {
		fmt.Printf("tangsible callback plugin: %v\n", err)
	}
}

// osDescription is a one-line human OS label: distro PRETTY_NAME on Linux,
// product version on macOS, plus the kernel string. "" if nothing useful
// turns up (the go: line already carries GOOS/GOARCH).
func osDescription() string {
	var label string
	if b, err := os.ReadFile("/etc/os-release"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if after, found := strings.CutPrefix(line, "PRETTY_NAME="); found {
				label = strings.Trim(after, `"`)
				break
			}
		}
	}
	if label == "" && runtime.GOOS == "darwin" {
		if out, err := exec.Command("sw_vers", "-productVersion").Output(); err == nil {
			label = "macOS " + strings.TrimSpace(string(out))
		}
	}
	if out, err := exec.Command("uname", "-r").Output(); err == nil {
		if k := strings.TrimSpace(string(out)); k != "" {
			if label != "" {
				return label + ", kernel " + k
			}
			return "kernel " + k
		}
	}
	return label
}
