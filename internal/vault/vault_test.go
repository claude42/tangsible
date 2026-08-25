package vault

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"code.aw.net/claude/tangsible/internal/vaultfile"
)

func TestParseVaultArgs(t *testing.T) {
	cases := []struct {
		name             string
		args             []string
		wantFilename     string
		wantPasswordFile string
		wantAskPass      bool
		wantErr          bool
	}{
		{"no args", nil, "", "", false, true},
		{"flag-shaped first arg", []string{"--vault-password-file", "x"}, "", "", false, true},
		{"filename only", []string{"secrets.yml"}, "secrets.yml", "", false, false},
		{"space-separated password file", []string{"secrets.yml", "--vault-password-file", "pw.txt"}, "secrets.yml", "pw.txt", false, false},
		{"equals-form password file", []string{"secrets.yml", "--vault-password-file=pw.txt"}, "secrets.yml", "pw.txt", false, false},
		{"ask pass", []string{"secrets.yml", "--ask-vault-pass"}, "secrets.yml", "", true, false},
		{"both password sources rejected", []string{"secrets.yml", "--vault-password-file", "pw.txt", "--ask-vault-pass"}, "", "", false, true},
		{"dangling password file flag ignored", []string{"secrets.yml", "--vault-password-file"}, "secrets.yml", "", false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			filename, passwordFile, askPass, err := ParseVaultArgs(c.args)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, c.wantErr)
			}
			if c.wantErr {
				return
			}
			if filename != c.wantFilename || passwordFile != c.wantPasswordFile || askPass != c.wantAskPass {
				t.Errorf("got (%q, %q, %v), want (%q, %q, %v)", filename, passwordFile, askPass, c.wantFilename, c.wantPasswordFile, c.wantAskPass)
			}
		})
	}
}

func TestChoosePasswordSource(t *testing.T) {
	cases := []struct {
		name             string
		flagFile         string
		askFlag          bool
		envFile          string
		ansibleCfgFile   string
		tangsibleCfgFile string
		wantKind         passwordSourceKind
		wantFile         string
	}{
		{"flag wins over everything", "flag.txt", true, "env.txt", "ansible.txt", "cfg.txt", passwordSourceFileKind, "flag.txt"},
		{"ask flag wins over env, ansible.cfg, and config", "", true, "env.txt", "ansible.txt", "cfg.txt", passwordSourcePromptKind, ""},
		{"env wins over ansible.cfg and config", "", false, "env.txt", "ansible.txt", "cfg.txt", passwordSourceFileKind, "env.txt"},
		{"ansible.cfg wins over tangsible's own config", "", false, "", "ansible.txt", "cfg.txt", passwordSourceFileKind, "ansible.txt"},
		{"tangsible config is the last file-backed fallback", "", false, "", "", "cfg.txt", passwordSourceFileKind, "cfg.txt"},
		{"prompt when nothing is configured", "", false, "", "", "", passwordSourcePromptKind, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kind, file := choosePasswordSource(c.flagFile, c.askFlag, c.envFile, c.ansibleCfgFile, c.tangsibleCfgFile)
			if kind != c.wantKind || file != c.wantFile {
				t.Errorf("got (%v, %q), want (%v, %q)", kind, file, c.wantKind, c.wantFile)
			}
		})
	}
}

// setupEditorLoopFixture copies the shared repo testdata/vault.yml fixture
// into a fresh temp dir (runEditorLoop writes to it directly, so each test
// needs its own copy) and returns the decrypted view plus the target
// path.
func setupEditorLoopFixture(t *testing.T) (vaultfile.DecryptedView, string) {
	t.Helper()
	src := readFile(t, filepath.Join("..", "..", "testdata", "vault.yml"))

	dir := t.TempDir()
	targetPath := filepath.Join(dir, "vault.yml")
	writeFile(t, targetPath, src)

	view, err := vaultfile.BuildDecryptedView([]byte(src), testFixturePassword)
	if err != nil {
		t.Fatalf("BuildDecryptedView: %v", err)
	}
	return view, targetPath
}

