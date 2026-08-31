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

// Shells out to ansible-playbook using the ansible.posix.jsonl stdout
// callback and streams events live into an interactive TUI as they arrive.
package session

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"

	"code.aw.net/claude/tangsible/internal/config"
	"code.aw.net/claude/tangsible/internal/host"
	pb "code.aw.net/claude/tangsible/internal/playbook"
	"code.aw.net/claude/tangsible/internal/revisit"
	"code.aw.net/claude/tangsible/internal/role"
	"code.aw.net/claude/tangsible/internal/runner"
	"code.aw.net/claude/tangsible/internal/source"
	"code.aw.net/claude/tangsible/internal/template"
	"code.aw.net/claude/tangsible/internal/uikit"
	"code.aw.net/claude/tangsible/internal/vault"
)

func Main(build BuildInfo) {
	v, args, ok := config.ParseVerb(os.Args[1:])
	if !ok {
		fmt.Fprintf(os.Stderr, "usage: %s <run|rerun|role|revisit|template|host|hosts|vault|version> [<playbook.yml>] [ansible-playbook args...]\n", os.Args[0])
		os.Exit(2)
	}

	if v == config.VerbVersion {
		os.Exit(RunVersion(build))
	}

	// "template" (design-docs/Tangsible template.md), "host"/"hosts"
	// (design-docs/HostVerb.md), "revisit" (design-docs/Revisit.md), and
	// "vault" (design-docs/Vault.md) are each standalone programs - they
	// share none of run/rerun/role's own tree-building machinery below
	// (procH, playbook resolution, the live jsonl pipeline, NewLiveTUI's
	// own construction, ...), so they're split off here before any of
	// that gets set up, rather than threaded through the switch below.
	// "revisit" does still end up inside NewLiveTUI for its own detail
	// view (unlike the other three) - but only once per selected entry,
	// each its own fresh call with a freshly replayed state, never
	// sharing this function's own run/rerun/role setup. "vault" never
	// touches tview at all, unlike every other Verb here.
	if v == config.VerbTemplate {
		os.Exit(template.RunTemplateVerb(args))
	}
	if v == config.VerbHost {
		os.Exit(host.RunHostVerb(args))
	}
	if v == config.VerbHosts {
		os.Exit(host.RunHostsVerb(args))
	}
	if v == config.VerbRevisit {
		os.Exit(revisit.RunRevisitVerb(args, NewLiveTUI))
	}
	if v == config.VerbVault {
		os.Exit(vault.RunVaultVerb(args))
	}

	var procH runner.ProcHandle
	var playbook string
	// spawnPlaybook is what's actually passed to ansible-playbook for this
	// session's first generation - normally identical to playbook, but set
	// to a source.TrimPlaybookToPlay copy by "run"'s own "--start-at-play"
	// handling below (design-docs/StartWithPlay.md's CLI form). Left "" by
	// every other Verb and defaulted to playbook right after the switch -
	// playbook itself always remains the identity AppendInvocation/
	// BuildTaskSourceIndex/history are recorded and built against; a
	// trimmed copy is a spawn-time detail only, exactly as it already is
	// for a mid-session rerun (runner.NewRequestRerun).
	var spawnPlaybook string
	// roleDisplayName is set only for a role-based session ("tangsible
	// role", or "tangsible rerun" resolving to one - design-docs/Tangsible
	// role.md): the role's own name, used in place of playbook's (here,
	// the generated stub's own meaningless filename) for the TUI's top bar
	// and for recording history under role rather than playbook. Captured
	// once, up front, and never changes for the rest of the process's
	// lifetime - a session's role-ness never changes mid-session (a
	// re-run reuses the same stub, see cleanup below, rather than ever
	// regenerating one for a different role).
	var roleDisplayName string
	// cleanup, if non-nil, removes this session's generated role stub -
	// nil for a plain playbook session, nothing to clean up. Deferred
	// unconditionally below so every normal return path (including main's
	// own implicit end) runs it; os.Exit skips deferred functions, so
	// every os.Exit call below the point cleanup might be set instead
	// goes through exitCleanly, which calls it explicitly first.
	var cleanup func()
	defer func() {
		if cleanup != nil {
			cleanup()
		}
	}()
	exitCleanly := func(code int) {
		if cleanup != nil {
			cleanup()
		}
		os.Exit(code)
	}
	// originalArgs is this session's baseline invocation, split into its
	// Tags/Hosts (pre-fills the re-run dialog's fields the first time each
	// is opened - Rerun.md's "if tags were already specified in the
	// previous run... pre-filled") and Rest - everything else (inventory,
	// extra-vars, verbosity, ...), which every rerun since carries forward
	// unedited, since the dialog only ever exposes Task/Tags/Hosts. Set by
	// whichever Verb's branch below runs; read the same way by both.
	var originalArgs config.ParsedPassthroughArgs
	// initialPlay is this session's own --start-at-play, if any (empty for
	// "role", which has no concept of it) - threaded through to
	// NewLiveTUI's own initialPlay param to pre-fill the re-run dialog's
	// Play field the first time it's opened, the same way originalArgs.Tags/
	// Hosts already pre-fill Tags/Hosts. Never itself recorded into
	// .tangsible/state.toml (see ExtractStartAtPlay's own doc comment), so
	// there is nothing for a later bare "tangsible rerun" to fall back to -
	// only an explicit --start-at-play on the "run"/"rerun" command line
	// that started *this* session ever populates it.
	var initialPlay string
	// pending is non-nil only for "run" and "role" - see PendingGeneration's
	// own doc comment.
	var pending *runner.PendingGeneration

	switch v {
	case config.VerbRun:
		// The playbook is normally args[0] (i.e. os.Args[2], after the
		// Verb), but doesn't have to be - SplitPlaybookArgs treats a
		// missing or flag-shaped first argument as "none given
		// positionally" and ResolvePlaybook takes over (see resolve.go for
		// the full TANGSIBLE_PLAYBOOK/.tangsible/config.toml/
		// $XDG_CONFIG_HOME/site.yml cascade).
		var rest []string
		var explicit bool
		playbook, rest, explicit = config.SplitPlaybookArgs(args)
		if !explicit {
			playbook, _ = config.ResolvePlaybook()
			if playbook == "" {
				fmt.Fprintf(os.Stderr, "usage: %s run [<playbook.yml>] [ansible-playbook args...]\n", os.Args[0])
				fmt.Fprintln(os.Stderr, "no playbook given, and none could be determined from TANGSIBLE_PLAYBOOK, .tangsible/config.toml, $XDG_CONFIG_HOME/tangsible/config.toml, or ./site.yml")
				os.Exit(2)
			}
		}

		// "--start-at-play" (design-docs/StartWithPlay.md's CLI form) is
		// Tangsible's own synthetic flag, understood by no real
		// ansible-playbook - ExtractStartAtPlay pulls it (and its value)
		// out of rest before anything else touches rest, so it never
		// reaches AppendInvocation's own history string (a future bare
		// "tangsible rerun" replays Rest verbatim, straight at
		// ansible-playbook, which would reject an unrecognized flag) or
		// SpawnGeneration itself.
		var startAtPlay string
		startAtPlay, rest = config.ExtractStartAtPlay(rest)
		initialPlay = startAtPlay
		spawnPlaybook = playbook
		if startAtPlay != "" {
			// Resolved - and any failure reported - before AppendInvocation
			// below, same as the "no playbook could be resolved" failure
			// above: an invocation that's invalid before ansible-playbook
			// is ever involved isn't worth recording into history at all.
			tempPath, cleanupTemp, ok, err := source.TrimPlaybookToPlay(playbook, startAtPlay)
			switch {
			case err != nil:
				fmt.Fprintf(os.Stderr, "tangsible: couldn't prepare a trimmed copy of %s for play %q: %v\n", playbook, startAtPlay, err)
				os.Exit(1)
			case !ok:
				fmt.Fprintf(os.Stderr, "tangsible: no play named %q found in %s\n", startAtPlay, playbook)
				os.Exit(1)
			default:
				spawnPlaybook = tempPath
				// Reuses the same cleanup/defer machinery already in place
				// for a role session's own stub playbook (see cleanup's own
				// doc comment above) - this session has nothing else that
				// would ever set it, "run" never being a role session
				// itself. Ties the temp file's lifetime to the whole
				// process, not just this first generation, exactly like the
				// role stub's own lifetime - simpler than the per-generation
				// cleanup runner.NewRequestRerun uses for a later,
				// interactive rerun, and harmless at this project's
				// interactive, short-lived-process scale.
				cleanup = cleanupTemp
			}
		}

		// Recorded unconditionally, before ansible-playbook is even
		// started - same "an invocation is an invocation" semantics as
		// shell history, independent of whether the run itself goes on to
		// succeed, fail, or never gets past ansible-playbook's own
		// pre-flight checks. Non-fatal: losing the ability to pre-fill a
		// future rerun dialog is never worth aborting the run the user
		// actually asked for. Unlike "rerun" (see below), "run" always
		// records immediately - there's no confirmation step to wait for,
		// the invocation already happened by definition. Recorded against
		// rest (startAtPlay already stripped out above, if it was there at
		// all) and playbook (the original path, never spawnPlaybook) - a
		// future "tangsible rerun" should replay the real playbook with
		// real ansible-playbook args, not reach for a temp file that may
		// already be gone.
		if err := config.AppendInvocation(config.TangsibleStatePath, playbook, "", config.ArgsToHistoryString(rest)); err != nil {
			fmt.Fprintf(os.Stderr, "tangsible: couldn't record invocation history in %s: %v\n", config.TangsibleStatePath, err)
		}
		originalArgs = config.ParsePassthroughArgs(rest)

		var showTUI bool
		pending, showTUI = runner.StartFirstGeneration(spawnPlaybook, rest, &procH, playbook, "", cleanup)
		if !showTUI {
			return
		}

	case config.VerbRole:
		// The role name is always required, positionally - unlike "run"'s
		// playbook, there's no config/env fallback cascade for it
		// (design-docs/Tangsible role.md only ever specifies
		// "tangsible role <role_name>"). SplitPlaybookArgs's own
		// shape-based rule (a missing or flag-shaped first argument means
		// nothing was given) applies identically here - a role name can't
		// start with '-' in practice either.
		roleName, rest, explicit := config.SplitPlaybookArgs(args)
		if !explicit {
			fmt.Fprintf(os.Stderr, "usage: %s role <role_name> [ansible-playbook args...]\n", os.Args[0])
			os.Exit(2)
		}

		playbook, cleanup = role.StartRoleSession(roleName)
		roleDisplayName = roleName

		// Recorded unconditionally, before ansible-playbook is even
		// started - same "an invocation is an invocation" semantics "run"
		// already has, and for the same reason: losing the ability to
		// pre-fill a future rerun is never worth aborting the run the user
		// actually asked for.
		if err := config.AppendInvocation(config.TangsibleStatePath, "", roleName, config.ArgsToHistoryString(rest)); err != nil {
			fmt.Fprintf(os.Stderr, "tangsible: couldn't record invocation history in %s: %v\n", config.TangsibleStatePath, err)
		}
		originalArgs = config.ParsePassthroughArgs(rest)

		var showTUI bool
		pending, showTUI = runner.StartFirstGeneration(playbook, rest, &procH, "", roleName, cleanup)
		if !showTUI {
			return
		}

	case config.VerbRerun:
		// "--start-at-play" is pulled out of args before ResolveRerun ever
		// sees the rest - same reasoning and same helper as "run"'s own
		// handling above: it's Tangsible's own synthetic flag, understood
		// by no real ansible-playbook, so it must never survive into
		// ResolveRerun's own Rest (which a later bare "tangsible rerun"
		// would otherwise replay verbatim, straight at ansible-playbook,
		// which would reject it as unrecognized). Unlike "run", it's never
		// resolved/validated here - there's no generation to spawn yet,
		// just the dialog to pre-fill (see initialPlay below); the exact
		// same TrimPlaybookToPlay validation "run" does synchronously here
		// instead runs at dialog-confirm time, in requestRerun
		// (runner.NewRequestRerun), whether the Play field's value came
		// from this pre-fill or was typed by hand.
		var rerunArgs []string
		initialPlay, rerunArgs = config.ExtractStartAtPlay(args)

		// No history/CLI-args resolution happens for "run" (its playbook
		// argument is passed straight through, verbatim, same as always) -
		// this is entirely new machinery, see rerunresolve.go. Read fresh
		// rather than threaded through from anywhere else, since this is
		// the only place in "rerun"'s own flow that needs it.
		cfg := config.ReadState(config.TangsibleStatePath)
		res, resolved := config.ResolveRerun(rerunArgs, cfg)
		if !resolved {
			fmt.Fprintf(os.Stderr, "usage: %s rerun [<playbook.yml>] [ansible-playbook args...]\n", os.Args[0])
			fmt.Fprintln(os.Stderr, "no playbook or role given, and nothing has ever been run in this project to rerun")
			os.Exit(2)
		}
		// res.Role set (rather than res.Playbook) means the most recent
		// invocation in this project was "tangsible role", not
		// "tangsible run" (design-docs/Tangsible role.md) - only possible
		// when no playbook was given explicitly (see RerunResolution's own
		// doc comment: an explicit positional argument to "rerun" always
		// means a playbook, there's no "tangsible rerun <role>" form). A
		// role rerun always starts from a brand new stub - the previous
		// session's own was already deleted when that process exited -
		// exactly like "tangsible role" itself, via the same
		// StartRoleSession helper.
		if res.Role != "" {
			playbook, cleanup = role.StartRoleSession(res.Role)
			roleDisplayName = res.Role
		} else {
			playbook = res.Playbook
		}
		originalArgs = config.ParsedPassthroughArgs{Tags: res.Tags, SkipTags: res.SkipTags, Hosts: res.Hosts, Rest: res.Rest}
		// pending stays nil: unlike "run"/"role", nothing is spawned yet -
		// the re-run dialog opens immediately instead (NewLiveTUI's
		// startWithRerunDialog below), and the very first generation only
		// starts once the user confirms it, via the exact same
		// requestRerun path every later re-run already goes through.
	}

	// spawnPlaybook is only ever set above, by "run"'s own "--start-at-play"
	// handling - every other Verb spawns against playbook itself.
	if spawnPlaybook == "" {
		spawnPlaybook = playbook
	}

	// Built synchronously - parsing a project's own YAML files is expected
	// to be well under the noise floor of an interactive ansible run at
	// this project's stated ~10-host target scale, so this isn't worth
	// backgrounding. Unaffected by a rerun - still the same playbook
	// (Rerun.md's interactive re-run never changes it), so there's no need
	// to rebuild this per generation. For "run" this is deliberately after
	// the pre-flight gate above, so a bad playbook path/parse error
	// doesn't pay for it - "rerun" has no such gate to be after (see
	// pending's own case above), so it's simply built before anything
	// else instead.
	sourceIndex, knownTags, knownTaskNames := source.BuildTaskSourceIndex(playbook)
	knownPlayNames := source.ListTopLevelPlayNames(playbook)
	if spawnPlaybook != playbook {
		// "--start-at-play" spawns this first generation against a trimmed
		// copy, not playbook itself - every RawEvent.Task.Path for a task
		// defined directly in the top-level playbook (as opposed to one
		// reached via roles:/include_tasks/import_tasks, whose own file is
		// untouched) now points into that copy's own path/line numbers,
		// which sourceIndex (built from playbook, above) can't contain.
		// See MergeSourceIndex's own doc comment - same fix
		// runner.NewRequestRerun applies for a later, interactive rerun.
		source.MergeSourceIndex(sourceIndex, spawnPlaybook)
	}

	// progH holds the current (or about-to-run) generation's own
	// "Task x/y" progress skeleton (progress.go) - an atomic.Pointer
	// since tui.go's OnTaskAdded hook and TopBarText rendering both read
	// it from whatever goroutine is running at the time (tview's event
	// loop, same as everywhere else in this file that isn't itself
	// mutating PlaybookState directly). Built synchronously here, same
	// reasoning as sourceIndex just above, for "run"/"role"'s very first
	// generation (pending != nil - the pre-flight gate already confirmed
	// a real run is happening): this is a prototype, and getting the
	// skeleton in place before any real task-start event can possibly
	// arrive is worth more, for now, than shaving startup latency -
	// unlike sourceIndex, this shells out to a second real
	// ansible-playbook invocation, so the cost is closer to (very
	// roughly) doubling ansible's own startup time than to a plain YAML
	// parse. Left unset for "rerun" (pending == nil): nothing has run
	// yet, and requestRerun below builds this exact same way once the
	// dialog is actually confirmed, using whatever args the user ends up
	// submitting.
	var progH atomic.Pointer[runner.ProgressTracker]
	if pending != nil {
		progH.Store(runner.NewProgressTracker(runner.BuildProgressSkeleton(spawnPlaybook, originalArgs.Reassemble())))
	}

	// Read fresh here rather than threading through the "rerun" branch's
	// own cfg local (which is scoped to that switch case, and is now a
	// StateConfig rather than the SettingsConfig this needs anyway) - a
	// second read of a small, local TOML file is cheap and consistent with
	// how ResolvePlaybook/ReadDefaultPlaybook already re-read it
	// independently elsewhere, rather than passing one shared value
	// through the whole program.
	startExpanded := config.DefaultTreeExpanded(config.ReadSettingsConfig(config.TangsibleConfigPath))
	twoPaneLayout := config.TwoPaneLayoutEnabled(config.ReadSettingsConfig(config.TangsibleConfigPath))
	colorEnabled := config.ColorEnabledByUser(config.ReadSettingsConfig(config.TangsibleConfigPath))

	state := &pb.PlaybookState{}
	var processDone, quitting atomic.Bool
	var exitCode atomic.Int32
	if pending == nil {
		// "rerun": no generation is in flight yet, or ever has been - true
		// is what's accurate here, and what NewLiveTUI's
		// startWithRerunDialog handling expects (see its own doc comment
		// in tui.go for exactly what this does and doesn't unlock before
		// anything has actually run).
		processDone.Store(true)
	}

	var outcomesMu sync.Mutex
	var outcomes []runner.GenerationOutcome // one appended per generation - see
	// GenerationOutcome; read back only after app.Run() returns below.

	var applyLive func(pb.RawEvent)
	apply := func(item runner.StreamItem) {
		if item.IsEvent && !quitting.Load() {
			applyLive(item.Ev)
		}
	}

	// recordOutcome accumulates one generation's outcome for later printing,
	// once app.Run() finally returns and the real terminal is restored -
	// shared by runGeneration below and by runner.NewRequestRerun's own spawn
	// failure path.
	recordOutcome := func(o runner.GenerationOutcome) {
		outcomesMu.Lock()
		outcomes = append(outcomes, o)
		outcomesMu.Unlock()
	}

	// runGeneration/requestRerun are thin, session-local wrappers around
	// runner.RunOneGeneration/runner.NewRequestRerun (generation.go) - the actual
	// mechanism is shared with revisit.go's own rerun-from-within-
	// "revisit" (design-docs/Revisit.md's Phase 3); this session's own
	// playbook/roleDisplayName/state/procH/processDone/exitCode/progH/
	// apply/recordOutcome are just what it closes over here.
	runGeneration := func(cmd *exec.Cmd, stdoutCh <-chan runner.StreamItem, stderrLines <-chan []string, runID string, peeked ...runner.StreamItem) {
		runner.RunOneGeneration(cmd, stdoutCh, stderrLines, runID, playbook, roleDisplayName, apply, &exitCode, &processDone, recordOutcome, peeked...)
	}
	requestRerun := runner.NewRequestRerun(playbook, roleDisplayName, originalArgs.Rest, state, &procH, &processDone, &exitCode, &progH, apply, recordOutcome, sourceIndex)

	// displayName is what the TUI's top bar shows - normally the resolved
	// playbook's own filename, but a role session's playbook local holds
	// its generated stub's own (meaningless) filename instead, so the
	// role's own name is shown there in that case - design-docs/Tangsible
	// role.md's own "loose end".
	displayName := filepath.Base(playbook)
	targetPlaybook, targetRole := playbook, ""
	if roleDisplayName != "" {
		displayName = roleDisplayName
		targetPlaybook, targetRole = "", roleDisplayName
	}
	app, applyLive := NewLiveTUI(state, displayName, roleDisplayName != "", &procH, &processDone, &quitting, &exitCode, sourceIndex, knownTags, knownTaskNames, knownPlayNames, startExpanded, twoPaneLayout, colorEnabled, initialPlay, originalArgs.Tags, originalArgs.SkipTags, originalArgs.Hosts, pending == nil, requestRerun, originalArgs.Rest, &progH, nil, targetPlaybook, targetRole)

	if pending != nil {
		go runGeneration(pending.Cmd, pending.StdoutCh, pending.StderrLines, pending.RunID, pending.First)
	}

	runErr := app.Run()
	quitting.Store(true) // defensive: also stop the streamer if Run() ever
	// returns for a reason other than our own Stop()

	outcomesMu.Lock()
	all := outcomes
	outcomesMu.Unlock()

	// A 99 exit means the user asked us (via q/Ctrl-C) to interrupt that
	// generation's run - not a failure. Suppress the stderr lines that
	// would otherwise read like an error report for something the user
	// deliberately did. Printed in generation order, oldest first, so a
	// mid-session rerun doesn't erase what an earlier generation reported -
	// that generation's own tree view is long gone by the time Tangsible
	// finally exits (Rerun.md's re-run forgets the previous run's results),
	// so this is the only remaining record of it.
	for _, o := range all {
		if o.ExitCode != runner.AnsibleUserInterruptedExitCode {
			for _, l := range o.ChildStderr {
				fmt.Fprintln(os.Stderr, "[ansible-playbook stderr]", l)
			}
		}
	}

	if runErr != nil {
		fmt.Fprintln(os.Stderr, "TUI error:", runErr)
		exitCleanly(1)
	}
	if len(all) == 0 {
		// Shouldn't happen via our own app.Stop() path - that's only ever
		// called once processDone is observed true, which itself only
		// happens right after a generation's outcome is recorded - but
		// guard against tview returning from Run() early for some other
		// reason instead of indexing into an empty slice below.
		return
	}

	// final is the LAST generation's outcome - only it decides Tangsible's
	// own exit status; state.HadUnreachable likewise reflects only the
	// current (== last) generation, since requestRerun's state.Reset()
	// clears it at the start of every generation but the first. See
	// tui.go's GenuineFailure for exactly what counts as a real failure
	// here (as opposed to a benign "some host(s) unreachable" run or a
	// user-requested interrupt) - the same logic tui.go's own status row
	// already renders, reused here rather than reimplemented so the two
	// can't silently drift apart on what "failed" means.
	final := all[len(all)-1]
	if uikit.GenuineFailure(final.ExitCode, state.HadUnreachable, runner.AnsibleUserInterruptedExitCode) {
		fmt.Fprintln(os.Stderr, "ansible-playbook exited with error:", final.WaitErr)
		exitCleanly(1)
	}
	// A user-interrupted run (exit 99) or a benign "some host(s) were
	// unreachable" run (exit 4 with independently-observed evidence) falls
	// through to a normal return (implicit exit code 0) - neither is a
	// failure of Tangsible or of the playbook itself.
}
