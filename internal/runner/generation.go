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

// The shared "how does a live generation actually run" mechanism -
// draining its stdout, waiting for it, recording its outcome, and
// starting a fresh one on request (Rerun.md). Originally lived entirely
// inline in main()'s own body; pulled out here once design-docs/
// Revisit.md's Phase 3 needed the identical mechanism for rerun-from-
// within-"revisit" (revisit.go) - matching the same "one shared funnel"
// philosophy SpawnGeneration/AppendInvocation/FinalizeInvocation already
// established for the start/record side of a generation's life, extended
// here to the run/rerun side. main.go's own run/rerun/role session is
// still the only caller with a whole-process exit status to decide (see
// its own tail, after app.Run() returns) - that part is deliberately not
// pulled in here, since revisit's rerun has no equivalent: a failed rerun
// while browsing history shouldn't take the whole "revisit" session down,
// only main.go's genuinely top-level session has that authority.
package runner

import (
	"os/exec"
	"sync/atomic"

	"code.aw.net/claude/tangsible/internal/config"
	pb "code.aw.net/claude/tangsible/internal/playbook"
)

// RunOneGeneration drains one generation's stdout to completion - from
// whatever's already been peeked off it (peeked, "run"'s own pre-flight
// gate only), through channel close - waits for its process, and records
// its outcome: exitCode/processDone (both read live by tui.go's
// rebuild()), recordOutcome (the caller's own accumulator, for whatever it
// does with a finished generation's stderr - main.go prints it once
// app.Run() finally returns; revisit.go's openRevisitEntry does the same,
// just scoped to one entry-viewing session), the saved run-log's stderr
// file (WriteRunStderr), and this generation's own invocation-history
// entry (FinalizeInvocation). playbook/roleDisplayName decide which of
// those an entry belongs under - a session's role-ness/playbook never
// changes mid-session (a rerun reuses the same stub/playbook throughout -
// see StartRoleSession), so whichever was true for this generation's own
// AppendInvocation call (in NewRequestRerun, or the caller's own first-
// generation recording) is still true now.
func RunOneGeneration(cmd *exec.Cmd, stdoutCh <-chan StreamItem, stderrLines <-chan []string, runID string, playbook, roleDisplayName string, apply func(StreamItem), exitCode *atomic.Int32, processDone *atomic.Bool, recordOutcome func(GenerationOutcome), peeked ...StreamItem) {
	for _, item := range peeked {
		apply(item)
	}
	for item := range stdoutCh {
		apply(item)
	}
	childStderr := <-stderrLines // wait for stderr to fully drain before Wait()
	waitErr := cmd.Wait()
	code := ExitCodeOf(waitErr)
	exitCode.Store(int32(code)) // before processDone below - tui.go's
	// rebuild() only ever reads exitCode once it observes processDone
	// true, and Go's atomics are sequentially consistent as a whole
	// program (not just per-variable), so this ordering is what makes
	// that store visible there.
	recordOutcome(GenerationOutcome{ExitCode: code, WaitErr: waitErr, ChildStderr: childStderr})
	config.WriteRunStderr(config.TangsibleStatePath, runID, childStderr)
	if roleDisplayName != "" {
		_ = config.FinalizeInvocation(config.TangsibleStatePath, "", roleDisplayName, code, runID)
	} else {
		_ = config.FinalizeInvocation(config.TangsibleStatePath, playbook, "", code, runID)
	}
	processDone.Store(true)
}

