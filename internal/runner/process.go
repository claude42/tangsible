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

// Package runner is the "how does one ansible-playbook generation
// actually run" mechanism - spawning the subprocess, streaming its
// stdout/stderr, tracking its progress against a --list-tasks skeleton,
// and recording its outcome. Used identically by main.go's own
// run/rerun/role session and by revisit.go's rerun-from-within-"revisit"
// (see generation.go's own doc comment, folded into this package rather
// than kept with a future "revisit" package - it shares nothing with
// revisit.go/revisitresolve.go beyond both calling into this package).
package runner

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"code.aw.net/claude/tangsible/internal/config"
	"code.aw.net/claude/tangsible/internal/execerr"
	pb "code.aw.net/claude/tangsible/internal/playbook"
)

// AnsibleUserInterruptedExitCode is ansible-playbook's documented exit code
// for "user interrupted execution" (its own CLI exit-code table). In
// Tangsible specifically, the only source of SIGINT to the child is our own
// SetInputCapture handler (tcell's raw mode disables the OS's normal
// Ctrl-C-to-SIGINT delivery) - so this code unambiguously means "the user
// asked us to stop this run," never a signal from anywhere else.
const AnsibleUserInterruptedExitCode = 99

// ExitCodeOf extracts ansible-playbook's process exit code from the error
// returned by cmd.Wait(), or 0 if it exited cleanly. Returns -1 for a
// non-ExitError failure from Wait() itself (e.g. an I/O error) - not a real
// exit code, but distinct from every real one (0-255), so it never
// accidentally matches AnsibleUserInterruptedExitCode or 0.
func ExitCodeOf(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

// ProcHandle holds the *os.Process that Ctrl-C/q should signal to interrupt
// a running invocation (tui.go's SetInputCapture). Mutable - unlike a plain
// *os.Process passed once at startup - so a rerun (Rerun.md) can point it
// at a freshly spawned child without tui.go needing to know a restart ever
// happened. Store is called from whichever goroutine just spawned a new
// generation (see SpawnGeneration); Load from SetInputCapture, on tview's
// own event-loop goroutine - atomic.Pointer makes that safe with no
// separate lock. Never observed nil once SpawnGeneration has run at least
// once: the very first call happens before the TUI (and so before
// SetInputCapture) exists at all, and every later one only replaces an
// already-non-nil value.
type ProcHandle struct {
	p atomic.Pointer[os.Process]
}

func (h *ProcHandle) Store(p *os.Process) { h.p.Store(p) }
func (h *ProcHandle) Load() *os.Process   { return h.p.Load() }

// PendingGeneration is the "run" Verb's own first generation - already
// spawned and past the pre-flight gate by the time the TUI exists, unlike
// every rerun since (including, for the "rerun" Verb, its very first one -
// see requestRerun) which only ever starts once a re-run dialog is
// confirmed. nil for the "rerun" Verb: nothing has been spawned yet when
// the TUI is constructed for it.
type PendingGeneration struct {
	Cmd         *exec.Cmd
	StdoutCh    <-chan StreamItem
	StderrLines <-chan []string
	First       StreamItem
	RunID       string
}

// GenerationOutcome is one ansible-playbook invocation's result. main
// accumulates one per generation - the first invocation, plus every rerun
// since (Rerun.md) - so every generation's stderr still gets printed once
// Tangsible finally exits, not just the last one, even though only the LAST
// generation's exit code decides Tangsible's own exit status.
type GenerationOutcome struct {
	ExitCode    int
	WaitErr     error
	ChildStderr []string
}

// SpawnGeneration starts one ansible-playbook invocation for playbook+args,
// wiring up its event stream/stderr exactly as every generation needs (see
// ScanEvents/StreamStderr) and pointing procH at the new child so Ctrl-C/q
// forwarding targets it. The returned stdoutCh - named for historical
// continuity with the days when ansible.posix.jsonl wrote events straight
// to stdout - is now actually sourced from the dedicated event fd our own
// bundled plugin writes to (design-docs/OwnCallbackPlugin.md); the child's
// real stdout is left untouched for the user's own stdout callback. Shared
// by the first invocation and every rerun
// since - the only thing that differs between them is what main does with
// the first item off the returned channel (see main's pre-flight gate,
// which only ever applies to the first invocation - a rerun's own
// pre-flight failure has nowhere to hide the already-visible TUI from, so
// it just renders as a failed generation like any other, no gate needed).
//
// runID names this generation's own saved run data (design-docs/
// Revisit.md, runlog.go) - "" if CreateRunLog couldn't actually open
// anything to save it to, so a caller never records a RunID (via
// FinalizeInvocation) that no file backs.
//
// stdout, if non-nil, receives the child's real stdout (the user's own
// stdout callback's output); nil keeps today's default of discarding it
// (exec redirects a nil cmd.Stdout to /dev/null). Only StartFirstGeneration
// passes one, for its pre-flight gate (see gateStdout).
func SpawnGeneration(playbook string, args []string, procH *ProcHandle, stdout io.Writer) (cmd *exec.Cmd, stdoutCh <-chan StreamItem, stderrLines <-chan []string, runID string, err error) {
	// --diff is always appended to the actual subprocess argv (never to
	// args itself, which is also what's reassembled into .tangsible's
	// history/rerun args) so the drill-down view's Diff tab
	// (BuildDiffTab, tui.go) has something to show whenever a module
	// supports diff mode - unconditionally, not just when the user
	// happens to pass --diff themselves. Harmless if they did anyway:
	// ansible-playbook tolerates a repeated boolean flag.
	cmdArgs := make([]string, 0, len(args)+2)
	cmdArgs = append(cmdArgs, playbook)
	cmdArgs = append(cmdArgs, args...)
	cmdArgs = append(cmdArgs, "--diff")
	cmd = exec.Command("ansible-playbook", cmdArgs...)

	pluginEnv, err := CallbackPluginEnv()
	if err != nil {
		return nil, nil, nil, "", err
	}

	// The plugin (design-docs/OwnCallbackPlugin.md) is an 'aggregate'
	// callback, not a 'stdout' one, so it needs a dedicated transport
	// rather than the stdout pipe: a pipe whose write end is inherited by
	// the child as fd 3 (ExtraFiles' documented numbering - fd 3 is the
	// first slot after the child's own stdin/stdout/stderr), named to the
	// plugin via TANGSIBLE_EVENT_FD. This frees stdout for the user's own
	// stdout callback (left at ansible-playbook's default unless the user
	// configured one) - discarded unless the caller asked for it via
	// stdout, since surfacing it in the live TUI is Phase 2.
	cmd.Stdout = stdout
	eventR, eventW, err := os.Pipe()
	if err != nil {
		return nil, nil, nil, "", fmt.Errorf("failed to create event pipe: %w", err)
	}
	cmd.ExtraFiles = []*os.File{eventW}

	cmd.Env = append(append(os.Environ(), pluginEnv...), "TANGSIBLE_EVENT_FD=3")

	stderr, err := cmd.StderrPipe()
	if err != nil {
		eventR.Close()
		eventW.Close()
		return nil, nil, nil, "", fmt.Errorf("failed to attach stderr: %w", err)
	}
	if err := cmd.Start(); err != nil {
		eventR.Close()
		eventW.Close()
		if msg := execerr.NotFoundMessage("ansible-playbook", err); msg != "" {
			return nil, nil, nil, "", errors.New(msg)
		}
		return nil, nil, nil, "", fmt.Errorf("failed to start ansible-playbook: %w", err)
	}
	procH.Store(cmd.Process)

	// Our own copy of the write end must close now, not just the child's:
	// eventR only sees EOF once every copy of the write end is closed, and
	// exec.Cmd never closes ExtraFiles entries itself.
	eventW.Close()

	runID = config.NewRunID(time.Now())
	logFile := config.CreateRunLog(config.TangsibleStatePath, runID)
	if logFile == nil {
		runID = ""
	}

	// ScanEvents takes ownership of eventR from here on and closes it once
	// scanning ends (see its own doc comment) - no explicit eventR.Close()
	// needed on this success path, unlike the two failure returns above
	// (which never reach ScanEvents at all).
	stdoutCh = ScanEvents(eventR, logFile)
	lines := make(chan []string, 1)
	go func() { lines <- StreamStderr(stderr) }()

	return cmd, stdoutCh, lines, runID, nil
}

// StartFirstGeneration spawns playbook+rest as this session's first
// generation and runs the same pre-flight gate "run" has always had - a
// bad playbook path, a parse error, a missing inventory, or (for
// "tangsible role") a role ansible itself can't resolve either all fail
// before any real event ever fires, writing zero bytes to stdout. showTUI
// is false when nothing ever arrived and the run turned out to need no
// TUI at all (a clean pre-flight failure already reported to stderr, or a
// genuinely empty-but-successful run, e.g. --list-tasks) - the caller
// should just return immediately in that case, exactly as "run" always
// has. cleanup, if non-nil, is called before every os.Exit path this
// function can take, and is expected to also be wired by the caller (via
// defer) to run on its own eventual return - "run" passes nil (nothing to
// clean up), "role" passes a func that removes its own stub playbook.
//
// histPlaybook/histRole (exactly one non-empty, mirroring AppendInvocation's
// own playbook/role parameters) are what this generation's invocation
// history entry was recorded under - needed here only for the pre-flight-
// failure branch below, which finalizes that entry itself (exitCode, and a
// RunID if anything was actually saved) since it bypasses main's own
// runGeneration entirely.
func StartFirstGeneration(playbook string, rest []string, procH *ProcHandle, histPlaybook, histRole string, cleanup func()) (pending *PendingGeneration, showTUI bool) {
	if cleanup == nil {
		cleanup = func() {}
	}

	childStdout := &gateStdout{}
	cmd, stdoutCh, stderrLines, runID, err := SpawnGeneration(playbook, rest, procH, childStdout)
	if err != nil {
		cleanup()
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// v2_playbook_on_play_start fires unconditionally as the very first
	// thing any real run does (even for a play with zero tasks - confirmed
	// against ansible-core's TaskQueueManager.run()), so "did at least one
	// line ever arrive on stdout" is a reliable, general signal that
	// ansible-playbook is genuinely running - not a heuristic specific to
	// one failure mode. Peeking it here, before ever constructing the TUI,
	// is what lets the caller skip showing it entirely rather than showing
	// it empty and waiting for the user to quit before the error becomes
	// visible. "rerun" has no equivalent of this: its own TUI is already
	// visible by the time anything is ever spawned, so a pre-flight
	// failure has nowhere to hide from and nothing to skip.
	first, ok := <-stdoutCh
	if !ok {
		// Nothing ever arrived: safe to call cmd.Wait() here because
		// ScanEvents's goroutine only closes its channel after its scan
		// loop has already hit real EOF on stdout - exec.Cmd's "read the
		// pipes fully before Wait()" contract is satisfied by
		// construction.
		cleanup()
		childStderr := <-stderrLines
		waitErr := cmd.Wait()
		config.WriteRunStderr(config.TangsibleStatePath, runID, childStderr)
		if histRole != "" {
			_ = config.FinalizeInvocation(config.TangsibleStatePath, "", histRole, ExitCodeOf(waitErr), runID)
		} else {
			_ = config.FinalizeInvocation(config.TangsibleStatePath, histPlaybook, "", ExitCodeOf(waitErr), runID)
		}
		for _, l := range childStderr {
			fmt.Fprintln(os.Stderr, "[ansible-playbook stderr]", l)
		}
		// Printed verbatim (no prefix, unlike stderr above) so e.g.
		// --list-tasks/--list-hosts/--syntax-check output - which bypasses
		// the callback system entirely, so never opens the TUI - reads
		// and pipes exactly as a plain ansible-playbook run's would.
		// cmd.Wait() above has already finished copying it.
		out, truncated := childStdout.contents()
		os.Stdout.Write(out)
		if truncated {
			fmt.Fprintf(os.Stderr, "tangsible: ansible-playbook output truncated after %d bytes\n", gateStdoutMax)
		}
		if waitErr != nil {
			fmt.Fprintln(os.Stderr, "ansible-playbook exited with error:", waitErr)
			os.Exit(1)
		}
		return nil, false
	}
	// The TUI is about to own the terminal; nothing will ever print this
	// generation's stdout now, so stop retaining it (the child keeps
	// writing, gateStdout keeps draining).
	childStdout.discard()
	return &PendingGeneration{Cmd: cmd, StdoutCh: stdoutCh, StderrLines: stderrLines, First: first, RunID: runID}, true
}

// gateStdoutMax caps how much of the first generation's stdout gateStdout
// retains - far more than any --list-tasks/--list-hosts listing at this
// app's ~10-host target scale, while keeping a verbose run's output from
// growing without bound before the gate has decided anything.
const gateStdoutMax = 4 << 20

// gateStdout is the first generation's cmd.Stdout: retains up to
// gateStdoutMax bytes so StartFirstGeneration can print them if the
// pre-flight gate declines the TUI, and switches to discarding everything
// once discard() is called. Always reports a full write - it must never
// make the child's stdout pipe back up (exec's copy goroutine would stop
// draining it on a short write/error). Guarded by a mutex since exec's own
// copy goroutine calls Write while StartFirstGeneration calls discard().
type gateStdout struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	truncated bool
	discarded bool
}

func (g *gateStdout) Write(p []byte) (int, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.discarded {
		return len(p), nil
	}
	keep := p
	if room := gateStdoutMax - g.buf.Len(); len(keep) > room {
		keep = keep[:room]
		g.truncated = true
	}
	g.buf.Write(keep)
	return len(p), nil
}

func (g *gateStdout) discard() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.discarded = true
	g.buf = bytes.Buffer{}
}

