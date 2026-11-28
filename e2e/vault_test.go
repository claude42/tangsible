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

package e2e

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
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
	// b64encode, not var=: newer ansible-core prints a vaulted value as
	// $REDACTED$ (also for msg={{ name }}), but a filter's result is a plain
	// string - the same "marker is lost" the debug masking has to cope with.
	cmd := exec.Command("ansible", "localhost", "-i", "localhost,", "-c", "local", "-m", "debug", "-a", "msg={{ "+name+" | b64encode }}",
		"-e", "@"+file, "--vault-password-file", "vault-password.txt")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ansible could not read %s from %s: %v\n%s", name, file, err, out)
	}
	return decodeAnsibleMsg(t, name, out)
}

// decodeAnsibleMsg is the plain text of an ad-hoc debug msg that carried
// "{{ name | b64encode }}".
func decodeAnsibleMsg(t *testing.T, name string, out []byte) string {
	t.Helper()
	m := regexp.MustCompile(`"msg": "([^"]*)"`).FindSubmatch(out)
	if m == nil {
		t.Fatalf("ansible's answer for %s has no msg:\n%s", name, out)
	}
	plain, err := base64.StdEncoding.DecodeString(string(m[1]))
	if err != nil {
		t.Fatalf("ansible's answer for %s is not base64 (%v):\n%s", name, err, out)
	}
	return string(plain)
}

func TestE2E_Vault_EditChangesOnlyTheEditedValues(t *testing.T) {
	requireE2ETools(t)
	requireAnsibleVault(t)
	dir := vaultFixture(t)
	original, _ := os.ReadFile(filepath.Join(dir, "vault.yml"))

	code, out := runVault(t, dir,
		`sed -i.bak -e 's/^db_password: .*/db_password: rotated secret/' -e 's/^region: .*/region: eu-west-1/' "$1" && rm -f "$1.bak"`,
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

// A password that fits no value does not stop the editor: every value shows
// as a placeholder, nothing is changed unless something is added, and a new
// value is encrypted with the given password.
func TestE2E_Vault_WrongPasswordOpensWithPlaceholdersAndAddsEncryptedValues(t *testing.T) {
	requireE2ETools(t)
	dir := vaultFixture(t)
	original, _ := os.ReadFile(filepath.Join(dir, "vault.yml"))
	os.WriteFile(filepath.Join(dir, "wrong.txt"), []byte("not the password\n"), 0o600)

	code, out := runVault(t, dir, `cp "$1" seen.yml`, "vault.yml", "--vault-password-file", "wrong.txt")
	if code != 0 || !strings.Contains(out, "none of the values") {
		t.Errorf("exit %d:\n%s", code, out)
	}
	if seen, _ := os.ReadFile(filepath.Join(dir, "seen.yml")); !strings.Contains(string(seen), "<vaulted") {
		t.Errorf("the editor saw:\n%s", seen)
	}
	if after, _ := os.ReadFile(filepath.Join(dir, "vault.yml")); !bytes.Equal(after, original) {
		t.Error("the file changed although nothing was edited")
	}

	code, out = runVault(t, dir, `echo 'extra: secret value' >> "$1"`, "vault.yml", "--vault-password-file", "wrong.txt")
	if code != 0 {
		t.Fatalf("adding a value: exit %d:\n%s", code, out)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "vault.yml"))
	if !bytes.HasPrefix(after, original) || !strings.Contains(string(after), "extra: !vault |") {
		t.Errorf("the old values must stay and extra be encrypted:\n%s", after)
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
}

// A file that does not exist is created empty, an empty file opens without a
// complaint, and every variable typed into it is encrypted.
func TestE2E_Vault_NewAndEmptyFilesStartEmptyAndEncryptWhatIsAdded(t *testing.T) {
	requireE2ETools(t)
	requireAnsibleVault(t)
	dir := vaultFixture(t)
	args := []string{"--vault-password-file", "vault-password.txt"}

	// Nothing typed: the created file stays empty.
	if code, out := runVault(t, dir, "test ! -s \"$1\"", append([]string{"fresh.yml"}, args...)...); code != 0 || !strings.Contains(out, "created it") {
		t.Fatalf("new file, no edit: exit %d:\n%s", code, out)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "fresh.yml")); err != nil || len(data) != 0 {
		t.Fatalf("fresh.yml: %q, %v", data, err)
	}

	// Now it exists and is empty: add a variable.
	if code, out := runVault(t, dir, "echo 'api_key: hunter2' >> \"$1\"", append([]string{"fresh.yml"}, args...)...); code != 0 {
		t.Fatalf("empty file: exit %d:\n%s", code, out)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "fresh.yml"))
	if !strings.HasPrefix(string(data), "api_key: !vault |") || strings.Contains(string(data), "hunter2") {
		t.Errorf("api_key was not encrypted:\n%s", data)
	}
	if out := ansibleValue(t, dir, "fresh.yml", "api_key"); !strings.Contains(out, "hunter2") {
		t.Errorf("ansible reads %q", out)
	}
}

