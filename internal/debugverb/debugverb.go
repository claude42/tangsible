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
	"strings"
	"sync"
	"time"

	"code.aw.net/claude/tangsible/internal/config"
	"code.aw.net/claude/tangsible/internal/credfile"
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
	// Rest is the passthrough ansible-playbook args, --step and
	// --step already rejected - debug has no synthetic flags of its own (no --start-at-play, no
	// --dialog, no rerun-only flags; design-docs/DebugMode.md's own
	// "no history recording" decision also means there's no rerun dialog
	// to pre-fill from).
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
	// PlaybookDir/CopyDir are the same two directories BuildShadowCopy was
	// given/returned - kept here (unlike playbookDir/copyDir, the plain
	// locals PrepareSession already computes for its own use below) so
	// internal/session can rewrite the shadow copy's own path back to the
	// user's real one wherever it leaks into a captured task_vars
	// snapshot's own string values - design-docs/DebugMode.md's "shadow
	// copy path leaks into magic variables" follow-up. Every file in the
	// copy sits at the exact same relative path it has in the original
	// tree (BuildShadowCopy's own hardlink walk), so this is always a
	// plain, unconditional prefix swap - no per-variable-name logic, no
	// line-shift math like PathMapping above needs.
	PlaybookDir string
	CopyDir     string
	// ForcedLinear reports that a play's own non-linear strategy was
	// rewritten to linear in the copy (debugcopy.ForceLinearStrategy);
	// the top bar says so.
	ForcedLinear bool
	// Refresher rebuilds the copy for a restart; nil only in tests.
	Refresher *Refresher
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
	playbookArg, rest, explicit := config.SplitPlaybookArgsAmongOptions(args)
	if !explicit {
		playbookArg, _ = config.ResolvePlaybook()
		if playbookArg == "" {
			fmt.Fprintln(os.Stderr, config.UsageLine(config.VerbDebug))
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
	// SplitPlaybookArgsAmongOptions' own missing-playbook case above uses.
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

	locs := source.ListTaskLocations(absPlaybook)
	if len(locs) == 0 {
		fmt.Fprintf(os.Stderr, "tangsible: no tasks found in %s\n", playbookArg)
		os.Exit(1)
	}

	// --ask-*-pass can't be answered once the TUI runs (F5 spawns after it
	// owns the terminal): ask now, hand ansible password files instead.
	rest, credCleanup, err := credfile.Rewrite(rest, credfile.TerminalPrompter)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tangsible: %v\n", err)
		os.Exit(1)
	}

	prep, shadowCleanup, err := buildCopy(absPlaybook, rest, locs)
	if err != nil {
		credCleanup()
		fmt.Fprintf(os.Stderr, "tangsible: %v\n", err)
		os.Exit(1)
	}
	prep.Refresher = &Refresher{playbook: absPlaybook, latest: shadowCleanup}
	return prep, func() { shadowCleanup(); credCleanup() }
}