func (g *gateStdout) contents() ([]byte, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.buf.Bytes(), g.truncated
}

// StreamStderr collects the child's stderr lines instead of printing them
// live — printing directly to the terminal while the TUI's alternate
// screen is active would corrupt the display. main prints them after
// app.Run() returns and the real terminal is restored.
//
// Reads via bufio.Reader.ReadBytes rather than bufio.Scanner deliberately -
// a Scanner has a hard per-token buffer cap (its own default is a mere
// 64KiB), and this reads directly off the child's live stderr pipe, not an
// already-fully-buffered byte slice: a single line past that cap would make
// Scan() return false and this function return early while the child is
// still writing, abandoning the read end of a pipe the child hasn't
// finished with - the child then blocks on a full pipe forever, hanging
// both processes. ReadBytes has no such cap (bounded only by memory, fine
// at this app's own ~10-host target scale), so an oversized line just costs
// one large allocation instead of a deadlock.
func StreamStderr(r io.Reader) []string {
	var lines []string
	reader := bufio.NewReader(r)
	for {
		raw, readErr := reader.ReadBytes('\n')
		raw = bytes.TrimSuffix(raw, []byte("\n"))
		raw = bytes.TrimSuffix(raw, []byte("\r"))
		if len(raw) > 0 || readErr == nil {
			lines = append(lines, string(raw))
		}
		if readErr != nil {
			return lines
		}
	}
}

