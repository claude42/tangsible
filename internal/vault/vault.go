// Implements the "vault" Verb (design-docs/Vault.md): edit individually
// vault-encrypted Ansible variables in a file as if editing plain text -
// decrypt every top-level !vault value, open $EDITOR on the plaintext
// view, and on close, re-encrypt only what actually changed. Unlike every
// other Verb in this codebase, this one never touches tview - it's a
// plain, sequential CLI flow with no TUI at all.
package vault

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"golang.org/x/term"

	"code.aw.net/claude/tangsible/internal/vaultfile"
)

// ParseVaultArgs splits args (everything after the "vault" Verb) into the
// required target filename plus the password-source flags design-docs/
// Vault.md's password-source decision specifies: --vault-password-file
// <path> (or --vault-password-file=<path>) and --ask-vault-pass (a bare
// boolean, no value) - mirroring real ansible-vault's own flags rather
// than inventing new ones, and rejected together the same way
// ansible-vault itself treats them as mutually exclusive. Hand-rolled
// rather than the stdlib flag package (used nowhere else in this
// codebase), matching internal/config/rerunargs.go's ParsePassthroughArgs
// style.
func ParseVaultArgs(args []string) (filename, passwordFile string, askPass bool, err error) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return "", "", false, errors.New("a target file is required")
	}
	filename = args[0]

	take := func(flag string, i int) (value string, consumed int, ok bool) {
		a := args[i]
		if a == flag {
			if i+1 < len(args) {
				return args[i+1], 2, true
			}
			return "", 0, false // dangling flag with no value
		}
		if v, found := strings.CutPrefix(a, flag+"="); found {
			return v, 1, true
		}
		return "", 0, false
	}

	for i := 1; i < len(args); {
		if v, n, ok := take("--vault-password-file", i); ok {
			passwordFile = v
			i += n
			continue
		}
		if args[i] == "--ask-vault-pass" {
			askPass = true
			i++
			continue
		}
		i++
	}

	if passwordFile != "" && askPass {
		return "", "", false, errors.New("--vault-password-file and --ask-vault-pass can't be used together")
	}
	return filename, passwordFile, askPass, nil
}

// passwordSourceKind identifies which of design-docs/Vault.md's four
// password sources won, per chosePasswordSource's precedence.
type passwordSourceKind int

const (
	passwordSourcePromptKind passwordSourceKind = iota
	passwordSourceFileKind                      // file is a path to read (flag, env var, or ansible.cfg)
)

// choosePasswordSource is the pure precedence decision behind
// resolvePassword: an explicit CLI flag wins over
// $ANSIBLE_VAULT_PASSWORD_FILE wins over the project's own ansible.cfg
// ([defaults] vault_password_file - the standard, most common place
// ansible users already configure this) wins over an interactive no-echo
// prompt as a last resort - a deliberate divergence from real
// ansible-vault, which errors instead of prompting when nothing is
// configured; accepted here because this verb is inherently interactive,
// with no batch/CI use case for editing one variable. Takes plain
// strings/bools, not the environment/filesystem directly, so the
// precedence logic itself is testable without a real filesystem or
// terminal.
func choosePasswordSource(flagFile string, askFlag bool, envFile string, ansibleCfgFile string) (kind passwordSourceKind, file string) {
	switch {
	case flagFile != "":
		return passwordSourceFileKind, flagFile
	case askFlag:
		return passwordSourcePromptKind, ""
	case envFile != "":
		return passwordSourceFileKind, envFile
	case ansibleCfgFile != "":
		return passwordSourceFileKind, ansibleCfgFile
	}
	return passwordSourcePromptKind, ""
}

func resolvePassword(flagFile string, askFlag bool) (string, error) {
	envFile := os.Getenv("ANSIBLE_VAULT_PASSWORD_FILE")
	ansibleCfgFile := ansibleCfgVaultPasswordFile()

	kind, file := choosePasswordSource(flagFile, askFlag, envFile, ansibleCfgFile)
	if kind == passwordSourceFileKind {
		return readPasswordFile(file)
	}
	return promptPassword()
}

func readPasswordFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("couldn't read vault password file %q: %w", path, err)
	}
	// Matches ansible-vault's own tolerance of a trailing newline in a
	// password file - a password file is conventionally one line, and a
	// trailing newline from `echo password > file` shouldn't become part
	// of the actual password.
	return strings.TrimRight(string(data), "\r\n"), nil
}

func promptPassword() (string, error) {
	fmt.Fprint(os.Stderr, "Vault password: ")
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("couldn't read password: %w", err)
	}
	return string(b), nil
}

// RunVaultVerb is main.go's entry point for the "vault" Verb. Returns the
// process exit code rather than calling os.Exit itself, so its own
// deferred cleanup (the editor loop's scratch-file removal, the
// signal.NotifyContext's own stop func) reliably runs on every path -
// same convention as RunTemplateVerb/RunHostVerb.
func RunVaultVerb(args []string) int {
	filename, passwordFile, askPass, err := ParseVaultArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "usage: %s vault <filename> [--vault-password-file <path> | --ask-vault-pass]\n", os.Args[0])
		fmt.Fprintln(os.Stderr, "tangsible:", err)
		return 2
	}

	// A secondary safety net, not the primary way to escape a detected
	// problem - see askFixOrRevert (editor.go) for that, and its own doc
	// comment for why Ctrl-C can't be relied on at all while a real
	// full-screen editor (vim, and similar) is open: such editors disable
	// the terminal's own SIGINT-on-Ctrl-C generation for the whole shared
	// terminal while running, not just for themselves. This still matters
	// for the moments tangsible itself is blocking (the password prompt,
	// askFixOrRevert, file reads): a genuine, unhandled terminating signal
	// there would bypass Go's own deferred cleanup and leave the decrypted
	// scratch dir behind. SIGTERM (plain `kill`, a session manager on
	// logout, system shutdown) and SIGHUP (the controlling terminal going
	// away) are trapped alongside SIGINT for exactly that reason; SIGKILL
	// is uncatchable and accepted as out of scope.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()

	password, err := resolvePassword(passwordFile, askPass)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tangsible:", err)
		return 1
	}
	if ctx.Err() != nil {
		return abortExitCode
	}

	source, err := os.ReadFile(filename)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tangsible: couldn't read %s: %v\n", filename, err)
		return 1
	}

	view, err := vaultfile.BuildDecryptedView(source, password)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tangsible:", err)
		return 1
	}

	return runEditorLoop(filename, view, password, realEditor, askFixOrRevert, ctx)
}
