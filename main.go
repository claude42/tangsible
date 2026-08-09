// Shells out to ansible-playbook using the ansible.posix.jsonl stdout
// callback and streams events live into an interactive TUI as they arrive.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
)

// ansibleUserInterruptedExitCode is ansible-playbook's documented exit code
// for "user interrupted execution" (its own CLI exit-code table). In
// Tangsible specifically, the only source of SIGINT to the child is our own
// SetInputCapture handler (tcell's raw mode disables the OS's normal
// Ctrl-C-to-SIGINT delivery) - so this code unambiguously means "the user
// asked us to stop this run," never a signal from anywhere else.
const ansibleUserInterruptedExitCode = 99

// exitCodeOf extracts ansible-playbook's process exit code from the error
// returned by cmd.Wait(), or 0 if it exited cleanly. Returns -1 for a
// non-ExitError failure from Wait() itself (e.g. an I/O error) - not a real
// exit code, but distinct from every real one (0-255), so it never
// accidentally matches ansibleUserInterruptedExitCode or 0.
func exitCodeOf(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

// procHandle holds the *os.Process that Ctrl-C/q should signal to interrupt
// a running invocation (tui.go's SetInputCapture). Mutable - unlike a plain
// *os.Process passed once at startup - so a rerun (Rerun.md) can point it
// at a freshly spawned child without tui.go needing to know a restart ever
// happened. Store is called from whichever goroutine just spawned a new
// generation (see spawnGeneration); Load from SetInputCapture, on tview's
// own event-loop goroutine - atomic.Pointer makes that safe with no
// separate lock. Never observed nil once spawnGeneration has run at least
// once: the very first call happens before the TUI (and so before
// SetInputCapture) exists at all, and every later one only replaces an
// already-non-nil value.
type procHandle struct {
	p atomic.Pointer[os.Process]
}

func (h *procHandle) Store(p *os.Process) { h.p.Store(p) }
func (h *procHandle) Load() *os.Process   { return h.p.Load() }

// pendingGeneration is the "run" verb's own first generation - already
// spawned and past the pre-flight gate by the time the TUI exists, unlike
// every rerun since (including, for the "rerun" verb, its very first one -
// see requestRerun) which only ever starts once a re-run dialog is
// confirmed. nil for the "rerun" verb: nothing has been spawned yet when
// the TUI is constructed for it.
type pendingGeneration struct {
	cmd         *exec.Cmd
	stdoutCh    <-chan streamItem
	stderrLines <-chan []string
	first       streamItem
}

// generationOutcome is one ansible-playbook invocation's result. main
// accumulates one per generation - the first invocation, plus every rerun
// since (Rerun.md) - so every generation's stderr/diagnostics still get
// printed once Tangsible finally exits, not just the last one, even though
// only the LAST generation's exit code decides Tangsible's own exit status.
type generationOutcome struct {
	exitCode    int
	waitErr     error
	childStderr []string
	diagnostics []string
}

// spawnGeneration starts one ansible-playbook invocation for playbook+args,
// wiring up its stdout/stderr exactly as every generation needs (see
// scanEvents/streamStderr) and pointing procH at the new child so Ctrl-C/q
// forwarding targets it. Shared by the first invocation and every rerun
// since - the only thing that differs between them is what main does with
// the first item off the returned channel (see main's pre-flight gate,
// which only ever applies to the first invocation - a rerun's own
// pre-flight failure has nowhere to hide the already-visible TUI from, so
// it just renders as a failed generation like any other, no gate needed).
func spawnGeneration(playbook string, args []string, procH *procHandle) (cmd *exec.Cmd, stdoutCh <-chan streamItem, stderrLines <-chan []string, err error) {
	cmd = exec.Command("ansible-playbook", append([]string{playbook}, args...)...)
	cmd.Env = append(os.Environ(),
		"ANSIBLE_STDOUT_CALLBACK=ansible.posix.jsonl",
		// Pin compact (single-line) JSON so our line-based scanner can't be
		// broken by a user's ansible.cfg overriding this to pretty-print.
		"ANSIBLE_JSON_INDENT=0",
	)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to attach stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to attach stderr: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to start ansible-playbook: %w", err)
	}
	procH.Store(cmd.Process)

	stdoutCh = scanEvents(stdout)
	lines := make(chan []string, 1)
	go func() { lines <- streamStderr(stderr) }()

	return cmd, stdoutCh, lines, nil
}

