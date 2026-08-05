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

// streamEvents reads one JSON object per line from r and prints it as soon
// as it arrives, so we can see events show up while the playbook is still
// running rather than all at once at the end.
func streamEvents(r io.Reader) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	n := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		n++

		var event map[string]interface{}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			fmt.Printf("[%d] (not JSON) %s\n", n, line)
			continue
		}

		keys := make([]string, 0, len(event))
		for k := range event {
			keys = append(keys, k)
		}

		pretty, _ := json.MarshalIndent(event, "  ", "  ")
		fmt.Printf("[%d] keys=%v\n  %s\n", n, keys, pretty)
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "error reading ansible-playbook output:", err)
	}
}