// multiIDFixture writes a vars file with one secret per vault id, made by the
// real ansible-vault (dev and prod, each with its own password file), plus an
// unlabeled plain value.
func multiIDFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	var file strings.Builder
	for _, id := range []string{"dev", "prod"} {
		if err := os.WriteFile(filepath.Join(dir, id+".pw"), []byte(id+"-password\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("ansible-vault", "encrypt_string", "--vault-id", id+"@"+id+".pw", "--encrypt-vault-id", id, "--name", id+"_secret", id+" original")
		cmd.Dir = dir
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("ansible-vault encrypt_string: %v", err)
		}
		file.Write(out)
	}
	file.WriteString("region: eu\n")
	if err := os.WriteFile(filepath.Join(dir, "vars.yml"), []byte(file.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// ansibleMultiValue asks the real ansible for name in vars.yml with both
// passwords.
func ansibleMultiValue(t *testing.T, dir, name string) string {
	t.Helper()
	cmd := exec.Command("ansible", "localhost", "-i", "localhost,", "-c", "local", "-m", "debug", "-a", "msg={{ "+name+" | b64encode }}",
		"-e", "@vars.yml", "--vault-id", "dev@dev.pw", "--vault-id", "prod@prod.pw")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ansible could not read %s: %v\n%s", name, err, out)
	}
	return decodeAnsibleMsg(t, name, out)
}

func TestE2E_Vault_TwoVaultIDsKeepTheirLabelsAndNewValuesUseEncryptVaultID(t *testing.T) {
	requireE2ETools(t)
	requireAnsibleVault(t)
	dir := multiIDFixture(t)

	code, out := runVault(t, dir,
		`sed -i.bak -e 's/^dev_secret: .*/dev_secret: dev rotated/' -e 's/^prod_secret: .*/prod_secret: prod rotated/' "$1" && rm -f "$1.bak"; echo 'fresh: brand new' >> "$1"`,
		"vars.yml", "--vault-id", "dev@dev.pw", "--vault-id", "prod@prod.pw", "--encrypt-vault-id", "prod")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	saved, _ := os.ReadFile(filepath.Join(dir, "vars.yml"))
	for _, want := range []string{"$ANSIBLE_VAULT;1.2;AES256;dev", "$ANSIBLE_VAULT;1.2;AES256;prod"} {
		if !strings.Contains(string(saved), want) {
			t.Errorf("the saved file lost %q:\n%s", want, saved)
		}
	}
	if strings.Count(string(saved), "AES256;prod") != 2 {
		t.Errorf("want prod_secret and the new value under prod:\n%s", saved)
	}
	for name, want := range map[string]string{"dev_secret": "dev rotated", "prod_secret": "prod rotated", "fresh": "brand new"} {
		if got := ansibleMultiValue(t, dir, name); !strings.Contains(got, want) {
			t.Errorf("ansible reads %s as:\n%s", name, got)
		}
	}
}

func TestE2E_Vault_WithoutTheProdPasswordProdValuesStayLocked(t *testing.T) {
	requireE2ETools(t)
	requireAnsibleVault(t)
	dir := multiIDFixture(t)
	before, _ := os.ReadFile(filepath.Join(dir, "vars.yml"))
	prodBlock := func(b []byte) string {
		s := string(b)
		i := strings.Index(s, "prod_secret:")
		j := strings.Index(s, "region:")
		return s[i:j]
	}

	// The editor sees a placeholder, not the secret; change the dev value.
	code, out := runVault(t, dir,
		`grep -q '^prod_secret: .<vaulted: prod>.' "$1" || exit 3; sed -i.bak 's/^dev_secret: .*/dev_secret: dev changed/' "$1" && rm -f "$1.bak"`,
		"vars.yml", "--vault-id", "dev@dev.pw")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if !strings.Contains(out, "placeholders") {
		t.Errorf("no note about the locked value:\n%s", out)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "vars.yml"))
	if prodBlock(after) != prodBlock(before) {
		t.Errorf("the prod value was rewritten:\nbefore:\n%s\nafter:\n%s", prodBlock(before), prodBlock(after))
	}
	if got := ansibleMultiValue(t, dir, "dev_secret"); !strings.Contains(got, "dev changed") {
		t.Errorf("ansible reads dev_secret as:\n%s", got)
	}
	if got := ansibleMultiValue(t, dir, "prod_secret"); !strings.Contains(got, "prod original") {
		t.Errorf("ansible reads prod_secret as:\n%s", got)
	}

	// With no password that fits any value the file still opens, and
	// nothing changes unless something is added.
	before, _ = os.ReadFile(filepath.Join(dir, "vars.yml"))
	if err := os.WriteFile(filepath.Join(dir, "other.pw"), []byte("nope\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, out := runVault(t, dir, `true`, "vars.yml", "--vault-id", "other@other.pw"); code != 0 {
		t.Errorf("a password that fits nothing: exit %d\n%s", code, out)
	}
	if now, _ := os.ReadFile(filepath.Join(dir, "vars.yml")); string(now) != string(before) {
		t.Error("the file changed although nothing was edited")
	}
}

func TestE2E_Vault_IdentityListFromAnsibleCfg(t *testing.T) {
	requireE2ETools(t)
	requireAnsibleVault(t)
	dir := multiIDFixture(t)
	cfg := "[defaults]\nvault_identity_list = dev@dev.pw, prod@prod.pw\nvault_encrypt_identity = dev\n"
	if err := os.WriteFile(filepath.Join(dir, "ansible.cfg"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	// No password option on the command line at all.
	code, out := runVault(t, dir, `sed -i.bak 's/^prod_secret: .*/prod_secret: via config/' "$1" && rm -f "$1.bak"; echo 'added: by default id' >> "$1"`, "vars.yml")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	saved, _ := os.ReadFile(filepath.Join(dir, "vars.yml"))
	if !strings.Contains(string(saved), "AES256;prod") {
		t.Errorf("prod_secret lost its vault id:\n%s", saved)
	}
	if got := ansibleMultiValue(t, dir, "prod_secret"); !strings.Contains(got, "via config") {
		t.Errorf("ansible reads prod_secret as:\n%s", got)
	}
	// The new value took vault_encrypt_identity = dev.
	if strings.Count(string(saved), "AES256;dev") != 2 {
		t.Errorf("want dev_secret and the new value under dev:\n%s", saved)
	}
}

// TestE2E_Vault_PromptsForEachVaultID drives the real terminal prompts
// (term.ReadPassword needs a TTY, so this one runs in tmux): one prompt per
// "@prompt" vault id in the order given, naming the id, the typed password not
// echoed; then --ask-vault-pass's plain wording and the error for a password
// that opens nothing.
func TestE2E_Vault_PromptsForEachVaultID(t *testing.T) {
	requireE2ETools(t)
	requireAnsibleVault(t)
	dir := multiIDFixture(t)
	bin := buildE2EBinary(t)
	editor := filepath.Join(t.TempDir(), "editor.sh")
	script := "#!/bin/sh\nsed -i.bak -e 's/^dev_secret: .*/dev_secret: dev prompted/' -e 's/^prod_secret: .*/prod_secret: prod prompted/' \"$1\" && rm -f \"$1.bak\"\n"
	if err := os.WriteFile(editor, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	s := startTmuxSession(t)
	run := func(args string) {
		s.send(fmt.Sprintf("cd %s && EDITOR=%s VISUAL= ANSIBLE_VAULT_PASSWORD_FILE= %s vault vars.yml %s; echo DONE-$?",
			shellQuote(dir), shellQuote(editor), shellQuote(bin), args), "Enter")
	}

	run("--vault-id dev@prompt --vault-id prod@prompt")
	s.waitFor("Vault password (dev):", 15*time.Second)
	s.send("dev-password", "Enter")
	// The second prompt only appears once the first was answered, so it
	// also shows the order.
	got := s.waitFor("Vault password (prod):", 15*time.Second)
	if strings.Contains(got, "dev-password") {
		t.Errorf("the typed password was echoed:\n%s", got)
	}
	s.send("prod-password", "Enter")
	got = s.waitFor("DONE-0", 20*time.Second)
	if strings.Contains(got, "prod-password") {
		t.Errorf("the typed password was echoed:\n%s", got)
	}
	for name, want := range map[string]string{"dev_secret": "dev prompted", "prod_secret": "prod prompted"} {
		if v := ansibleMultiValue(t, dir, name); !strings.Contains(v, want) {
			t.Errorf("ansible reads %s as:\n%s", name, v)
		}
	}

	// --ask-vault-pass keeps the plain wording; a password that opens no
	// value gives a note. The test's editor changes the (now locked) values:
	// that is refused, and reverting leaves the file as it was.
	before, _ := os.ReadFile(filepath.Join(dir, "vars.yml"))
	s.send("clear", "Enter")
	run("--ask-vault-pass")
	s.waitFor("Vault password:", 15*time.Second)
	s.send("not-a-password", "Enter")
	got = s.waitFor("[f/r]", 20*time.Second)
	if !strings.Contains(got, "none of the values") {
		t.Errorf("expected the note that nothing could be decrypted:\n%s", got)
	}
	s.send("r", "Enter")
	s.waitFor("reverted", 20*time.Second)
	if now, _ := os.ReadFile(filepath.Join(dir, "vars.yml")); string(now) != string(before) {
		t.Error("the file changed although no password fit")
	}
}