// FilterRedundantWarnings drops every line whose trimmed text starts with
// "[WARNING]:" - ansible-core's own stable, long-standing prefix for
// Display.warning() output. Confirmed live (design-docs/
// OwnCallbackPlugin.md's own testing): a per-task/per-host warning printed
// this way is always *also* embedded in that host's own JSON result
// ("warnings" field) - the same data the drill-down's Warnings section and
// the tree's own ⚠ marker already read - so printing it a second time
// after the TUI closes is pure duplication, not new information. Anything
// else (real errors, tracebacks, config problems) passes through
// unchanged, since those typically have no other visibility path.
//
// Deliberately NOT applied to StartFirstGeneration's own pre-flight-
// failure dump: that path never builds a TUI at all (a bad playbook path,
// a parse error, a missing inventory - see its own doc comment), so
// stderr - [WARNING]: lines included - is the *only* signal the user ever
// gets; filtering there would risk silently swallowing the one clue
// available. Only applied where a drill-down (live, or via a saved run log
// through "revisit") genuinely already covers the same ground: main.go's
// and revisit.go's own post-app.Run() dumps.
func FilterRedundantWarnings(lines []string) []string {
	var kept []string
	for _, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "[WARNING]:") {
			continue
		}
		kept = append(kept, l)
	}
	return kept
}

