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
	// The playbook is normally os.Args[1], but doesn't have to be -
	// splitPlaybookArgs treats a missing or flag-shaped first argument as
	// "none given positionally" and resolvePlaybook takes over (see
	// resolve.go for the full TANGSIBLE_PLAYBOOK/.tangsible/
	// $XDG_CONFIG_HOME/site.yml cascade).
	playbook, rest, explicit := splitPlaybookArgs(os.Args[1:])
	if !explicit {
		var source string
		playbook, source = resolvePlaybook()
		if playbook == "" {
			fmt.Fprintf(os.Stderr, "usage: %s [<playbook.yml>] [ansible-playbook args...]\n", os.Args[0])
			fmt.Fprintln(os.Stderr, "no playbook given, and none could be determined from TANGSIBLE_PLAYBOOK, .tangsible, $XDG_CONFIG_HOME/tangsible/config.toml, or ./site.yml")
			os.Exit(2)
		}
		fmt.Fprintf(os.Stderr, "tangsible: no playbook given - using %q (%s)\n", playbook, source)
	}

	cmd := exec.Command("ansible-playbook", append([]string{playbook}, rest...)...)
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

	stdoutCh := scanEvents(stdout)

	stderrLines := make(chan []string, 1)
	go func() { stderrLines <- streamStderr(stderr) }()

	// The gate: ansible-playbook writes zero bytes to stdout for a
	// pre-flight failure (bad playbook path, a parse error, a missing
	// inventory, ...) - those are reported entirely via stderr + a nonzero
	// exit code, before any real event ever fires. v2_playbook_on_play_start
	// fires unconditionally as the very first thing any real run does (even
	// for a play with zero tasks - confirmed against ansible-core's
	// TaskQueueManager.run()), so "did at least one line ever arrive on
	// stdout" is a reliable, general signal that ansible-playbook is
	// genuinely running - not a heuristic specific to this one failure mode.
	// Peeking it here, before ever constructing the TUI, is what lets this
	// branch skip showing it entirely rather than showing it empty and
	// waiting for the user to quit before the error becomes visible.
	first, ok := <-stdoutCh
	if !ok {
		// Nothing ever arrived: safe to call cmd.Wait() here because
		// scanEvents's goroutine only closes its channel after its scan
		// loop has already hit real EOF on stdout - exec.Cmd's "read the
		// pipes fully before Wait()" contract is satisfied by construction.
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

	// Built synchronously, after the pre-flight gate above, so a bad
	// playbook path/parse error doesn't pay for it - parsing a project's
	// own YAML files is expected to be well under the noise floor of an
	// interactive ansible run at this project's stated ~10-host target
	// scale, so this isn't worth backgrounding.
	sourceIndex := buildTaskSourceIndex(playbook)

	state := &playbookState{}
	var processDone, quitting atomic.Bool
	var exitCode atomic.Int32
	app, applyLive := NewLiveTUI(state, filepath.Base(playbook), cmd.Process, &processDone, &quitting, &exitCode, sourceIndex)

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

	type runResult struct {
		waitErr     error
		exitCode    int
		childStderr []string
		diagnostics []string
	}
	resultCh := make(chan runResult, 1)
	go func() {
		diagnostics := apply(first)
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
