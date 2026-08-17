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

package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// roleStubFilename returns the dot-prefixed stub playbook filename
// design-docs/Tangsible role.md specifies for role - written into the
// current working directory, never a system temp directory. That matters
// beyond tidiness: source.go's buildTaskSourceIndex finds a task's own
// source for the output drill-down view by walking the *playbook's own*
// directory tree, never by tracing roles:/include_role references - a
// stub written to a system temp directory would start that walk from an
// empty directory and the role's own Task/Play definition sections would
// silently go blank for every role run. Dot-prefixed so it doesn't clutter
// a directory listing next to the user's own files; should be
// .gitignore'd (not done automatically - same "don't reach beyond the
// project's own files uninvited" restraint writeState already exercises
// for .tangsible/state.toml itself).
func roleStubFilename(role string) string {
	return ".tangsible-role-" + role + ".yml"
}

// writeRoleStub generates a minimal playbook referencing role - hosts:
// all, per design-docs/Tangsible role.md (roles are generally
// host-agnostic; the actual target is narrowed the normal way, via -l/-i
// passthrough args) - and writes it to roleStubFilename(role) in the
// current working directory, overwriting any stale leftover from a
// previous crashed run. No locking, no uniqueness beyond the role's own
// name - the same "not expected to be edited concurrently by more than
// one tangsible process, best effort" tradeoff .tangsible's own read/write
// already accepts (see history.go's appendInvocation).
func writeRoleStub(role string) (path string, err error) {
	path = roleStubFilename(role)
	content := fmt.Sprintf("- hosts: all\n  roles:\n    - %s\n", role)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// roleFoundNearby is a best-effort check for whether role's own directory
// is actually discoverable under the current working directory (the
// standard ./roles/<name> layout) - purely informational, used to warn the
// user up front rather than to gate anything: per design-docs/Tangsible
// role.md's own "best-effort, degrade gracefully" decision, the stub is
// generated and handed to ansible-playbook regardless of what this
// reports. Deliberately doesn't also search $ANSIBLE_ROLES_PATH or any
// other of ansible's own role-search locations - since the stub always
// lives in the current working directory (see roleStubFilename), only a
// role actually reachable from *that* directory can ever have its source
// found by buildTaskSourceIndex's own walk, so checking places the walk
// could never reach anyway would just be dead code, not a more thorough
// check.
func roleFoundNearby(role string) bool {
	info, err := os.Stat(filepath.Join("roles", role))
	return err == nil && info.IsDir()
}

// startRoleSession generates a fresh stub playbook for role and returns
// its path plus a cleanup func that removes it - shared by "tangsible
// role" itself and by "tangsible rerun" when it resolves to a role
// (design-docs/Tangsible role.md): a role rerun always starts from a brand
// new stub, never a reused one - the previous session's own stub was
// already deleted when that earlier process exited (see main's cleanup
// wiring). Exits the program directly on a write failure, before any
// cleanup has anything to do yet (nothing was created) - same "not worth a
// recoverable error path" treatment spawnGeneration's own construction-time
// failures already get for "run".
func startRoleSession(role string) (stubPath string, cleanup func()) {
	path, err := writeRoleStub(role)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tangsible: couldn't create stub playbook for role %q: %v\n", role, err)
		os.Exit(1)
	}
	if !roleFoundNearby(role) {
		fmt.Fprintf(os.Stderr, "tangsible: couldn't find ./roles/%s - ansible-playbook will still try to resolve it, but the drill-down view's Task/Play definition sections won't be available for it\n", role)
	}
	return path, func() { os.Remove(path) }
}
