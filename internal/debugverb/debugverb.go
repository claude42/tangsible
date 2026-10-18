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

// Package debugverb is the "debug" verb's own headless glue
// (design-docs/DebugMode.md's "Architecture decision (2026-10-18)"):
// PrepareSession resolves "tangsible debug"'s own arguments and builds
// the shadow copy + splice + skeleton probe on top of M0-M2's headless
// primitives (internal/debugcopy, internal/debugrun); SeedState/Cursor
// (cursor.go) resolve a live debug session's own jsonl event stream
// against that skeleton without ever going through PlaybookState.Apply
// (see SeedState's own doc comment for why not). internal/session is the
// only caller - it builds a real liveSession around this package's own
// output, the same tree/two-pane/drill-down every other verb gets,
// rather than this package building a second, separate TUI of its own
// the way it used to (M3). Like internal/debugcopy/internal/debugrun,
// this package touches no tview/tcell/uikit at all.
package debugverb

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"code.aw.net/claude/tangsible/internal/config"
	"code.aw.net/claude/tangsible/internal/debugcopy"
	"code.aw.net/claude/tangsible/internal/runner"
	"code.aw.net/claude/tangsible/internal/source"
)

// SessionPrep is PrepareSession's own successful result - everything
// internal/session needs to build a debug liveSession.
type SessionPrep struct {
	// Playbook is the real playbook's own absolute path - what display/
	// sourceIndex/invocation-history code should use, never CopyPlaybookPath.
	Playbook string
	// Rest is the passthrough ansible-playbook args, --step already
	// rejected and stripped of nothing else - debug has no synthetic
	// flags of its own to extract (no --start-at-play, no --dialog, no
	// rerun-only flags; design-docs/DebugMode.md's own "no history
	// recording" decision also means there's no rerun dialog to pre-fill
	// from).
	Rest []string
	// CopyPlaybookPath is the absolute path into the shadow copy - what
	// actually gets spawned (internal/debugrun.Spawn).
	CopyPlaybookPath string
	// Skeleton is the --list-tasks/--list-hosts probe against the
	// already-spliced copy - SeedState's own input.
	Skeleton []runner.ProgressEntry
	// PathMapping translates a live event's own task.path (reported
	// against the shadow copy, post-splice) back to the real tree's own
	// "path:line" - debugcopy.BuildPathMapping's own output, Cursor's
	// input. Needed for the drill-down's Task definition/Resolved tabs,
	// both keyed by TaskNode.Path against sourceIndex, which is built
	// from the real tree and so has no entries for the copy's own,
	// line-shifted paths at all.
	PathMapping map[string]string
}

// PrepareSession resolves "tangsible debug [<playbook.yml>] [ansible-
// playbook args...]" and builds the shadow copy + debugger: always
// splice (internal/debugcopy) + skeleton probe - everything this
// package's own former RunDebugVerb/runDebugTUI did before constructing
// its own TUI (M3), minus the TUI itself. Prints to stderr and exits on
// any fatal error - same convention role.StartRoleSession already uses
// for its own setup failures: there is no TUI yet at this point for an
// error to show up inside instead. cleanup removes the shadow copy; the
// caller is expected to wire it the same way a role stub's own cleanup
// already is (session/main.go's "cleanup, if non-nil" convention).
func PrepareSession(args []string) (prep SessionPrep, cleanup func()) {
	playbookArg, rest, explicit := config.SplitPlaybookArgs(args)
	if !explicit {
		playbookArg, _ = config.ResolvePlaybook()
		if playbookArg == "" {
			fmt.Fprintf(os.Stderr, "usage: %s debug [<playbook.yml>] [ansible-playbook args...]\n", os.Args[0])
			fmt.Fprintln(os.Stderr, "no playbook given, and none could be determined from TANGSIBLE_PLAYBOOK, .tangsible/config.toml, $XDG_CONFIG_HOME/tangsible/config.toml, or ./site.yml")
			os.Exit(2)
		}
	}

	// ansible-playbook's own --step installs a second, structurally
	// different pre-dispatch pause (design-docs/DebugMode.md's own
	// "--step CLI-flag side discussion") whose prompt text readUntilPrompt
	// (internal/debugrun) doesn't recognize at all - confirmed by reading
	// that discussion, not re-spiked here: letting it through would mean
	// Session's own REPL-driving calls block forever on a prompt that
	// never arrives in the shape they expect, hanging rather than erroring
	// cleanly. Rejected up front instead, the same "usage error" shape
	// SplitPlaybookArgs' own missing-playbook case above uses.
	for _, a := range rest {
		if a == "--step" {
			fmt.Fprintln(os.Stderr, "tangsible: debug does not support --step - its own breakpoint mechanism (not ansible-playbook's) is what pauses the run")
			os.Exit(2)
		}
	}

	absPlaybook, err := filepath.Abs(playbookArg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tangsible: %v\n", err)
		os.Exit(1)
	}
	playbookDir := filepath.Dir(absPlaybook)
	relPlaybook, err := filepath.Rel(playbookDir, absPlaybook)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tangsible: %v\n", err)
		os.Exit(1)
	}

	locs := source.ListTaskLocations(absPlaybook)
	if len(locs) == 0 {
		fmt.Fprintf(os.Stderr, "tangsible: no tasks found in %s\n", playbookArg)
		os.Exit(1)
	}

	// internal/debugcopy (M0): hardlink shadow copy + debugger: always
	// splice, exactly as spiked/built there - never touches the real
	// playbook tree.
	copyDir, copyCleanup, err := debugcopy.BuildShadowCopy(playbookDir, config.NewRunID(time.Now()))
	if err != nil {
		fmt.Fprintf(os.Stderr, "tangsible: couldn't prepare a debug copy of %s: %v\n", playbookDir, err)
		os.Exit(1)
	}

	if err := debugcopy.SpliceDebuggerAlways(playbookDir, copyDir, locs); err != nil {
		copyCleanup()
		fmt.Fprintf(os.Stderr, "tangsible: couldn't prepare a debug copy of %s: %v\n", playbookDir, err)
		os.Exit(1)
	}

	// Computed from the exact same locs SpliceDebuggerAlways just used,
	// so the line-shift math always agrees with whatever that call
	// actually spliced - see BuildPathMapping's own doc comment.
	pathMapping := debugcopy.BuildPathMapping(playbookDir, copyDir, locs)

	// Probed against the already-spliced copy, with the exact passthrough
	// args the real debug run is about to use (runner.BuildProgressSkeleton's
	// own doc comment: --tags/--skip-tags/-l all shrink this output the
	// same way they'd shrink the real run's scope) - not the original
	// playbook, on the off chance a future splice ever changed anything
	// --list-tasks itself would see (today it doesn't: debugger: always
	// is a no-op for --list-tasks), and not relying on the process's own
	// cwd, since the copy lives under playbookDir/.tangsible/debug/<runID>,
	// not necessarily cwd.
	copyPlaybookPath := filepath.Join(copyDir, relPlaybook)
	skeleton := runner.BuildProgressSkeleton(copyPlaybookPath, rest)
	if len(skeleton) == 0 {
		copyCleanup()
		fmt.Fprintln(os.Stderr, "tangsible: couldn't predict this playbook's own task list (--list-tasks probe failed) - nothing to debug")
		os.Exit(1)
	}

	return SessionPrep{
		Playbook:         absPlaybook,
		Rest:             rest,
		CopyPlaybookPath: copyPlaybookPath,
		Skeleton:         skeleton,
		PathMapping:      pathMapping,
	}, copyCleanup
}
