// Shells out to ansible-playbook using the ansible.posix.jsonl stdout
// callback and streams events live into an interactive TUI as they arrive.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
)

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
	app, applyLive := NewLiveTUI(state, filepath.Base(os.Args[1]), cmd.Process, &processDone, &quitting)

	stderrLines := make(chan []string, 1)
	go func() { stderrLines <- streamStderr(stderr) }()

	type runResult struct {
		waitErr     error
		childStderr []string
		diagnostics []string
	}
	resultCh := make(chan runResult, 1)
	go func() {
		diagnostics := streamEvents(stdout, applyLive, &quitting)
		childStderr := <-stderrLines // wait for stderr to fully drain before Wait()
		waitErr := cmd.Wait()
		resultCh <- runResult{waitErr: waitErr, childStderr: childStderr, diagnostics: diagnostics}
		processDone.Store(true)
	}()

	runErr := app.Run()
	quitting.Store(true) // defensive: also stop the streamer if Run() ever
	// returns for a reason other than our own Stop()

	result := <-resultCh
	for _, l := range result.childStderr {
		fmt.Fprintln(os.Stderr, "[ansible-playbook stderr]", l)
	}
	for _, l := range result.diagnostics {
		fmt.Println(l)
	}

	if runErr != nil {
		fmt.Fprintln(os.Stderr, "TUI error:", runErr)
		os.Exit(1)
	}
	if result.waitErr != nil {
		fmt.Fprintln(os.Stderr, "ansible-playbook exited with error:", result.waitErr)
		os.Exit(1)
	}
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
