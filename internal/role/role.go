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

package role

import (
	"fmt"
	"os"

	"code.aw.net/claude/tangsible/internal/rolepath"
)

// RoleStubFilename returns the dot-prefixed stub playbook filename
// design-docs/Tangsible role.md specifies for role - written into the
// current working directory, never a system temp directory. That matters
// beyond tidiness: source.go's buildTaskSourceIndex finds a task's own
// source for the output drill-down view by walking the *playbook's own*
// directory tree (plus the directories of roles named in its roles: list
// that live elsewhere on the roles path) - a stub written to a system temp
// directory would start that walk from an empty directory and a
// project-local role's Task definition sections would silently go blank. Dot-prefixed so it doesn't clutter
// a directory listing next to the user's own files; should be
// .gitignore'd (not done automatically - same "don't reach beyond the
// project's own files uninvited" restraint writeState already exercises
// for .tangsible/state.toml itself).
func RoleStubFilename(role string) string {
	return ".tangsible-role-" + role + ".yml"
}

// WriteRoleStub generates a minimal playbook referencing role - hosts:
// all, per design-docs/Tangsible role.md (roles are generally
// host-agnostic; the actual target is narrowed the normal way, via -l/-i
// passthrough args) - and writes it to roleStubFilename(role) in the
// current working directory, overwriting any stale leftover from a
// previous crashed run. No locking, no uniqueness beyond the role's own
// name - the same "not expected to be edited concurrently by more than
// one tangsible process, best effort" tradeoff .tangsible's own read/write
// already accepts (see history.go's appendInvocation).
func WriteRoleStub(role string) (path string, err error) {
	path = RoleStubFilename(role)
	content := fmt.Sprintf("- hosts: all\n  roles:\n    - %s\n", role)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// RoleFound is a best-effort check for whether role's own directory is
// discoverable the way ansible would find it: ./roles/<name> under the
// current working directory first, then each roles_path entry
// (rolepath.Dirs: $ANSIBLE_ROLES_PATH, ansible.cfg, or ansible's built-in
// ~/.ansible/roles, /usr/share/ansible/roles, /etc/ansible/roles) -
// purely informational, used to warn the user up front rather than to
// gate anything: per design-docs/Tangsible role.md's own "best-effort,
// degrade gracefully" decision, the stub is generated and handed to
// ansible-playbook regardless of what this reports. A role found outside
// ./roles still gets its Task definition sections, because the source
// index (source.BuildTaskSourceIndex) looks in the same places.
func RoleFound(role string) bool {
	return rolepath.Find(role, ".", rolepath.Dirs()) != ""
}

// StartRoleSession generates a fresh stub playbook for role and returns
// its path plus a cleanup func that removes it - shared by "tangsible
// role" itself and by "tangsible rerun" when it resolves to a role
// (design-docs/Tangsible role.md): a role rerun always starts from a brand
// new stub, never a reused one - the previous session's own stub was
// already deleted when that earlier process exited (see main's cleanup
// wiring). Exits the program directly on a write failure, before any
// cleanup has anything to do yet (nothing was created) - same "not worth a
// recoverable error path" treatment spawnGeneration's own construction-time
// failures already get for "run".
func StartRoleSession(role string) (stubPath string, cleanup func()) {
	path, err := WriteRoleStub(role)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tangsible: couldn't create stub playbook for role %q: %v\n", role, err)
		os.Exit(1)
	}
	if !RoleFound(role) {
		fmt.Fprintf(os.Stderr, "tangsible: couldn't find role %q in ./roles or the roles path - ansible-playbook will still try to resolve it, but the drill-down view's Task definition section won't be available for it\n", role)
	}
	return path, func() { os.Remove(path) }
}
