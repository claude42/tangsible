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

// Package debugrun drives one ansible-playbook invocation running against
// a debug-mode shadow copy (internal/debugcopy) with debugger: always
// spliced onto every task - design-docs/DebugMode.md's M1 build step.
// Headless, no verb/TUI wiring yet: Session is a primitive a future
// breakpoint UI calls into, mirroring how internal/debugcopy (M0) is a
// primitive this package itself builds on.
package debugrun

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"code.aw.net/claude/tangsible/internal/runner"
)

// Pause is one point where the ansible-playbook child is blocked inside
// ansible-core's own built-in task debugger, waiting for a command.
// Design-docs/DebugMode.md's uniform debugger:-always splice means every
// task produces one of these, not just a chosen breakpoint - deciding
// whether to actually stop and show this to the user, or silently
// Continue past it, is a caller-side policy this package doesn't make.
type Pause struct {
	Host string
	Task string // the debugger's own task label verbatim, e.g. "TASK: some task name"
}

// promptRe matches ansible-core's own debugger prompt
// (ansible/plugins/strategy/__init__.py's Debugger class: "[%s] %s (debug)> "),
// confirmed live across every spike in design-docs/DebugMode.md.
var promptRe = regexp.MustCompile(`\[(\S+)\] (TASK: .+?) \(debug\)> $`)

// Session drives one ansible-playbook invocation against copyDir/playbook
// (a shadow copy already spliced via internal/debugcopy.SpliceDebuggerAlways).
// Two independent channels, matching the dual-channel architecture
// confirmed live (design-docs/DebugMode.md, "Full interactive-debugger
// vision and dual-channel spike (2026-10-14)"):
//
//   - Events is the ordinary jsonl stream (runner.ScanEvents, completely
//     unchanged) - an async push the child produces regardless of what the
//     debugger is doing.
//   - The debugger REPL itself (Continue/Query below) is synchronous
//     request-response over a second, dedicated pipe pair - never a pty:
//     ansible-core's own debugger reads via Python's plain input(), which
//     needs no tty, and a pty's local echo would just be noise to filter
//     back out of every response.
//
// stderr is folded into the same pipe as the REPL's own stdout for now -
// debug mode has no separate stderr surface yet, in keeping with this
// being a primitive, not the finished feature.
type Session struct {
	cmd    *exec.Cmd
	Events <-chan runner.StreamItem

	replW     io.WriteCloser // our write end -> child's stdin
	replR     *bufio.Reader  // child's stdout+stderr (REPL prompts + the quiet stdout callback's own noise)
	replRFile *os.File
}

// Spawn starts ansible-playbook against the already-spliced copy at
// playbook (an absolute path, e.g. copyDir/relPlaybook - see debugverb.go's
// own copyPlaybookPath), with the bundled callback plugin wired for jsonl
// events (runner.CallbackPluginEnv, the same mechanism runner.
// SpawnGeneration already uses) and a quiet stdout callback forced so the
// REPL's prompt text is never ambiguous with another callback's own
// output - design-docs/DebugMode.md's stdout-ownership wrinkle. Colors are
// forced off (not forceColorEnv's usual force-on): the prompt is matched
// by a plain-text regex, and ANSI escapes in the middle of it would break
// that.
//
// Deliberately leaves cmd.Dir unset, inheriting the caller's own working
// directory - the same as runner.SpawnGeneration's own plain run. An
// earlier version set cmd.Dir to the shadow copy, which broke any relative
// passthrough path (-i some/inventory, -e @some/vars.yml, ...): it then
// resolved against the copy's own directory instead of where the user
// actually typed it, silently matching no hosts at all instead of erroring
// (design-docs/DebugMode.md's "relative inventory path vs. shadow-copy
// cwd" finding). Passing playbook as an absolute path makes that
// unnecessary: ansible-core itself always resolves role/include paths
// relative to the playbook's own directory, never cwd.
func Spawn(playbook string, args []string) (*Session, error) {
	pluginEnv, err := runner.CallbackPluginEnv()
	if err != nil {
		return nil, err
	}

	cmdArgs := append([]string{playbook}, args...)
	cmd := exec.Command("ansible-playbook", cmdArgs...)

	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("debugrun: creating stdin pipe: %w", err)
	}
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		stdinR.Close()
		stdinW.Close()
		return nil, fmt.Errorf("debugrun: creating stdout pipe: %w", err)
	}
	eventR, eventW, err := os.Pipe()
	if err != nil {
		stdinR.Close()
		stdinW.Close()
		stdoutR.Close()
		stdoutW.Close()
		return nil, fmt.Errorf("debugrun: creating event pipe: %w", err)
	}

	cmd.Stdin = stdinR
	cmd.Stdout = stdoutW
	cmd.Stderr = stdoutW
	cmd.ExtraFiles = []*os.File{eventW} // fd 3 in the child, same slot SpawnGeneration uses

	cmd.Env = append(append(os.Environ(), pluginEnv...),
		"ANSIBLE_NOCOLOR=1",
		"ANSIBLE_STDOUT_CALLBACK=minimal",
		"TANGSIBLE_EVENT_FD=3",
	)

	if err := cmd.Start(); err != nil {
		stdinR.Close()
		stdinW.Close()
		stdoutR.Close()
		stdoutW.Close()
		eventR.Close()
		eventW.Close()
		return nil, fmt.Errorf("debugrun: starting ansible-playbook: %w", err)
	}

	// Our own copies of the child's ends must close now, not just the
	// child's - a reader only ever sees EOF once every copy of the write
	// end is closed (same reasoning SpawnGeneration's own eventW.Close()
	// already documents).
	stdinR.Close()
	stdoutW.Close()
	eventW.Close()

	return &Session{
		cmd:       cmd,
		Events:    runner.ScanEvents(eventR, nil),
		replW:     stdinW,
		replR:     bufio.NewReader(stdoutR),
		replRFile: stdoutR,
	}, nil
}

