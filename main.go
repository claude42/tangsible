// Prototype: shells out to ansible-playbook using the ansible.posix.jsonl
// stdout callback and prints each event as it arrives, to validate that we
// really do get live, line-delimited JSON rather than one buffered blob at
// the end of the run.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
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

	stderrDone := make(chan struct{})
	go streamStderr(stderr, stderrDone)
	streamEvents(stdout)
	<-stderrDone

	if err := cmd.Wait(); err != nil {
		fmt.Fprintln(os.Stderr, "ansible-playbook exited with error:", err)
		os.Exit(1)
	}
}

func streamStderr(r io.Reader, done chan struct{}) {
	defer close(done)
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		fmt.Fprintln(os.Stderr, "[ansible-playbook stderr]", scanner.Text())
	}
}

// streamEvents reads one JSON object per line from r, feeds it into the
// play/task/host aggregator, and reprints the whole current tree after each
// event — so the terminal scrollback shows the model's progression live,
// while the playbook is still running, rather than all at once at the end.
func streamEvents(r io.Reader) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	state := &playbookState{}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var ev rawEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			fmt.Println("(not JSON)", line)
			continue
		}

		state.Apply(ev)

		fmt.Println("----")
		Render(os.Stdout, state)

		if ev.Event == "v2_playbook_on_stats" {
			var stats struct {
				Stats map[string]interface{} `json:"stats"`
			}
			json.Unmarshal([]byte(line), &stats)
			fmt.Println("ansible's own final stats (for cross-checking):")
			pretty, _ := json.MarshalIndent(stats.Stats, "  ", "  ")
			fmt.Printf("  %s\n", pretty)
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "error reading ansible-playbook output:", err)
	}
}