// StreamItem is one unit of stdout output. isEvent is true whenever the
// line decoded successfully as JSON; a malformed line is still sent (with
// isEvent false) so main's pre-flight gate - which only cares whether
// anything ever arrived on stdout at all - sees it.
type StreamItem struct {
	Ev      pb.RawEvent
	IsEvent bool
}

// ScanEvents reads one JSON object per line from r, decoding each into a
// StreamItem sent on the returned channel; the channel is closed once r
// hits EOF or a scan error occurs. Runs on its own goroutine so a caller can
// observe "did anything ever arrive at all" (via the ok value of a channel
// receive) before deciding whether to show the TUI - see main's gate, added
// because ansible-playbook produces zero stdout output for pre-flight
// failures (bad playbook path, parse errors, missing inventory, ...),
// reporting those solely via stderr + a nonzero exit code.
//
// logFile, if non-nil (see runlog.go's CreateRunLog), gets every raw line
// teed into it verbatim, byte-identical to what ansible-playbook actually
// emitted - before trimming/decoding, so a malformed or blank line is saved
// too, same as a real one. This is design-docs/Revisit.md's own save
// mechanism: byte-identical means "revisit" can later replay a saved file
// through this exact same scan-and-decode logic, just pointed at a file
// instead of a live pipe, rather than needing a second, parallel
// serialization format to stay in sync with this one. Writes are
// best-effort (errors ignored) - same tolerance every other piece of this
// feature has for its own I/O failures, never worth disrupting the live
// event stream over. Closed once scanning ends, right alongside the
// channel.
//
// Reads via bufio.Reader.ReadBytes rather than bufio.Scanner deliberately -
// a real bug found by a code review, not a style choice: r here is always
// either the live event pipe (a real ansible-playbook run) or a saved
// run-log file (ReplayRunLog/revisit), and a Scanner enforces a hard
// per-token buffer cap. A single event's JSON is deliberately retained
// whole (host output, registered vars, ...) - a large stdout/facts result
// can legitimately exceed even a generously raised cap - so hitting it mid-
// run would make Scan() return false and this goroutine return early while
// ansible-playbook is still writing to the pipe's other end: the pipe then
// fills, the child blocks writing to it, and RunOneGeneration blocks
// forever waiting for a channel close that already happened and a process
// exit that can now never come. ReadBytes has no such cap (bounded only by
// memory, fine at this app's own ~10-host target scale), so an oversized
// event just costs one large allocation, never a hang.
//
// Takes ownership of r's lifetime the same way it already owns logFile's:
// if r implements io.Closer, it's closed once scanning ends. Another real
// bug the same review found: SpawnGeneration hands this the read end of
// its own dedicated event pipe (eventR) and, before this, never closed it
// on its own success path - only its two pre-Start failure returns did,
// since those never reach ScanEvents at all - leaking one fd per
// generation, and this app is explicitly built around repeated reruns
// within one process. r not implementing io.Closer (e.g. a bare
// strings.Reader, this file's own tests) is a harmless no-op. A caller
// that already closes its own r itself (ReplayRunLog below, plus diff.go's
// ReplayRun and revisit.go's resolveHadUnreachable, all opening a saved
// run's .jsonl file) just ends up closing it twice, harmlessly - the
// second Close's error return is already ignored everywhere it happens.
func ScanEvents(r io.Reader, logFile *os.File) <-chan StreamItem {
	ch := make(chan StreamItem, 64)
	go func() {
		defer close(ch)
		if logFile != nil {
			defer logFile.Close()
		}
		if closer, ok := r.(io.Closer); ok {
			defer closer.Close()
		}

		reader := bufio.NewReader(r)
		for {
			raw, readErr := reader.ReadBytes('\n')
			raw = bytes.TrimSuffix(raw, []byte("\n"))
			raw = bytes.TrimSuffix(raw, []byte("\r"))

			if len(raw) > 0 || readErr == nil {
				if logFile != nil {
					logFile.Write(raw)
					logFile.Write([]byte{'\n'})
				}

				line := strings.TrimSpace(string(raw))
				if line != "" {
					var ev pb.RawEvent
					if err := json.Unmarshal([]byte(line), &ev); err != nil {
						ch <- StreamItem{}
					} else {
						ch <- StreamItem{Ev: ev, IsEvent: true}
					}
				}
			}

			if readErr != nil {
				return
			}
		}
	}()
	return ch
}

// ReplayRunLog opens a saved generation's jsonl file (config.RunLogPaths)
// and replays its events into a fresh pb.PlaybookState via ScanEvents +
// state.Apply - the same reconstruction revisit.go's own OpenRevisitEntry
// needs to show a historical run's tree, factored out here so other
// callers (design-docs/Rerun.md's "Extend rerun dialog": the "rerun"
// verb's own Only-failed/Only-unreachable/Resume-where-failed defaults,
// computed from a fresh process with nothing in memory yet) can reuse it
// without duplicating the loop. Returns whatever os.Open itself returns on
// failure (missing file, permissions, ...) - callers decide how to treat
// "couldn't replay" (revisit.go's own PruneMissingRunLogs already keeps
// jsonlPath trustworthy before this is ever called, so a failure here is
// usually just a genuine race, not the common case).
func ReplayRunLog(jsonlPath string) (*pb.PlaybookState, error) {
	f, err := os.Open(jsonlPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	state := &pb.PlaybookState{}
	for item := range ScanEvents(f, nil) {
		if item.IsEvent {
			state.Apply(item.Ev)
		}
	}
	return state, nil
}
