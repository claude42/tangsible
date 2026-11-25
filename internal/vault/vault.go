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
	"sort"
	"syscall"

	"code.aw.net/claude/tangsible/internal/config"
	"code.aw.net/claude/tangsible/internal/vaultfile"
)

// RunVaultVerb is main.go's entry point for the "vault" Verb. Returns the
// process exit code rather than calling os.Exit itself, so its own
// deferred cleanup (the editor loop's scratch-file removal, the
// signal.NotifyContext's own stop func) reliably runs on every path -
// same convention as RunTemplateVerb/RunHostVerb.
func RunVaultVerb(args []string) int {
	parsed, err := ParseVaultArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, config.UsageLine(config.VerbVault))
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

	sources := readPasswordSources()
	if parsed.EncryptID == "" {
		parsed.EncryptID = sources.EncryptID
	}
	secrets, err := loadSecrets(secretSpecs(parsed, sources), promptPassword)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tangsible:", err)
		return 1
	}
	if ctx.Err() != nil {
		return abortExitCode
	}

	// A file that does not exist yet is created empty; an empty file is
	// simply a file with no variables, and whatever is added is encrypted.
	if _, statErr := os.Stat(parsed.Filename); errors.Is(statErr, os.ErrNotExist) {
		f, err := os.OpenFile(parsed.Filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
		if err != nil {
			fmt.Fprintf(os.Stderr, "tangsible: couldn't create %s: %v\n", parsed.Filename, err)
			return 1
		}
		f.Close()
		fmt.Fprintf(os.Stderr, "tangsible: %s did not exist, created it\n", parsed.Filename)
	}
	source, err := os.ReadFile(parsed.Filename)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tangsible: couldn't read %s: %v\n", parsed.Filename, err)
		return 1
	}

	view, err := vaultfile.BuildDecryptedView(source, secrets)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tangsible:", err)
		return 1
	}
	reportLocked(view)

	newSecret, err := chooseEncryptSecret(view, secrets, parsed.EncryptID)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tangsible:", err)
		return 2
	}

	return runEditorLoop(parsed.Filename, view, newSecret, realEditor, askFixOrRevert, ctx)
}

// reportLocked tells the user which values the passwords given could not
// open: they show as placeholders in the editor and are saved unchanged.
func reportLocked(view vaultfile.DecryptedView) {
	locked := view.LockedLabels()
	if len(locked) == 0 {
		return
	}
	if len(view.VaultedBefore) == 0 {
		fmt.Fprintln(os.Stderr, "tangsible: none of the values could be decrypted with the given passwords (wrong password?); they show as placeholders and are saved unchanged, and any value you add is encrypted with the first password or --encrypt-vault-id")
		return
	}
	labels := make([]string, 0, len(locked))
	for l := range locked {
		labels = append(labels, l)
	}
	sort.Strings(labels)
	for _, l := range labels {
		fmt.Fprintf(os.Stderr, "tangsible: %d value(s) with the vault id %q are not decryptable with the given passwords; they show as placeholders and are saved unchanged\n", locked[l], l)
	}
}