const testFixturePassword = "tangsible-vaultfile-test-password"

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}

func writeFile(t *testing.T, path, content string) error {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	return nil
}

// askFailsIfCalled is the ask func for tests where the editor never
// produces a Problem - if runEditorLoop ever prompts anyway, that's a
// bug in the loop's own control flow, not something to route around.
func askFailsIfCalled(t *testing.T) askFixOrRevertFunc {
	return func() (bool, error) {
		t.Fatal("askFixOrRevert should not have been called")
		return false, nil
	}
}

func TestRunEditorLoop_NoOpMakesNoChange(t *testing.T) {
	view, targetPath := setupEditorLoopFixture(t)
	original := readFile(t, targetPath)

	edit := func(ctx context.Context, path string) error { return nil } // editor "closes" without touching the file

	code := runEditorLoop(targetPath, view, testFixturePassword, edit, askFailsIfCalled(t), context.Background())
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if got := readFile(t, targetPath); got != original {
		t.Error("target file was rewritten even though nothing changed")
	}
}

func TestRunEditorLoop_SuccessfulEditWrites(t *testing.T) {
	view, targetPath := setupEditorLoopFixture(t)

	edit := func(ctx context.Context, path string) error {
		content := readFile(t, path)
		content = strings.Replace(content, "region: us-east-1", "region: eu-west-1", 1)
		writeFile(t, path, content)
		return nil
	}

	code := runEditorLoop(targetPath, view, testFixturePassword, edit, askFailsIfCalled(t), context.Background())
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if got := readFile(t, targetPath); !strings.Contains(got, "eu-west-1") {
		t.Errorf("target file wasn't updated:\n%s", got)
	}
}

// TestRunEditorLoop_ProblemReopensThenSucceeds exercises the "fix" branch
// of the fix-or-revert prompt: choosing to fix reopens the editor with
// the problem annotated, exactly as before this prompt existed.
func TestRunEditorLoop_ProblemReopensThenSucceeds(t *testing.T) {
	view, targetPath := setupEditorLoopFixture(t)

	calls := 0
	edit := func(ctx context.Context, path string) error {
		calls++
		content := readFile(t, path)
		if calls == 1 {
			// Break db_password's type - this must NOT be written to
			// targetPath, and must come back annotated on the next round.
			content = strings.Replace(content, "db_password: a top secret db password", "db_password: 12345", 1)
			writeFile(t, path, content)
			return nil
		}
		// Second round: the annotation comment should be present, and we
		// now fix the problem for real.
		if !strings.Contains(content, "# TANGSIBLE VAULT:") {
			t.Fatalf("round 2 didn't receive an annotated file:\n%s", content)
		}
		content = strings.Replace(content, "db_password: 12345", "db_password: a fixed value", 1)
		writeFile(t, path, content)
		return nil
	}
	askCalls := 0
	ask := func() (bool, error) {
		askCalls++
		return true, nil // always choose "fix"
	}

	code := runEditorLoop(targetPath, view, testFixturePassword, edit, ask, context.Background())
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if calls != 2 {
		t.Fatalf("editor invoked %d times, want 2", calls)
	}
	if askCalls != 1 {
		t.Fatalf("askFixOrRevert invoked %d times, want 1", askCalls)
	}
	if got := readFile(t, targetPath); strings.Contains(got, "12345") {
		t.Errorf("the invalid intermediate value must never reach the target file:\n%s", got)
	}
	// Real bug caught live: the annotation comment from round 1 must not
	// linger in the file once the problem it described is actually fixed.
	if got := readFile(t, targetPath); strings.Contains(got, "TANGSIBLE VAULT") {
		t.Errorf("a resolved problem's annotation comment leaked into the saved file:\n%s", got)
	}
}

