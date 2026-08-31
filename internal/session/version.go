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
	"runtime"
	"runtime/debug"
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

// RunVersion implements the "version" verb: print the build stamps and
// exit. No TTY, no inventory, no config - it runs before any of that is
// set up, same as "vault".
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
	return 0
}
