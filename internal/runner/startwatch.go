package runner

import (
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// A container can take long to start - the first start of a new image
// re-owns its layers, silently, for tens of seconds - and until its first
// event arrives nothing is on screen (before the TUI) or in the tree (a
// re-run). watchStart breaks that silence: after startNotice a line saying
// what we are waiting for, and every startNoticeEvery another, each with what
// the engine has printed so far. Variables only so tests can shorten them.
var (
	startNotice      = 2 * time.Second
	startNoticeEvery = 15 * time.Second
)

// watchStart reports on w until done is closed. cmdline, if not empty, is the
// engine command, for reference (printed once, with the first notice - it
// names tangsible's own temporary event directory and container, so it is not
// something to paste while tangsible runs); tryLine, if not empty, is a bare
// command that tests the image on its own; log supplies the engine's messages:
// the lines after index from.
func watchStart(w io.Writer, cmdline, tryLine string, log *OutputLog, from int, done <-chan struct{}) {
	started := time.Now()
	wait := startNotice
	first := true
	seen := from
	for {
		select {
		case <-done:
			return
		case <-time.After(wait):
		}
		elapsed := time.Since(started).Round(time.Second)
		if first {
			fmt.Fprintf(w, "tangsible: waiting for ansible-playbook to start in the container (%s) - the first start of a newly pulled or built image can take a while\n", elapsed)
			if cmdline != "" {
				fmt.Fprintf(w, "tangsible: command: %s\n", cmdline)
			}
			if tryLine != "" {
				fmt.Fprintf(w, "tangsible: to try the image on its own (timing the first start): %s\n", tryLine)
			}
		} else {
			fmt.Fprintf(w, "tangsible: still waiting for ansible-playbook in the container (%s)\n", elapsed)
		}
		first = false
		if log != nil {
			lines := log.LinesFrom(seen)
			seen += len(lines)
			if len(lines) > 3 {
				lines = lines[len(lines)-3:]
			}
			for _, l := range lines {
				fmt.Fprintf(w, "tangsible: the engine said: %s\n", strings.TrimRight(l, "\r\n"))
			}
		}
		wait = startNoticeEvery
	}
}

var safeShellWord = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

// quoteArg quotes one argument for a POSIX shell, only when it needs it.
func quoteArg(a string) string {
	if a != "" && safeShellWord.MatchString(a) {
		return a
	}
	return "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
}

// CommandLine renders cmd as a line that can be pasted into a shell. Values of
// the environment are not part of it (and never are: secrets travel in the
// engine process's environment, not on the command line).
func CommandLine(cmd *exec.Cmd) string {
	parts := make([]string, len(cmd.Args))
	for i, a := range cmd.Args {
		parts[i] = quoteArg(a)
	}
	return strings.Join(parts, " ")
}

// tryLine is a command that starts the image the way tangsible does (user
// namespace mapping is what makes a new image's first start slow) with
// nothing else, to tell a slow image from a problem in tangsible's own setup.
func tryLine(engine, image string) string {
	return quoteArg(engine) + " run --rm --userns=keep-id " + quoteArg(image) + " true"
}
