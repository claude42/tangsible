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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"

	"code.aw.net/claude/tangsible/internal/config"
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
// wiring up its stdout/stderr exactly as every generation needs (see
// ScanEvents/StreamStderr) and pointing procH at the new child so Ctrl-C/q
// forwarding targets it. Shared by the first invocation and every rerun
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
func SpawnGeneration(playbook string, args []string, procH *ProcHandle) (cmd *exec.Cmd, stdoutCh <-chan StreamItem, stderrLines <-chan []string, runID string, err error) {
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
	cmd.Env = append(os.Environ(),
		"ANSIBLE_STDOUT_CALLBACK=ansible.posix.jsonl",
		// Pin compact (single-line) JSON so our line-based scanner can't be
		// broken by a user's ansible.cfg overriding this to pretty-print.
		"ANSIBLE_JSON_INDENT=0",
	)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, nil, "", fmt.Errorf("failed to attach stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, nil, "", fmt.Errorf("failed to attach stderr: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, nil, "", fmt.Errorf("failed to start ansible-playbook: %w", err)
	}
	procH.Store(cmd.Process)

	runID = config.NewRunID(time.Now())
	logFile := config.CreateRunLog(config.TangsibleStatePath, runID)
	if logFile == nil {
		runID = ""
	}

	stdoutCh = ScanEvents(stdout, logFile)
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

	cmd, stdoutCh, stderrLines, runID, err := SpawnGeneration(playbook, rest, procH)
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
		if waitErr != nil {
			fmt.Fprintln(os.Stderr, "ansible-playbook exited with error:", waitErr)
			os.Exit(1)
		}
		return nil, false
	}
	return &PendingGeneration{Cmd: cmd, StdoutCh: stdoutCh, StderrLines: stderrLines, First: first, RunID: runID}, true
}

// StreamStderr collects the child's stderr lines instead of printing them
// live — printing directly to the terminal while the TUI's alternate
// screen is active would corrupt the display. main prints them after
// app.Run() returns and the real terminal is restored.
func StreamStderr(r io.Reader) []string {
	var lines []string
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines
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
func ScanEvents(r io.Reader, logFile *os.File) <-chan StreamItem {
	ch := make(chan StreamItem, 64)
	go func() {
		defer close(ch)
		if logFile != nil {
			defer logFile.Close()
		}

		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		for scanner.Scan() {
			raw := scanner.Bytes()
			if logFile != nil {
				logFile.Write(raw)
				logFile.Write([]byte{'\n'})
			}

			line := strings.TrimSpace(string(raw))
			if line == "" {
				continue
			}

			var ev pb.RawEvent
			if err := json.Unmarshal([]byte(line), &ev); err != nil {
				ch <- StreamItem{}
				continue
			}
			ch <- StreamItem{Ev: ev, IsEvent: true}
		}
	}()
	return ch
}
