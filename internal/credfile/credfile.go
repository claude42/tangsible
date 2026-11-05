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

// Package credfile turns ansible-playbook's own interactive password flags
// (--ask-become-pass, --ask-vault-pass, --ask-pass) into password files, so
// a process spawned after the TUI owns the terminal (debug's F5, a re-run)
// never prompts: ansible's getpass reads /dev/tty directly and would fight
// the raw screen. The prompt happens once, here, before any TUI exists.
package credfile

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"

	"golang.org/x/term"
)

// flagSpec maps one ask flag family to the file flag replacing it.
var flagSpecs = []struct {
	label    string
	askFlags []string
	fileFlag string
}{
	{"BECOME password", []string{"-K", "--ask-become-pass"}, "--become-password-file"},
	{"Vault password", []string{"-J", "--ask-vault-pass", "--ask-vault-password"}, "--vault-password-file"},
	{"SSH password", []string{"-k", "--ask-pass"}, "--connection-password-file"},
}

// originals remembers which ask flag each generated file replaced, so
// Restore can put the original spelling back (history must never record a
// temp path that is gone after exit).
var (
	originalsMu sync.Mutex
	originals   = map[string]string{} // password file path -> original ask flag
)

// secretParent prefers the per-user runtime directory (tmpfs, 0700, wiped at
// logout) over /tmp, so a file a hard kill leaves behind never reaches disk.
func secretParent() string {
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
		if fi, err := os.Stat(d); err == nil && fi.IsDir() {
			return d
		}
	}
	return ""
}

// Restore is Rewrite's inverse for display/history: every
// "<fileFlag> <generated path>" pair becomes the ask flag it replaced.
// Pairs it doesn't know about (a user's own --vault-password-file) stay.
func Restore(args []string) []string {
	originalsMu.Lock()
	defer originalsMu.Unlock()
	if len(originals) == 0 {
		return args
	}
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if i+1 < len(args) {
			if ask, ok := originals[args[i+1]]; ok && isFileFlag(args[i]) {
				out = append(out, ask)
				i++
				continue
			}
		}
		out = append(out, args[i])
	}
	return out
}

func isFileFlag(a string) bool {
	for _, s := range flagSpecs {
		if a == s.fileFlag {
			return true
		}
	}
	return false
}

// Prompter reads one secret, showing label.
type Prompter func(label string) (string, error)

// TerminalPrompter reads from the controlling terminal without echo.
func TerminalPrompter(label string) (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", errors.New("stdin is not a terminal, so " + label + " can't be asked for - use a password file instead")
	}
	fmt.Fprintf(os.Stderr, "%s: ", label)
	b, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("couldn't read %s: %w", label, err)
	}
	return string(b), nil
}

// Rewrite replaces every ask flag in args by its *-password-file
// equivalent, prompting once per flag family and writing each answer to a
// 0600 file in a private 0700 directory. Returns args unchanged (and a
// no-op cleanup) if there is nothing to ask. cleanup removes the files;
// callers must run it on every exit path. Only exact tokens are
// recognized, not bundled short forms like -vK (same gap as
// config.HasInteractiveCredentialFlag).
func Rewrite(args []string, prompt Prompter) (out []string, cleanup func(), err error) {
	cleanup = func() {}
	var dir string
	defer func() {
		if err != nil && dir != "" {
			os.RemoveAll(dir)
		}
	}()

	files := map[string]string{} // fileFlag -> path
	out = make([]string, 0, len(args))
	for _, a := range args {
		fileFlag := ""
		label := ""
		for _, s := range flagSpecs {
			for _, f := range s.askFlags {
				if a == f {
					fileFlag, label = s.fileFlag, s.label
				}
			}
		}
		if fileFlag == "" {
			out = append(out, a)
			continue
		}
		if _, done := files[fileFlag]; done {
			continue // flag repeated: one prompt, one file
		}
		if dir == "" {
			if dir, err = os.MkdirTemp(secretParent(), "tangsible-cred-"); err != nil {
				return nil, cleanup, err
			}
		}
		secret, perr := prompt(label)
		if perr != nil {
			return nil, cleanup, perr
		}
		path := filepath.Join(dir, fmt.Sprintf("%d", len(files)))
		if err = os.WriteFile(path, []byte(secret), 0o600); err != nil {
			return nil, cleanup, err
		}
		files[fileFlag] = path
		originalsMu.Lock()
		originals[path] = a
		originalsMu.Unlock()
		out = append(out, fileFlag, path)
	}
	if dir == "" {
		return args, cleanup, nil
	}
	// The TUI's raw mode only disables SIGINT: a plain kill or a closing
	// terminal would otherwise leave the secrets behind.
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		sig := <-sigc
		os.RemoveAll(dir)
		code := 143
		if sig == syscall.SIGHUP {
			code = 129
		}
		os.Exit(code)
	}()
	return out, func() { signal.Stop(sigc); os.RemoveAll(dir) }, nil
}
