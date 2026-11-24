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

//go:build e2e

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Smoke tests for the vault verb (design-docs/Vault.md): the real binary,
// a real $EDITOR (a one-line shell script) and, to check the result, the
// real ansible decrypting what tangsible wrote. No TUI is involved, so no
// tmux either. The fixture is testdata/vault.yml (password in
// testdata/vault-password.txt).

func requireAnsibleVault(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"ansible", "ansible-vault"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found on PATH - skipping e2e test", tool)
		}
	}
}

// vaultFixture copies the vault fixture and its password into a fresh dir.
func vaultFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, f := range []string{"vault.yml", "vault-password.txt"} {
		data, err := os.ReadFile(filepath.Join(repoRoot(t), "testdata", f))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, f), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// runVault runs "tangsible vault <args>" in dir with editorScript as the
// editor (a shell script body; $1 is the file to edit). It returns the exit
// code and the combined output; it also fails the test if the scratch
// directory with the decrypted view is still there afterwards.
func runVault(t *testing.T, dir, editorScript string, args ...string) (int, string) {
	t.Helper()
	editor := filepath.Join(t.TempDir(), "editor.sh")
	if err := os.WriteFile(editor, []byte("#!/bin/sh\n"+editorScript+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(buildE2EBinary(t), append([]string{"vault"}, args...)...)
	cmd.Dir = dir
	// The plaintext scratch directory goes under TMPDIR: a private one lets
	// the test check that nothing is left in it afterwards.
	tmp := t.TempDir()
	cmd.Env = append(os.Environ(), "EDITOR="+editor, "VISUAL=", "ANSIBLE_VAULT_PASSWORD_FILE=", "TMPDIR="+tmp)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	if left, _ := os.ReadDir(tmp); len(left) > 0 {
		t.Errorf("the plaintext scratch directory outlived the run: %v", left)
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode(), out.String()
	} else if err != nil {
		t.Fatal(err)
	}
	return 0, out.String()
}

// ansibleValue asks the real ansible for variable name's value in file,
// decrypting with the fixture password.
func ansibleValue(t *testing.T, dir, file, name string) string {
	t.Helper()
	cmd := exec.Command("ansible", "localhost", "-i", "localhost,", "-c", "local", "-m", "debug", "-a", "var="+name,
		"-e", "@"+file, "--vault-password-file", "vault-password.txt")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ansible could not read %s from %s: %v\n%s", name, file, err, out)
	}
	return string(out)
}

func TestE2E_Vault_EditChangesOnlyTheEditedValues(t *testing.T) {
	requireE2ETools(t)
	requireAnsibleVault(t)
	dir := vaultFixture(t)
	original, _ := os.ReadFile(filepath.Join(dir, "vault.yml"))

	code, out := runVault(t, dir,
		`sed -i -e 's/^db_password: .*/db_password: rotated secret/' -e 's/^region: .*/region: eu-west-1/' "$1"`,
		"vault.yml", "--vault-password-file", "vault-password.txt")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	edited, _ := os.ReadFile(filepath.Join(dir, "vault.yml"))
	if !strings.Contains(string(edited), "region: eu-west-1") {
		t.Errorf("the plain value was not changed:\n%s", edited)
	}
	if strings.Contains(string(edited), "rotated secret") {
		t.Errorf("the new secret was written in clear text:\n%s", edited)
	}
	// The untouched secret keeps its exact bytes (nothing is re-encrypted
	// that did not change): everything from api_token on is as before.
	tail := func(b []byte) string { return string(b[bytes.Index(b, []byte("api_token:")):]) }
	if tail(edited) != tail(original) {
		t.Errorf("the unchanged api_token block was rewritten:\nbefore:\n%s\nafter:\n%s", tail(original), tail(edited))
	}
	// ... and the real ansible reads both secrets back.
	if got := ansibleValue(t, dir, "vault.yml", "db_password"); !strings.Contains(got, "rotated secret") {
		t.Errorf("ansible reads db_password as:\n%s", got)
	}
	if got := ansibleValue(t, dir, "vault.yml", "api_token"); !strings.Contains(got, "another secret, an api token") {
		t.Errorf("ansible reads api_token as:\n%s", got)
	}
}

func TestE2E_Vault_EditorSeesPlaintextAndNoChangeLeavesTheFileAlone(t *testing.T) {
	requireE2ETools(t)
	dir := vaultFixture(t)
	original, _ := os.ReadFile(filepath.Join(dir, "vault.yml"))
	seen := filepath.Join(t.TempDir(), "seen.txt")

	code, out := runVault(t, dir, `cp "$1" `+shellQuote(seen), "vault.yml", "--vault-password-file", "vault-password.txt")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	view, _ := os.ReadFile(seen)
	for _, want := range []string{"a top secret db password", "another secret, an api token", "region: us-east-1"} {
		if !strings.Contains(string(view), want) {
			t.Errorf("the editor's view lacks %q:\n%s", want, view)
		}
	}
	if strings.Contains(string(view), "ANSIBLE_VAULT") {
		t.Errorf("the editor's view still holds ciphertext:\n%s", view)
	}
	if after, _ := os.ReadFile(filepath.Join(dir, "vault.yml")); !bytes.Equal(after, original) {
		t.Errorf("an unchanged edit rewrote the file:\n%s", after)
	}
}

func TestE2E_Vault_WrongPasswordFailsWithoutTouchingTheFile(t *testing.T) {
	requireE2ETools(t)
	dir := vaultFixture(t)
	original, _ := os.ReadFile(filepath.Join(dir, "vault.yml"))
	os.WriteFile(filepath.Join(dir, "wrong.txt"), []byte("not the password\n"), 0o600)

	code, out := runVault(t, dir, `echo editor ran > editor-ran`, "vault.yml", "--vault-password-file", "wrong.txt")
	if code != 1 {
		t.Errorf("exit %d, want 1:\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "editor-ran")); err == nil {
		t.Error("the editor was opened although nothing could be decrypted")
	}
	if after, _ := os.ReadFile(filepath.Join(dir, "vault.yml")); !bytes.Equal(after, original) {
		t.Error("the file changed")
	}
}

func TestE2E_Vault_UsageAndMissingFileErrors(t *testing.T) {
	requireE2ETools(t)
	dir := vaultFixture(t)
	if code, out := runVault(t, dir, "true"); code != 2 || !strings.Contains(out, "a target file is required") {
		t.Errorf("no file: exit %d:\n%s", code, out)
	}
	if code, out := runVault(t, dir, "true", "vault.yml", "--ask-vault-pass", "--vault-password-file", "vault-password.txt"); code != 2 || !strings.Contains(out, "can't be used together") {
		t.Errorf("both password sources: exit %d:\n%s", code, out)
	}
	if code, out := runVault(t, dir, "true", "nope.yml", "--vault-password-file", "vault-password.txt"); code != 1 || !strings.Contains(out, "couldn't read nope.yml") {
		t.Errorf("missing file: exit %d:\n%s", code, out)
	}
}