// NewRequestRerun builds tui.go's requestRerun hook (Rerun.md) - starting a
// new generation mid-session, called once the re-run dialog is confirmed.
// Every parameter is exactly what this one mechanism needs from its own
// enclosing session; nothing else is assumed about who's calling it, which
// is what makes it reusable identically by main.go's own run/rerun/role
// session and revisit.go's rerun-from-within-"revisit" (Phase 3).
//
// startAtTask, if non-empty, is prepended as --start-at-task; tags/hosts
// replace the original invocation's own (originalRest is always carried
// forward unedited alongside them - see ParsedPassthroughArgs.Reassemble).
func NewRequestRerun(playbook, roleDisplayName string, originalRest []string, state *pb.PlaybookState, procH *ProcHandle, processDone *atomic.Bool, exitCode *atomic.Int32, progH *atomic.Pointer[ProgressTracker], apply func(StreamItem), recordOutcome func(GenerationOutcome)) func(startAtTask, tags, skipTags, hosts string) {
	return func(startAtTask, tags, skipTags, hosts string) {
		// Reset synchronously, on whatever goroutine calls this (tview's
		// event-loop goroutine, from the re-run dialog's Enter handler) -
		// by the time this returns, a QueueUpdateDraw-driven rebuild()
		// already sees a running, empty generation, matching the
		// view-state reset tui.go does right alongside calling this.
		state.Reset()
		exitCode.Store(0)
		processDone.Store(false)

		newArgs := config.ParsedPassthroughArgs{Tags: tags, SkipTags: skipTags, Hosts: hosts, Rest: originalRest}.Reassemble()
		if startAtTask != "" {
			newArgs = append([]string{"--start-at-task", startAtTask}, newArgs...)
		}

		// Rebuilt synchronously, same place/reasoning as state.Reset()
		// just above - tags/skip-tags/hosts (and --start-at-task) can all
		// change on a rerun, so the previous generation's own skeleton
		// (if any) is stale the instant any of them do. --list-tasks
		// itself ignores --start-at-task entirely (confirmed empirically -
		// it always lists the playbook's full task set regardless), so
		// the resulting skeleton's front few entries simply won't ever be
		// matched - harmless, ProgressTracker's own bounded lookahead
		// already treats "not found (yet)" as a no-op rather than an
		// error, and the real run's own first task-start event is still
		// found well within that window for any reasonably-early
		// --start-at-task point.
		progH.Store(NewProgressTracker(BuildProgressSkeleton(playbook, newArgs)))

		// Recorded the same way the original invocation was, at the top
		// of whichever session this is - but its own error, unlike that
		// one, can't be printed here: the TUI's alternate screen is
		// already active by now (unlike a top-level call, which always
		// runs before any TUI exists), and printing directly to the
		// terminal while it's up would corrupt the display. Silently
		// dropped instead - non-fatal, same reasoning as a top-level
		// call: losing the ability to pre-fill a *future* rerun is never
		// worth disrupting the one the user just asked for.
		if roleDisplayName != "" {
			_ = config.AppendInvocation(config.TangsibleStatePath, "", roleDisplayName, config.ArgsToHistoryString(newArgs))
		} else {
			_ = config.AppendInvocation(config.TangsibleStatePath, playbook, "", config.ArgsToHistoryString(newArgs))
		}

		go func() {
			cmd, stdoutCh, stderrLines, runID, err := SpawnGeneration(playbook, newArgs, procH)
			if err != nil {
				// Rare (ansible-playbook vanished, pipes failed, ...) and,
				// unlike the same failure on a session's very first
				// invocation, not fatal to the whole session - the TUI
				// already exists and the user is mid-session. Recorded as
				// this one generation's own failed outcome instead;
				// GenuineFailure renders it the same as any other failed
				// run.
				exitCode.Store(-1)
				recordOutcome(GenerationOutcome{ExitCode: -1, WaitErr: err})
				if roleDisplayName != "" {
					_ = config.FinalizeInvocation(config.TangsibleStatePath, "", roleDisplayName, -1, "")
				} else {
					_ = config.FinalizeInvocation(config.TangsibleStatePath, playbook, "", -1, "")
				}
				processDone.Store(true)
				return
			}
			RunOneGeneration(cmd, stdoutCh, stderrLines, runID, playbook, roleDisplayName, apply, exitCode, processDone, recordOutcome)
		}()
	}
}
