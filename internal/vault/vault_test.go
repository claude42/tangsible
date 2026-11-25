package vault

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"code.aw.net/claude/tangsible/internal/vaultcrypto"
	"code.aw.net/claude/tangsible/internal/vaultfile"
)

func TestParseVaultArgs(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		want    Args
		wantErr bool
	}{
		{"no args", nil, Args{}, true},
		{"only flags, no filename", []string{"--vault-password-file", "x"}, Args{}, true},
		{"filename only", []string{"secrets.yml"}, Args{Filename: "secrets.yml"}, false},
		{"space-separated password file", []string{"secrets.yml", "--vault-password-file", "pw.txt"}, Args{Filename: "secrets.yml", PasswordFile: "pw.txt"}, false},
		{"equals-form password file", []string{"secrets.yml", "--vault-password-file=pw.txt"}, Args{Filename: "secrets.yml", PasswordFile: "pw.txt"}, false},
		{"the alias of the password file flag", []string{"secrets.yml", "--vault-pass-file", "pw.txt"}, Args{Filename: "secrets.yml", PasswordFile: "pw.txt"}, false},
		{"ask pass", []string{"secrets.yml", "--ask-vault-pass"}, Args{Filename: "secrets.yml", AskPass: true}, false},
		{"both password sources rejected", []string{"secrets.yml", "--vault-password-file", "pw.txt", "--ask-vault-pass"}, Args{}, true},
		{"options before the filename", []string{"--vault-password-file", "pw.txt", "secrets.yml"}, Args{Filename: "secrets.yml", PasswordFile: "pw.txt"}, false},
		{"ask pass before the filename", []string{"--ask-vault-pass", "secrets.yml"}, Args{Filename: "secrets.yml", AskPass: true}, false},
		{"password file value that looks like a file", []string{"--vault-password-file=pw.yml", "secrets.yml"}, Args{Filename: "secrets.yml", PasswordFile: "pw.yml"}, false},
		{"dangling password file flag ignored", []string{"secrets.yml", "--vault-password-file"}, Args{Filename: "secrets.yml"}, false},
		{"vault ids, both spellings, in order", []string{"--vault-id", "dev@d.txt", "secrets.yml", "--vault-id=prod@prompt"},
			Args{Filename: "secrets.yml", VaultIDs: []string{"dev@d.txt", "prod@prompt"}}, false},
		{"a vault id value is not the filename", []string{"--vault-id", "pw.txt", "secrets.yml"}, Args{Filename: "secrets.yml", VaultIDs: []string{"pw.txt"}}, false},
		{"encrypt vault id", []string{"--encrypt-vault-id", "prod", "secrets.yml"}, Args{Filename: "secrets.yml", EncryptID: "prod"}, false},
		{"dangling vault id is an error", []string{"secrets.yml", "--vault-id"}, Args{}, true},
		{"dangling encrypt vault id is an error", []string{"secrets.yml", "--encrypt-vault-id"}, Args{}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ParseVaultArgs(c.args)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, c.wantErr)
			}
			if c.wantErr {
				return
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestSecretSpecs(t *testing.T) {
	d := vaultcrypto.DefaultID
	cases := []struct {
		name string
		args Args
		src  passwordSources
		want []secretSpec
	}{
		{"flag wins over everything", Args{PasswordFile: "flag.txt", AskPass: false}, passwordSources{EnvFile: "env.txt", CfgFile: "ansible.txt"},
			[]secretSpec{{d, "flag.txt"}}},
		{"ask flag wins over env and ansible.cfg", Args{AskPass: true}, passwordSources{EnvFile: "env.txt", CfgFile: "ansible.txt"},
			[]secretSpec{{d, askSource}}},
		{"env wins over ansible.cfg", Args{}, passwordSources{EnvFile: "env.txt", CfgFile: "ansible.txt"}, []secretSpec{{d, "env.txt"}}},
		{"ansible.cfg is the last file-backed fallback", Args{}, passwordSources{CfgFile: "ansible.txt"}, []secretSpec{{d, "ansible.txt"}}},
		{"prompt when nothing is configured", Args{}, passwordSources{}, []secretSpec{{d, askSource}}},
		{"the identity list comes first, then --vault-id, then the single password",
			Args{VaultIDs: []string{"b@b.txt", "plain.txt"}, PasswordFile: "flag.txt"},
			passwordSources{IdentityList: []string{"a@a.txt"}},
			[]secretSpec{{"a", "a.txt"}, {"b", "b.txt"}, {d, "plain.txt"}, {d, "flag.txt"}}},
		{"explicit ids do not add a prompt", Args{VaultIDs: []string{"prod@prompt"}}, passwordSources{}, []secretSpec{{"prod", promptSource}}},
		{"an @ later in a path is part of the source", Args{VaultIDs: []string{"x@/tmp/a@b"}}, passwordSources{}, []secretSpec{{"x", "/tmp/a@b"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := secretSpecs(c.args, c.src); !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestLoadSecrets(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "pw")
	writeFile(t, file, "from-file\n")
	script := filepath.Join(dir, "pw.sh")
	writeFile(t, script, "#!/bin/sh\necho from-script\n")
	if err := os.Chmod(script, 0o700); err != nil {
		t.Fatal(err)
	}
	client := filepath.Join(dir, "vault-client.sh")
	writeFile(t, client, "#!/bin/sh\necho \"for $2\"\n")
	if err := os.Chmod(client, 0o700); err != nil {
		t.Fatal(err)
	}
	var asked []string
	prompt := func(id string, ask bool) (string, error) {
		asked = append(asked, id)
		return "typed-" + id, nil
	}
	got, err := loadSecrets([]secretSpec{{"a", file}, {"b", script}, {"c", client}, {"d", promptSource}, {"e", askSource}}, prompt)
	if err != nil {
		t.Fatal(err)
	}
	want := []vaultcrypto.Secret{{ID: "a", Password: "from-file"}, {ID: "b", Password: "from-script"},
		{ID: "c", Password: "for c"}, {ID: "d", Password: "typed-d"}, {ID: "e", Password: "typed-e"}}
	if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(asked, []string{"d", "e"}) {
		t.Errorf("got %v (asked %v)", got, asked)
	}
	if _, err := loadSecrets([]secretSpec{{"x", filepath.Join(dir, "missing")}}, prompt); err == nil {
		t.Error("a missing password file must be an error")
	}
	if _, err := loadSecrets([]secretSpec{{"x", dir}}, prompt); err == nil {
		t.Error("a directory must be an error")
	}
}

func TestChooseEncryptSecret(t *testing.T) {
	dev := vaultcrypto.Secret{ID: "dev", Password: "d"}
	prod := vaultcrypto.Secret{ID: "prod", Password: "p"}
	both := []vaultcrypto.Secret{dev, prod}
	view := func(labels ...string) vaultfile.DecryptedView {
		v := vaultfile.DecryptedView{UsedBefore: map[string]vaultfile.Used{}, LockedBefore: map[string]string{}}
		for i, l := range labels {
			v.UsedBefore[string(rune('a'+i))] = vaultfile.Used{Label: l}
		}
		return v
	}
	cases := []struct {
		name    string
		view    vaultfile.DecryptedView
		id      string
		want    string
		wantErr bool
	}{
		{"explicit id", view("dev"), "prod", "prod", false},
		{"explicit id that was not given", view(), "qa", "", true},
		{"the file is all prod", view("prod", "prod"), "", "prod", false},
		{"mixed file: the first password, as ansible does", view("prod", "dev"), "", "dev", false},
		{"empty file: the first password", view(), "", "dev", false},
		{"the file's one id has no password: the first", view("qa"), "", "dev", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := chooseEncryptSecret(c.view, both, c.id)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v", err)
			}
			if err == nil && got.ID != c.want {
				t.Errorf("got %q, want %q", got.ID, c.want)
			}
		})
	}
	// An unlabeled value counts as the default id.
	def := vaultcrypto.Secret{ID: "default", Password: "x"}
	if got, _ := chooseEncryptSecret(view("", ""), []vaultcrypto.Secret{dev, def}, ""); got.ID != "default" {
		t.Errorf("unlabeled values: got %q", got.ID)
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

	view, err := vaultfile.BuildDecryptedView([]byte(src), []vaultcrypto.Secret{{ID: "default", Password: testFixturePassword}})
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

	code := runEditorLoop(targetPath, view, &vaultcrypto.Secret{ID: "default", Password: testFixturePassword}, edit, askFailsIfCalled(t), context.Background())
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

	code := runEditorLoop(targetPath, view, &vaultcrypto.Secret{ID: "default", Password: testFixturePassword}, edit, askFailsIfCalled(t), context.Background())
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if got := readFile(t, targetPath); !strings.Contains(got, "eu-west-1") {
		t.Errorf("target file wasn't updated:\n%s", got)
	}
}

// TestRunEditorLoop_SweepsPrivateScratchDir checks design-docs/Vault.md
// point 6's cleanup guarantee: the whole private scratch directory - not
// just the one scratch file - is removed on return, so an editor's own
// spill files (a .swp here) can't leave decrypted plaintext behind.
func TestRunEditorLoop_SweepsPrivateScratchDir(t *testing.T) {
	view, targetPath := setupEditorLoopFixture(t)

	var scratchDir, swapFile string
	edit := func(ctx context.Context, path string) error {
		scratchDir = filepath.Dir(path)
		if base := filepath.Base(scratchDir); !strings.HasPrefix(base, "tangsible-vault-") {
			t.Errorf("scratch file not in a private per-run dir: %s", path)
		}
		if info, err := os.Stat(scratchDir); err != nil {
			t.Fatalf("stat scratch dir: %v", err)
		} else if perm := info.Mode().Perm(); perm != 0o700 {
			t.Errorf("scratch dir perm = %o, want 700", perm)
		}
		// Simulate vim dropping a swap file next to the scratch file.
		swapFile = filepath.Join(scratchDir, ".vault.yml.swp")
		writeFile(t, swapFile, "decrypted secrets in here")
		return nil
	}

	code := runEditorLoop(targetPath, view, &vaultcrypto.Secret{ID: "default", Password: testFixturePassword}, edit, askFailsIfCalled(t), context.Background())
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if _, err := os.Stat(scratchDir); !os.IsNotExist(err) {
		t.Errorf("scratch dir %s survived, err = %v", scratchDir, err)
	}
	if _, err := os.Stat(swapFile); !os.IsNotExist(err) {
		t.Errorf("editor spill file %s survived, err = %v", swapFile, err)
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

	code := runEditorLoop(targetPath, view, &vaultcrypto.Secret{ID: "default", Password: testFixturePassword}, edit, ask, context.Background())
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

	code := runEditorLoop(targetPath, view, &vaultcrypto.Secret{ID: "default", Password: testFixturePassword}, edit, ask, context.Background())
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

	code := runEditorLoop(targetPath, view, &vaultcrypto.Secret{ID: "default", Password: testFixturePassword}, edit, ask, context.Background())
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

	code := runEditorLoop(targetPath, view, &vaultcrypto.Secret{ID: "default", Password: testFixturePassword}, edit, askFailsIfCalled(t), ctx)
	if code != abortExitCode {
		t.Fatalf("exit code = %d, want %d", code, abortExitCode)
	}
	if got := readFile(t, targetPath); got != original {
		t.Error("target file was modified despite an abort")
	}
}