// TestRunEditorLoop_StaleAnnotationDoesNotDuplicate is the other real bug
// StripAnnotations exists to prevent: if the user saves again without
// having fixed the problem, the next round must show exactly one
// annotation comment, not one stacked on top of the previous round's.
func TestRunEditorLoop_StaleAnnotationDoesNotDuplicate(t *testing.T) {
	view, targetPath := setupEditorLoopFixture(t)

	calls := 0
	edit := func(ctx context.Context, path string) error {
		calls++
		content := readFile(t, path)
		switch calls {
		case 1:
			// Break db_password's type and save without fixing it.
			content = strings.Replace(content, "db_password: a top secret db password", "db_password: 12345", 1)
		case 2:
			// Still broken - the file already has one annotation from
			// round 1. Save it again, completely untouched.
			if n := strings.Count(content, "TANGSIBLE VAULT"); n != 1 {
				t.Fatalf("round 2 started with %d annotations, want 1:\n%s", n, content)
			}
		case 3:
			// Still exactly one annotation, not stacked - then fix it.
			if n := strings.Count(content, "TANGSIBLE VAULT"); n != 1 {
				t.Fatalf("round 3 started with %d annotations, want 1 (stacked, not deduplicated):\n%s", n, content)
			}
			content = strings.Replace(content, "db_password: 12345", "db_password: a fixed value", 1)
		}
		writeFile(t, path, content)
		return nil
	}
	ask := func() (bool, error) { return true, nil } // always choose "fix"

	code := runEditorLoop(targetPath, view, testFixturePassword, edit, ask, context.Background())
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if calls != 3 {
		t.Fatalf("editor invoked %d times, want 3", calls)
	}
}

// TestRunEditorLoop_ProblemUserReverts is the direct test for this
// project's actual answer to "how do I get out of the reopen loop": not
// Ctrl-C (which can't reliably reach tangsible while a real full-screen
// editor is open - see askFixOrRevert's own doc comment), but an explicit
// prompt asking to fix or revert, shown on tangsible's own terminal after
// the editor has already exited and handed control back.
func TestRunEditorLoop_ProblemUserReverts(t *testing.T) {
	view, targetPath := setupEditorLoopFixture(t)
	original := readFile(t, targetPath)

	calls := 0
	edit := func(ctx context.Context, path string) error {
		calls++
		content := readFile(t, path)
		content = strings.Replace(content, "db_password: a top secret db password", "db_password: 12345", 1)
		writeFile(t, path, content)
		return nil
	}
	ask := func() (bool, error) { return false, nil } // choose "revert"

	code := runEditorLoop(targetPath, view, testFixturePassword, edit, ask, context.Background())
	if code != abortExitCode {
		t.Fatalf("exit code = %d, want %d", code, abortExitCode)
	}
	if calls != 1 {
		t.Fatalf("editor invoked %d times, want 1 (no reopen after reverting)", calls)
	}
	if got := readFile(t, targetPath); got != original {
		t.Error("target file was modified despite reverting")
	}
}

// TestRunEditorLoop_AbortLeavesTargetUntouched covers the secondary
// safety net: Ctrl-C reaching tangsible directly (only possible outside
// an active editor session) still cleanly aborts via ctx cancellation,
// same as before the fix-or-revert prompt existed.
func TestRunEditorLoop_AbortLeavesTargetUntouched(t *testing.T) {
	view, targetPath := setupEditorLoopFixture(t)
	original := readFile(t, targetPath)

	ctx, cancel := context.WithCancel(context.Background())
	edit := func(ctx context.Context, path string) error {
		cancel() // simulate Ctrl-C arriving while the "editor" is running
		return ctx.Err()
	}

	code := runEditorLoop(targetPath, view, testFixturePassword, edit, askFailsIfCalled(t), ctx)
	if code != abortExitCode {
		t.Fatalf("exit code = %d, want %d", code, abortExitCode)
	}
	if got := readFile(t, targetPath); got != original {
		t.Error("target file was modified despite an abort")
	}
}