// Start blocks until the first debugger pause, or returns eof=true if the
// child exits without ever reaching one (an empty playbook, or a
// pre-flight failure).
func (s *Session) Start() (pause Pause, eof bool, err error) {
	_, pause, eof, err = s.readUntilPrompt()
	return pause, eof, err
}

// Continue sends the debugger's own "continue" command and blocks until
// the next pause, or the child exits (eof=true - the normal way a debug
// run ends, once its very last task's pause is resumed).
func (s *Session) Continue() (pause Pause, eof bool, err error) {
	if _, werr := io.WriteString(s.replW, "c\n"); werr != nil {
		return Pause{}, false, werr
	}
	_, pause, eof, err = s.readUntilPrompt()
	return pause, eof, err
}

// Query evaluates expr against the currently paused task's own debugger
// scope (task/host/task_vars/play_context/result - ansible-core's own
// debugger globals) and returns its JSON-encoded result, without
// advancing past the current pause. expr should itself produce
// JSON-serializable data; this wraps it exactly the way design-docs/
// DebugMode.md's JSON-dump trick does, confirmed live: __import__('json')
// is required since json isn't pre-imported into the debugger's own eval
// globals, and default=str sanitizes non-JSON-serializable objects
// (warnings, etc.) that can appear nested in a real result.
func (s *Session) Query(expr string) (json.RawMessage, error) {
	cmd := fmt.Sprintf("p print(__import__('json').dumps(%s, default=str)) or 0\n", expr)
	if _, err := io.WriteString(s.replW, cmd); err != nil {
		return nil, err
	}
	text, _, eof, err := s.readUntilPrompt()
	if err != nil {
		return nil, err
	}
	if eof {
		return nil, fmt.Errorf("debugrun: process exited while awaiting a query response")
	}
	return extractJSONLine(text)
}

// Signal forwards sig to the child process - used so a caller's own
// Ctrl-C/q handling can abort a live debug session the same way
// CLAUDE.md's own Ctrl-C/q invariant already works for a normal run
// (internal/session's procH.Signal). A no-op if the child was never
// successfully started.
func (s *Session) Signal(sig os.Signal) error {
	if s.cmd.Process == nil {
		return nil
	}
	return s.cmd.Process.Signal(sig)
}

// Close ends the session: closing our end of the REPL pipe sends EOF on
// the child's stdin, which the debugger's own do_EOF handler treats as
// "quit" if the child is still paused; then waits for the process to
// exit. Safe to call after the session has already ended on its own
// (Continue returning eof=true) - Wait on an already-exited process
// just returns its already-recorded result.
func (s *Session) Close() error {
	s.replW.Close()
	s.replRFile.Close()
	return s.cmd.Wait()
}

// readUntilPrompt reads raw bytes until promptRe matches the accumulated
// text (the debugger's next prompt has fully arrived) or the pipe hits
// EOF (the child exited - eof=true, err nil, since that's the normal way
// a run ends, not a failure this function reports as one).
func (s *Session) readUntilPrompt() (text string, pause Pause, eof bool, err error) {
	var buf bytes.Buffer
	for {
		b, rerr := s.replR.ReadByte()
		if rerr != nil {
			return buf.String(), Pause{}, true, nil
		}
		buf.WriteByte(b)
		if m := promptRe.FindStringSubmatch(buf.String()); m != nil {
			return buf.String(), Pause{Host: m[1], Task: m[2]}, false, nil
		}
	}
}

// extractJSONLine returns the first line of text that parses as valid
// JSON - Query's own response is exactly one such line (the query's
// print()'d JSON), followed by a throwaway pformat'd sentinel line and
// eventually the next prompt, neither of which is valid JSON itself.
func extractJSONLine(text string) (json.RawMessage, error) {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		var v any
		if json.Unmarshal([]byte(line), &v) == nil {
			return json.RawMessage(line), nil
		}
	}
	return nil, fmt.Errorf("debugrun: no JSON line found in query response: %q", text)
}
