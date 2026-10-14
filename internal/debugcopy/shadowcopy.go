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

// Package debugcopy builds and mutates the disposable "shadow copy" debug
// mode runs against, so a breakpointed run never touches the user's own
// tracked playbook/roles - design-docs/DebugMode.md's "full directory-tree
// hardlink shadow copy" (spiked live 2026-09-27) and its uniform
// debugger:-always splicing (spiked live 2026-10-14). This is the M0 step
// from that same doc's build roadmap: a headless primitive, no TUI/verb
// wiring yet.
package debugcopy

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// excludedDirNames are never walked into when building a shadow copy -
// the app's own state directory (which would otherwise try to copy
// itself, since the shadow copy lives inside it - see BuildShadowCopy) and
// VCS metadata, neither of which ansible-playbook has any use for.
var excludedDirNames = map[string]bool{
	".tangsible": true,
	".git":       true,
}

// BuildShadowCopy builds a complete, disposable copy of playbookDir's own
// directory tree under "<playbookDir>/.tangsible/debug/<runID>/",
// hardlinking every file instead of copying bytes so the cost is
// independent of repo size - confirmed live: a byte-for-byte copy would
// pay for a repo's own vendored collections/galaxy roles on every debug
// session. Hardlinks require the same filesystem, which is exactly why the
// copy lives nested inside playbookDir rather than a system temp
// directory (confirmed live: linking across a tmpfs/zfs boundary fails
// outright with EXDEV).
//
// The returned cleanup func removes the whole copy tree - safe to call
// unconditionally, even after a partial failure, since it only ever
// touches directory entries under its own runID-scoped directory, never
// the hardlinked originals' data (removing a hardlink just decrements the
// shared inode's link count, confirmed live).
//
// Every file in the result is a hardlink to the original except whatever
// SpliceDebuggerAlways later rewrites - callers must never open a path
// under the returned copyDir for in-place writing themselves; see that
// function's own doc comment for why.
func BuildShadowCopy(playbookDir, runID string) (copyDir string, cleanup func(), err error) {
	copyDir = filepath.Join(playbookDir, ".tangsible", "debug", runID)
	cleanup = func() { os.RemoveAll(copyDir) }

	if err := os.MkdirAll(copyDir, 0o755); err != nil {
		return "", func() {}, fmt.Errorf("debugcopy: creating shadow copy dir: %w", err)
	}

	walkErr := filepath.WalkDir(playbookDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if excludedDirNames[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(playbookDir, path)
		if err != nil {
			return err
		}
		dst := filepath.Join(copyDir, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.Link(path, dst)
	})
	if walkErr != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("debugcopy: building shadow copy: %w", walkErr)
	}

	return copyDir, cleanup, nil
}
