package vault

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"code.aw.net/claude/tangsible/internal/template"
	"code.aw.net/claude/tangsible/internal/vaultfile"
)

// abortExitCode is what RunVaultVerb returns when the user reverts out of
// the reopen loop (see askFixOrRevert) or aborts before/between editor
// rounds via Ctrl-C - 128+SIGINT is the conventional shell exit code for
// "killed by this signal" and is reused here for both cases, distinct
// from ansible-playbook's own 99 (a different process's own convention
// for a similar gesture).
const abortExitCode = 130

// editorFunc runs an editor against the file at path and waits for it to
// finish, honoring ctx's cancellation. Abstracted out of runEditorLoop so
// tests can substitute a fake editor that just rewrites the temp file
// directly, with no real process spawned - the loop's own decision logic
// (no-op detection, reopen vs. write vs. abort) is what needs testing,
// not a real $EDITOR invocation.
type editorFunc func(ctx context.Context, path string) error

// realEditor invokes template.PreferredEditor() - reused directly rather
// than duplicated, per design-docs/Vault.md's implementation plan - with
// stdio wired straight through to tangsible's own. Unlike tui.go's own
// editor call sites, there's no app.Suspend here: this verb never touches
// tview, so there's no alternate-screen app to hand the terminal back to.
//
// Driven by exec.CommandContext so a Ctrl-C that reaches tangsible before
// or between editor rounds (see RunVaultVerb's signal.NotifyContext) also
// kills a *currently running* editor rather than leaving it orphaned -
// but this is a secondary safety net now, not the primary way out of the
// reopen loop. See askFixOrRevert's own doc comment for why: a raw-mode
// full-screen editor (vim, and similar) disables the terminal's own
// SIGINT-on-Ctrl-C generation for the *whole shared terminal* while it's
// running, not just for itself, so Ctrl-C is structurally unable to reach
// tangsible at all for as long as such an editor is actually open -
// confirmed directly (a minimal repro outside this codebase never
// observed the signal), not just inferred.
func realEditor(ctx context.Context, path string) error {
	cmd := exec.CommandContext(ctx, template.PreferredEditor(), path)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// askFixOrRevertFunc asks the user, on tangsible's own terminal, whether
// to reopen the editor to fix a just-detected problem or give up and
// discard every change made so far. Returns fix=true to reopen, false to
// revert.
type askFixOrRevertFunc func() (fix bool, err error)

// askFixOrRevert is the real implementation, called only after the
// editor has already exited and handed the terminal back - deliberately,
// since that's what makes this reliable where Ctrl-C wasn't: tangsible's
// own terminal is in ordinary cooked mode at this point (it never puts
// the terminal in raw mode itself, unlike the run/rerun/role tree's tview
// app), so a plain blocking read here behaves exactly as any other CLI
// prompt would, regardless of which editor was just used or how it
// happened to leave signal handling.
func askFixOrRevert() (bool, error) {
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Fprint(os.Stderr, "Fix this in the editor again, or revert and discard all changes? [f/r] ")
		line, err := reader.ReadString('\n')
		if err != nil {
			return false, err
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "f", "fix":
			return true, nil
		case "r", "revert":
			return false, nil
		default:
			fmt.Fprintln(os.Stderr, "tangsible: please answer 'f' or 'r'")
		}
	}
}

