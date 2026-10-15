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

// Package debugverb implements the "debug" verb (design-docs/DebugMode.md's
// M3): an entry point and its own standalone TUI on top of the headless
// primitives built in M0-M2 (internal/debugcopy, internal/debugrun) -
// following the same "one package per standalone verb" pattern internal/
// role, internal/template, internal/host and internal/vault already use,
// not internal/session's own liveSession/NewLiveTUI (a deliberate choice,
// confirmed with the user before writing any of this: that machinery's 30+
// constructor parameters and cross-cutting dialogs - rerun, revisit,
// two-pane drill-down, filter/search, recap - don't apply to a debug
// session, and grafting onto it risked destabilizing the riskiest,
// highest-invariant file in the repo for a feature that needs none of it).
//
// What IS reused, deliberately, per the user's own standing instruction
// ("certainly use the same mechanisms as with the live run, otherwise
// implementations will drift apart"): the same jsonl event stream/decoding
// (runner.ScanEvents, playbook.RawEvent/DecodeHostResult), the same
// outcome/color vocabulary (playbook.Outcome, uikit.ColorTag), and the same
// --list-tasks --list-hosts skeleton-building/forward-matching machinery
// the live run's own progress indicator already uses (runner.
// BuildProgressSkeleton/ProgressTracker) - repurposed here not to predict a
// percentage, but to build the pre-run tree itself and to resolve which row
// a live event or debugger pause belongs to, despite task names repeating.
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

// RunDebugVerb implements "tangsible debug [<playbook.yml>] [ansible-
// playbook args...]". Deliberately simpler than "run"'s own arg handling
// (resolve.go's VerbRun case in session/main.go): no --start-at-play, no
// re-run dialog, no history recording - a debug session isn't an
// "invocation" a later bare "tangsible rerun" should ever replay, and there
// is no re-run dialog here to pre-fill from one. Known, accepted gap for
// this first slice (noted in design-docs/DebugMode.md): the exit code is
// always 0 once the TUI itself exits normally, regardless of whether any
// task actually failed - the tree's own per-host coloring already shows
// that, and this verb has made no other promise about its exit code yet.
func RunDebugVerb(args []string) int {
	playbookArg, rest, explicit := config.SplitPlaybookArgs(args)
	if !explicit {
		playbookArg, _ = config.ResolvePlaybook()
		if playbookArg == "" {
			fmt.Fprintf(os.Stderr, "usage: %s debug [<playbook.yml>] [ansible-playbook args...]\n", os.Args[0])
			fmt.Fprintln(os.Stderr, "no playbook given, and none could be determined from TANGSIBLE_PLAYBOOK, .tangsible/config.toml, $XDG_CONFIG_HOME/tangsible/config.toml, or ./site.yml")
			return 2
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
			return 2
		}
	}

	absPlaybook, err := filepath.Abs(playbookArg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tangsible: %v\n", err)
		return 1
	}
	playbookDir := filepath.Dir(absPlaybook)
	relPlaybook, err := filepath.Rel(playbookDir, absPlaybook)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tangsible: %v\n", err)
		return 1
	}

	locs := source.ListTaskLocations(absPlaybook)
	if len(locs) == 0 {
		fmt.Fprintf(os.Stderr, "tangsible: no tasks found in %s\n", playbookArg)
		return 1
	}

	// internal/debugcopy (M0): hardlink shadow copy + debugger: always
	// splice, exactly as spiked/built there - never touches the real
	// playbook tree.
	copyDir, cleanup, err := debugcopy.BuildShadowCopy(playbookDir, config.NewRunID(time.Now()))
	if err != nil {
		fmt.Fprintf(os.Stderr, "tangsible: couldn't prepare a debug copy of %s: %v\n", playbookDir, err)
		return 1
	}
	defer cleanup()

	if err := debugcopy.SpliceDebuggerAlways(playbookDir, copyDir, locs); err != nil {
		fmt.Fprintf(os.Stderr, "tangsible: couldn't prepare a debug copy of %s: %v\n", playbookDir, err)
		return 1
	}

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
		fmt.Fprintln(os.Stderr, "tangsible: couldn't predict this playbook's own task list (--list-tasks probe failed) - nothing to debug")
		return 1
	}

	runDebugTUI(copyPlaybookPath, relPlaybook, rest, skeleton)
	return 0
}