func main() {
	v, args, ok := parseVerb(os.Args[1:])
	if !ok {
		fmt.Fprintf(os.Stderr, "usage: %s <run|rerun> [<playbook.yml>] [ansible-playbook args...]\n", os.Args[0])
		os.Exit(2)
	}

	var procH procHandle
	var playbook string
	// originalArgs is this session's baseline invocation, split into its
	// Tags/Hosts (pre-fills the re-run dialog's fields the first time each
	// is opened - Rerun.md's "if tags were already specified in the
	// previous run... pre-filled") and Rest - everything else (inventory,
	// extra-vars, verbosity, ...), which every rerun since carries forward
	// unedited, since the dialog only ever exposes Task/Tags/Hosts. Set by
	// whichever verb's branch below runs; read the same way by both.
	var originalArgs parsedPassthroughArgs
	// pending is non-nil only for "run" - see pendingGeneration's own doc
	// comment.
	var pending *pendingGeneration

	switch v {
	case verbRun:
		// The playbook is normally args[0] (i.e. os.Args[2], after the
		// verb), but doesn't have to be - splitPlaybookArgs treats a
		// missing or flag-shaped first argument as "none given
		// positionally" and resolvePlaybook takes over (see resolve.go for
		// the full TANGSIBLE_PLAYBOOK/.tangsible/$XDG_CONFIG_HOME/site.yml
		// cascade).
		var rest []string
		var explicit bool
		playbook, rest, explicit = splitPlaybookArgs(args)
		if !explicit {
			var source string
			playbook, source = resolvePlaybook()
			if playbook == "" {
				fmt.Fprintf(os.Stderr, "usage: %s run [<playbook.yml>] [ansible-playbook args...]\n", os.Args[0])
				fmt.Fprintln(os.Stderr, "no playbook given, and none could be determined from TANGSIBLE_PLAYBOOK, .tangsible, $XDG_CONFIG_HOME/tangsible/config.toml, or ./site.yml")
				os.Exit(2)
			}
			fmt.Fprintf(os.Stderr, "tangsible: no playbook given - using %q (%s)\n", playbook, source)
		}

		// Recorded unconditionally, before ansible-playbook is even
		// started - same "an invocation is an invocation" semantics as
		// shell history, independent of whether the run itself goes on to
		// succeed, fail, or never gets past ansible-playbook's own
		// pre-flight checks. Non-fatal: losing the ability to pre-fill a
		// future rerun dialog is never worth aborting the run the user
		// actually asked for. Unlike "rerun" (see below), "run" always
		// records immediately - there's no confirmation step to wait for,
		// the invocation already happened by definition.
		if err := appendInvocation(tangsibleFilePath, playbook, argsToHistoryString(rest)); err != nil {
			fmt.Fprintf(os.Stderr, "tangsible: couldn't record invocation history in %s: %v\n", tangsibleFilePath, err)
		}
		originalArgs = parsePassthroughArgs(rest)

		cmd, stdoutCh, stderrLines, err := spawnGeneration(playbook, rest, &procH)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}

		// The gate: ansible-playbook writes zero bytes to stdout for a
		// pre-flight failure (bad playbook path, a parse error, a missing
		// inventory, ...) - those are reported entirely via stderr + a
		// nonzero exit code, before any real event ever fires.
		// v2_playbook_on_play_start fires unconditionally as the very
		// first thing any real run does (even for a play with zero tasks -
		// confirmed against ansible-core's TaskQueueManager.run()), so
		// "did at least one line ever arrive on stdout" is a reliable,
		// general signal that ansible-playbook is genuinely running - not
		// a heuristic specific to this one failure mode. Peeking it here,
		// before ever constructing the TUI, is what lets this branch skip
		// showing it entirely rather than showing it empty and waiting for
		// the user to quit before the error becomes visible. "rerun" (see
		// below) has no equivalent of this: its own TUI is already visible
		// by the time anything is ever spawned, so a pre-flight failure
		// has nowhere to hide from and nothing to skip.
		first, ok := <-stdoutCh
		if !ok {
			// Nothing ever arrived: safe to call cmd.Wait() here because
			// scanEvents's goroutine only closes its channel after its
			// scan loop has already hit real EOF on stdout - exec.Cmd's
			// "read the pipes fully before Wait()" contract is satisfied
			// by construction.
			childStderr := <-stderrLines
			waitErr := cmd.Wait()
			for _, l := range childStderr {
				fmt.Fprintln(os.Stderr, "[ansible-playbook stderr]", l)
			}
			if waitErr != nil {
				fmt.Fprintln(os.Stderr, "ansible-playbook exited with error:", waitErr)
				os.Exit(1)
			}
			return
		}
		pending = &pendingGeneration{cmd: cmd, stdoutCh: stdoutCh, stderrLines: stderrLines, first: first}

	case verbRerun:
		// No history/CLI-args resolution happens for "run" (its playbook
		// argument is passed straight through, verbatim, same as always) -
		// this is entirely new machinery, see rerunresolve.go. Read fresh
		// rather than threaded through from anywhere else, since this is
		// the only place in "rerun"'s own flow that needs it.
		cfg := readTangsibleConfig(tangsibleFilePath)
		res, resolved := resolveRerun(args, cfg)
		if !resolved {
			fmt.Fprintf(os.Stderr, "usage: %s rerun [<playbook.yml>] [ansible-playbook args...]\n", os.Args[0])
			fmt.Fprintln(os.Stderr, "no playbook given, and nothing has ever been run in this project to rerun")
			os.Exit(2)
		}
		if _, _, explicit := splitPlaybookArgs(args); !explicit {
			fmt.Fprintf(os.Stderr, "tangsible: no playbook given - re-running %q (last run in this project)\n", res.Playbook)
		}
		playbook = res.Playbook
		originalArgs = parsedPassthroughArgs{Tags: res.Tags, Hosts: res.Hosts, Rest: res.Rest}
		// pending stays nil: unlike "run", nothing is spawned yet - the
		// re-run dialog opens immediately instead (NewLiveTUI's
		// startWithRerunDialog below), and the very first generation only
		// starts once the user confirms it, via the exact same
		// requestRerun path every later re-run already goes through.
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
	sourceIndex := buildTaskSourceIndex(playbook)

	state := &playbookState{}
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
	var outcomes []generationOutcome // one appended per generation - see
	// generationOutcome; read back only after app.Run() returns below.

	var applyLive func(rawEvent)
	apply := func(item streamItem) []string {
		var diagnostics []string
		if item.diag != "" {
			diagnostics = append(diagnostics, item.diag)
		}
		if item.isEvent && !quitting.Load() {
			applyLive(item.ev)
		}
		return diagnostics
	}

	// runGeneration drains one generation's stdout to completion - from
	// whatever's already been peeked off it (peeked, "run"'s pre-flight
	// gate only), through channel close - waits for its process, and
	// records its outcome. Shared by "run"'s own first invocation below
	// and every rerun since, for both verbs (see requestRerun) - so
	// there's exactly one place that knows how a generation finishes.
	runGeneration := func(cmd *exec.Cmd, stdoutCh <-chan streamItem, stderrLines <-chan []string, peeked ...streamItem) {
		var diagnostics []string
		for _, item := range peeked {
			diagnostics = append(diagnostics, apply(item)...)
		}
		for item := range stdoutCh {
			diagnostics = append(diagnostics, apply(item)...)
		}
		childStderr := <-stderrLines // wait for stderr to fully drain before Wait()
		waitErr := cmd.Wait()
		code := exitCodeOf(waitErr)
		exitCode.Store(int32(code)) // before processDone below - tui.go's
		// rebuild() only ever reads exitCode once it observes processDone
		// true, and Go's atomics are sequentially consistent as a whole
		// program (not just per-variable), so this ordering is what makes
		// that store visible there.
		outcomesMu.Lock()
		outcomes = append(outcomes, generationOutcome{exitCode: code, waitErr: waitErr, childStderr: childStderr, diagnostics: diagnostics})
		outcomesMu.Unlock()
		processDone.Store(true)
	}

	// requestRerun is tui.go's hook for starting a new generation mid-
	// session (Rerun.md) - passed into NewLiveTUI below, called once the
	// re-run dialog is confirmed. startAtTask, if non-empty, is prepended
	// as --start-at-task; tags/hosts replace the original invocation's own
	// (originalArgs.Rest is always carried forward unedited alongside
	// them).
	requestRerun := func(startAtTask, tags, hosts string) {
		// Reset synchronously, on whatever goroutine calls this (tview's
		// event-loop goroutine, from the re-run dialog's Enter handler) -
		// by the time this returns, a QueueUpdateDraw-driven rebuild()
		// already sees a running, empty generation, matching the
		// view-state reset tui.go does right alongside calling this.
		state.Reset()
		exitCode.Store(0)
		processDone.Store(false)

		newArgs := parsedPassthroughArgs{Tags: tags, Hosts: hosts, Rest: originalArgs.Rest}.Reassemble()
		if startAtTask != "" {
			newArgs = append([]string{"--start-at-task", startAtTask}, newArgs...)
		}

		// Recorded the same way the original invocation was at the top of
		// main - but its own error, unlike that one, can't be printed
		// here: the TUI's alternate screen is already active by now
		// (unlike the top-level call, which always runs before it exists),
		// and printing directly to the terminal while it's up would
		// corrupt the display. Silently dropped instead - non-fatal, same
		// reasoning as the top-level call: losing the ability to pre-fill
		// a *future* rerun is never worth disrupting the one the user just
		// asked for.
		_ = appendInvocation(tangsibleFilePath, playbook, argsToHistoryString(newArgs))

		go func() {
			cmd, stdoutCh, stderrLines, err := spawnGeneration(playbook, newArgs, &procH)
			if err != nil {
				// Rare (ansible-playbook vanished, pipes failed, ...) and,
				// unlike the same failure on the very first invocation,
				// not fatal to the whole program - the TUI already exists
				// and the user is mid-session. Recorded as this one
				// generation's own failed outcome instead; genuineFailure
				// below renders it the same as any other failed run.
				exitCode.Store(-1)
				outcomesMu.Lock()
				outcomes = append(outcomes, generationOutcome{exitCode: -1, waitErr: err, diagnostics: []string{err.Error()}})
				outcomesMu.Unlock()
				processDone.Store(true)
				return
			}
			runGeneration(cmd, stdoutCh, stderrLines)
		}()
	}

	app, applyLive := NewLiveTUI(state, filepath.Base(playbook), &procH, &processDone, &quitting, &exitCode, sourceIndex, originalArgs.Tags, originalArgs.Hosts, pending == nil, requestRerun)

	if pending != nil {
		go runGeneration(pending.cmd, pending.stdoutCh, pending.stderrLines, pending.first)
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
	// deliberately did; every generation's diagnostics still print
	// unconditionally. Printed in generation order, oldest first, so a
	// mid-session rerun doesn't erase what an earlier generation reported -
	// that generation's own tree view is long gone by the time Tangsible
	// finally exits (Rerun.md's re-run forgets the previous run's results),
	// so this is the only remaining record of it.
	for _, o := range all {
		if o.exitCode != ansibleUserInterruptedExitCode {
			for _, l := range o.childStderr {
				fmt.Fprintln(os.Stderr, "[ansible-playbook stderr]", l)
			}
		}
		for _, l := range o.diagnostics {
			fmt.Println(l)
		}
	}

	if runErr != nil {
		fmt.Fprintln(os.Stderr, "TUI error:", runErr)
		os.Exit(1)
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
	// tui.go's genuineFailure for exactly what counts as a real failure
	// here (as opposed to a benign "some host(s) unreachable" run or a
	// user-requested interrupt) - the same logic tui.go's own status row
	// already renders, reused here rather than reimplemented so the two
	// can't silently drift apart on what "failed" means.
	final := all[len(all)-1]
	if genuineFailure(final.exitCode, state.HadUnreachable) {
		fmt.Fprintln(os.Stderr, "ansible-playbook exited with error:", final.waitErr)
		os.Exit(1)
	}
	// A user-interrupted run (exit 99) or a benign "some host(s) were
	// unreachable" run (exit 4 with independently-observed evidence) falls
	// through to a normal return (implicit exit code 0) - neither is a
	// failure of Tangsible or of the playbook itself.
}

// streamStderr collects the child's stderr lines instead of printing them
// live — printing directly to the terminal while the TUI's alternate
// screen is active would corrupt the display. main prints them after
// app.Run() returns and the real terminal is restored.
func streamStderr(r io.Reader) []string {
	var lines []string
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines
}

// streamItem is one unit of stdout output. isEvent is true whenever the
// line decoded successfully as JSON, independent of whether diag is also
// set - the two are not mutually exclusive: v2_playbook_on_stats sets both
// (isEvent so Apply still sees it, diag for the cross-check dump), while a
// malformed line or a final scanner error sets only diag.
type streamItem struct {
	ev      rawEvent
	isEvent bool
	diag    string
}

// scanEvents reads one JSON object per line from r, decoding each into a
// streamItem sent on the returned channel; the channel is closed once r
// hits EOF or a scan error occurs. Runs on its own goroutine so a caller can
// observe "did anything ever arrive at all" (via the ok value of a channel
// receive) before deciding whether to show the TUI - see main's gate, added
// because ansible-playbook produces zero stdout output for pre-flight
// failures (bad playbook path, parse errors, missing inventory, ...),
// reporting those solely via stderr + a nonzero exit code.
func scanEvents(r io.Reader) <-chan streamItem {
	ch := make(chan streamItem, 64)
	go func() {
		defer close(ch)

		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}

			var ev rawEvent
			if err := json.Unmarshal([]byte(line), &ev); err != nil {
				ch <- streamItem{diag: "(not JSON) " + line}
				continue
			}

			item := streamItem{ev: ev, isEvent: true}
			if ev.Event == "v2_playbook_on_stats" {
				var stats struct {
					Stats map[string]interface{} `json:"stats"`
				}
				json.Unmarshal([]byte(line), &stats)
				pretty, _ := json.MarshalIndent(stats.Stats, "  ", "  ")
				item.diag = fmt.Sprintf("ansible's own final stats (for cross-checking):\n  %s", pretty)
			}
			ch <- item
		}
		if err := scanner.Err(); err != nil {
			ch <- streamItem{diag: fmt.Sprintf("error reading ansible-playbook output: %v", err)}
		}
	}()
	return ch
}