// buildCopy makes one shadow copy of absPlaybook's directory, splices the
// debugger in, probes the task skeleton and returns it all, with the
// copy's own cleanup. Shared by PrepareSession and Refresher.Reprepare;
// rest already has its password prompts swapped for files. The error text
// is the whole message (no "tangsible:" prefix), possibly several lines.
func buildCopy(absPlaybook string, rest []string, locs []source.TaskLocation) (SessionPrep, func(), error) {
	playbookDir := filepath.Dir(absPlaybook)
	relPlaybook, err := filepath.Rel(playbookDir, absPlaybook)
	if err != nil {
		return SessionPrep{}, nil, err
	}

	// internal/debugcopy (M0): hardlink shadow copy + debugger: always
	// splice, exactly as spiked/built there - never touches the real
	// playbook tree.
	copyDir, copyCleanup, err := debugcopy.BuildShadowCopy(playbookDir, config.NewRunID(time.Now()))
	if err != nil {
		return SessionPrep{}, nil, fmt.Errorf("couldn't prepare a debug copy of %s: %v", playbookDir, err)
	}

	// Before the splice (which only inserts lines; this edits in place):
	// debug's skeleton-by-position tree needs a lockstep strategy.
	forcedLinear := debugcopy.ForceLinearStrategy(playbookDir, copyDir, absPlaybook, locs)

	if err := debugcopy.SpliceDebuggerAlways(playbookDir, copyDir, locs); err != nil {
		copyCleanup()
		return SessionPrep{}, nil, fmt.Errorf("couldn't prepare a debug copy of %s: %v", playbookDir, err)
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
	skeleton, probeStderr := runner.BuildProgressSkeletonVerbose(copyPlaybookPath, rest, true)
	if len(skeleton) == 0 {
		copyCleanup()
		// Lead with the cause, not with tangsible's own bookkeeping: the
		// usual reason is that ansible-playbook itself failed (a missing
		// vault password file, a YAML error, an unknown flag), and its own
		// message is what the user needs to read.
		if probeStderr != "" {
			return SessionPrep{}, nil, fmt.Errorf("ansible-playbook could not list this playbook's tasks, so there is nothing to debug:\n%s", summarizeProbeStderr(probeStderr))
		}
		return SessionPrep{}, nil, fmt.Errorf("ansible-playbook listed no tasks for this playbook - nothing to debug")
	}

	return SessionPrep{
		Playbook:         absPlaybook,
		Rest:             rest,
		CopyPlaybookPath: copyPlaybookPath,
		Skeleton:         skeleton,
		PathMapping:      pathMapping,
		PlaybookDir:      playbookDir,
		CopyDir:          copyDir,
		ForcedLinear:     forcedLinear,
	}, copyCleanup, nil
}

// Refresher rebuilds a session's shadow copy from the playbook as it is on
// disk now (design-docs/DebugMode.md, "Restart"): what lets 'r' pick up
// edits to the playbook or its roles. It keeps the latest copy's cleanup so
// the session's exit removes the copy actually in use.
type Refresher struct {
	playbook string

	mu     sync.Mutex
	latest func()
}

// Reprepare builds a new copy and skeleton - with rest as the
// ansible-playbook arguments (tags, limit, ... may have changed; password
// prompts already swapped for files) - and, once that worked, removes the
// previous copy. On error nothing changes: the previous copy stays. Safe off
// the event loop (it only runs ansible-playbook --list-tasks).
func (r *Refresher) Reprepare(rest []string) (SessionPrep, error) {
	locs := source.ListTaskLocations(r.playbook)
	if len(locs) == 0 {
		return SessionPrep{}, fmt.Errorf("no tasks found in %s", r.playbook)
	}
	prep, cleanup, err := buildCopy(r.playbook, rest, locs)
	if err != nil {
		return SessionPrep{}, err
	}
	r.mu.Lock()
	old := r.latest
	r.latest = cleanup
	r.mu.Unlock()
	if old != nil {
		old()
	}
	prep.Refresher = r
	return prep, nil
}

// Cleanup removes the latest copy; idempotent.
func (r *Refresher) Cleanup() {
	r.mu.Lock()
	c := r.latest
	r.mu.Unlock()
	if c != nil {
		c()
	}
}

// summarizeProbeStderr trims BuildProgressSkeletonVerbose's own raw
// stderr down to what's actually worth showing: an unrecognized or
// malformed flag makes ansible-playbook's own argparse print a short
// usage block and one "ansible-playbook: error: ..." line, immediately
// followed by its own full --help text - confirmed empirically, well
// over a hundred lines describing every option that exists, no matter
// how short the real problem is. That line is cut right after, when
// present; a failure with no such line (e.g. a YAML syntax error) has no
// single reliable landmark to cut at, so falls back to a generous line
// cap instead, rather than risk dumping an equally large, unrelated
// wall of text for some failure shape never actually seen while writing this.
func summarizeProbeStderr(stderr string) string {
	const maxLines = 20
	lines := strings.Split(stderr, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "ansible-playbook: error:") {
			return strings.Join(lines[:i+1], "\n")
		}
	}
	lines = runner.DropWarningsRepeatingErrors(lines)
	stderr = strings.Join(lines, "\n")
	if len(lines) > maxLines {
		return strings.Join(lines[:maxLines], "\n") + "\n... (truncated)"
	}
	return stderr
}