// runEditorLoop drives design-docs/Vault.md's edit/validate/reopen cycle
// for one target file: create a private 0700 scratch dir holding view's
// plaintext in a 0600 file, invoke edit repeatedly until the user
// produces something valid or chooses to revert (askFixOrRevert), and on
// success write the result to targetPath. Returns the process exit code.
//
// ctx is expected to come from signal.NotifyContext(..., os.Interrupt,
// SIGTERM, SIGHUP) - see RunVaultVerb - and still guards the moments
// where tangsible itself is doing the blocking (the password prompt,
// askFixOrRevert, reading files): a genuine, unhandled terminating
// signal there would otherwise bypass Go's own deferred cleanup and
// leave the decrypted scratch dir behind. It is *not* the primary way to
// escape a detected problem, though - see askFixOrRevert's own doc
// comment for why Ctrl-C can't reliably reach tangsible at all while a
// real full-screen editor is open.
func runEditorLoop(targetPath string, view vaultfile.DecryptedView, password string, edit editorFunc, askFixOrRevert askFixOrRevertFunc, ctx context.Context) int {
	// A private scratch *directory* (0700), not a bare file in the shared
	// /tmp - design-docs/Vault.md point 6's "restrictive permissions" for
	// a file briefly holding decrypted secrets, hardened two ways:
	//   - the plaintext never lands directly in a world-traversable
	//     directory, only inside this per-run 0700 dir;
	//   - os.RemoveAll sweeps the whole dir on the way out, so an editor's
	//     own spill files (vim's .swp, backups, persistent-undo files) -
	//     which land next to the scratch file and also hold the decrypted
	//     plaintext - are cleaned up too, where a bare os.Remove of just
	//     the scratch file would leave them behind.
	dir, err := os.MkdirTemp("", "tangsible-vault-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "tangsible: couldn't create a private scratch directory: %v\n", err)
		return 1
	}
	defer os.RemoveAll(dir)

	// Named .yml (rather than a random basename) purely so $EDITOR picks
	// up YAML syntax - safe to be predictable now that it lives inside a
	// 0700 dir nothing else can traverse.
	tmpPath := filepath.Join(dir, "vault.yml")
	if err := os.WriteFile(tmpPath, []byte(view.PlaintextYAML), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "tangsible: couldn't write the scratch file: %v\n", err)
		return 1
	}

	// revert reports the user's choice (or a stdin failure, treated the
	// same way - never getting stuck asking again) as "give up": prints a
	// closing message and returns true, so callers can just
	// `if revert() { return abortExitCode }`.
	revert := func() bool {
		fix, err := askFixOrRevert()
		if err != nil || !fix {
			fmt.Fprintln(os.Stderr, "tangsible: reverted - no changes were saved")
			return true
		}
		return false
	}

	for {
		editErr := edit(ctx, tmpPath)
		if ctx.Err() != nil {
			// Ctrl-C reached tangsible directly - only possible outside an
			// active editor session (before the first round, or between
			// rounds while this loop itself is running Go code).
			return abortExitCode
		}
		if editErr != nil {
			fmt.Fprintf(os.Stderr, "tangsible: editor exited with an error: %v\n", editErr)
		}

		edited, err := os.ReadFile(tmpPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "tangsible: couldn't read the scratch file back: %v\n", err)
			return 1
		}
		// Strip any annotation from a previous round before looking at
		// anything else - a real, live-caught bug otherwise: leaving a
		// stale annotation in place either stacks a duplicate on top of
		// itself next round (if the same problem still isn't fixed) or
		// lingers permanently in the file actually saved to targetPath
		// (if it was fixed - the comment about it is no longer relevant,
		// but nothing else would ever remove it). See
		// vaultfile.StripAnnotations' own doc comment.
		edited = vaultfile.StripAnnotations(edited)

		// No-op short-circuit: a raw byte comparison against the exact
		// text written before the editor opened is what actually
		// guarantees "same file, same mtime, no diff" - independent of
		// Reassemble's own YAML round-trip fidelity. Still correct on a
		// later round where the *only* prior difference was an
		// annotation (now stripped above): if the user genuinely fixed
		// nothing else either, the underlying value is still wrong, so
		// this comparison still won't match view.PlaintextYAML, and
		// Reassemble below will (correctly) raise the same Problem again.
		if bytes.Equal(edited, []byte(view.PlaintextYAML)) {
			return 0
		}

		result, err := vaultfile.Reassemble(view, edited, password)
		if errors.Is(err, vaultfile.ErrInvalidYAML) {
			// No line to annotate a comment above - show the raw error
			// and let the user choose whether to try again or give up,
			// same as any other Problem (design-docs/Vault.md point 5).
			fmt.Fprintf(os.Stderr, "tangsible: %v\n", err)
			if revert() {
				return abortExitCode
			}
			continue
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "tangsible: %v\n", err)
			return 1
		}

		if len(result.Problems) > 0 {
			for _, p := range result.Problems {
				fmt.Fprintf(os.Stderr, "tangsible: %s\n", p.Msg)
			}
			if revert() {
				return abortExitCode
			}
			annotated := vaultfile.AnnotateProblems(edited, result.Problems)
			if err := os.WriteFile(tmpPath, annotated, 0o600); err != nil {
				fmt.Fprintf(os.Stderr, "tangsible: couldn't write the annotated scratch file: %v\n", err)
				return 1
			}
			continue
		}

		for _, w := range result.Warnings {
			fmt.Fprintln(os.Stderr, w)
		}

		mode := os.FileMode(0o600)
		if info, err := os.Stat(targetPath); err == nil {
			mode = info.Mode()
		}
		if err := os.WriteFile(targetPath, []byte(result.YAML), mode); err != nil {
			fmt.Fprintf(os.Stderr, "tangsible: couldn't write %s: %v\n", targetPath, err)
			return 1
		}
		return 0
	}
}
