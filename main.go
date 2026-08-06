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

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: %s <playbook.yml> [ansible-playbook args...]\n", os.Args[0])
		os.Exit(2)
	}

	cmd := exec.Command("ansible-playbook", os.Args[1:]...)
	cmd.Env = append(os.Environ(),
		"ANSIBLE_STDOUT_CALLBACK=ansible.posix.jsonl",
		// Pin compact (single-line) JSON so our line-based scanner can't be
		// broken by a user's ansible.cfg overriding this to pretty-print.
		"ANSIBLE_JSON_INDENT=0",
	)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to attach stdout:", err)
		os.Exit(1)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to attach stderr:", err)
		os.Exit(1)
	}
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "failed to start ansible-playbook:", err)
		os.Exit(1)
	}

	state := &playbookState{}
	var processDone, quitting atomic.Bool
	var exitCode atomic.Int32
	app, applyLive := NewLiveTUI(state, filepath.Base(os.Args[1]), cmd.Process, &processDone, &quitting, &exitCode)

	stderrLines := make(chan []string, 1)
	go func() { stderrLines <- streamStderr(stderr) }()

	type runResult struct {
		waitErr     error
		exitCode    int
		childStderr []string
		diagnostics []string
	}
	resultCh := make(chan runResult, 1)
	go func() {
		diagnostics := streamEvents(stdout, applyLive, &quitting)
		childStderr := <-stderrLines // wait for stderr to fully drain before Wait()
		waitErr := cmd.Wait()
		code := exitCodeOf(waitErr)
		exitCode.Store(int32(code)) // before processDone below - tui.go's
		// rebuild() only ever reads exitCode once it observes processDone
		// true, and Go's atomics are sequentially consistent as a whole
		// program (not just per-variable), so this ordering is what makes
		// that store visible there.
		resultCh <- runResult{waitErr: waitErr, exitCode: code, childStderr: childStderr, diagnostics: diagnostics}
		processDone.Store(true)
	}()

	runErr := app.Run()
	quitting.Store(true) // defensive: also stop the streamer if Run() ever
	// returns for a reason other than our own Stop()

	result := <-resultCh

	// benignHostUnreachable: ansible-playbook's exit code 4 is itself
	// ambiguous - ansible-core's own ExitCode enum assigns 4 to both
	// HOST_UNREACHABLE and PARSER_ERROR (a static-include syntax error),
	// with its own "FIXME: conflicts" comment acknowledging this. We only
	// treat 4 as benign when Tangsible independently observed concrete
	// evidence explaining it that way: a real v2_runner_on_unreachable
	// event during this run (state.HadUnreachable, aggregate.go). A static
	// import/role's syntax error is resolved entirely at parse time, before
	// any task in any play begins - so HadUnreachable can only become true
	// after the playbook has already finished parsing without error,
	// structurally ruling out that fatal cause whenever it's set.
	//
	// Read here with no synchronization, deliberately: every write to
	// state.HadUnreachable happens inside a state.Apply call, and every
	// state.Apply call - regardless of which goroutine enqueued it via
	// QueueUpdate/QueueUpdateDraw - executes on whichever goroutine is
	// running app.Run()'s event loop. Since app.Run() above is called
	// directly by this goroutine (not spawned with `go`), that goroutine is
	// this one - so every state.Apply call already happened-before this
	// line in straightforward program order, the same as any other
	// sequential self-read. Unlike processDone/quitting/exitCode below
	// (genuinely written by the separate orchestrator goroutine), no atomic
	// is needed.
	//
	// Deliberately gated on exitCode == 4 specifically, not a general
	// "anything ever went unreachable" override: e.g. exit 6 (a real host
	// failure alongside an unreachable one) still reports as today.
	benignHostUnreachable := result.exitCode == 4 && state.HadUnreachable

	// A 99 exit means the user asked us (via q/Ctrl-C) to interrupt the run
	// - not a failure. Suppress the two lines that would otherwise read
	// like an error report for something the user deliberately did.
	if result.exitCode != ansibleUserInterruptedExitCode {
		for _, l := range result.childStderr {
			fmt.Fprintln(os.Stderr, "[ansible-playbook stderr]", l)
		}
	}
	for _, l := range result.diagnostics {
		fmt.Println(l)
	}

	if runErr != nil {
		fmt.Fprintln(os.Stderr, "TUI error:", runErr)
		os.Exit(1)
	}
	if result.waitErr != nil && result.exitCode != ansibleUserInterruptedExitCode && !benignHostUnreachable {
		fmt.Fprintln(os.Stderr, "ansible-playbook exited with error:", result.waitErr)
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

// streamEvents reads one JSON object per line from r and feeds each into
// applyLive, which queues it onto the TUI's event loop. Diagnostic output
// (parse failures, the final stats cross-check) is collected rather than
// printed live, for the same terminal-corruption reason as streamStderr.
// Once quitting is set, r keeps being drained (so ansible-playbook's
// stdout pipe never backs up) but applyLive is no longer called.
func streamEvents(r io.Reader, applyLive func(rawEvent), quitting *atomic.Bool) []string {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var diagnostics []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var ev rawEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			diagnostics = append(diagnostics, "(not JSON) "+line)
			continue
		}

		if ev.Event == "v2_playbook_on_stats" {
			var stats struct {
				Stats map[string]interface{} `json:"stats"`
			}
			json.Unmarshal([]byte(line), &stats)
			pretty, _ := json.MarshalIndent(stats.Stats, "  ", "  ")
			diagnostics = append(diagnostics, fmt.Sprintf("ansible's own final stats (for cross-checking):\n  %s", pretty))
		}

		if quitting.Load() {
			continue
		}
		applyLive(ev)
	}
	if err := scanner.Err(); err != nil {
		diagnostics = append(diagnostics, fmt.Sprintf("error reading ansible-playbook output: %v", err))
	}
	return diagnostics
}
