package vaultcrypto

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestEncryptDecryptRoundTrip is the inverse-pair test this package's
// correctness hinges on, in the same spirit as rerunargs_test.go's
// ParsePassthroughArgs<->Reassemble round-trip: Encrypt then Decrypt must
// always reproduce the original plaintext. The multi-block case exists
// specifically to exercise the PKCS7 padding boundary (see pkcs7Pad's own
// doc comment on why padding is mandatory here, not optional).
func TestEncryptDecryptRoundTrip(t *testing.T) {
	cases := []struct {
		name      string
		plaintext string
	}{
		{"empty string", ""},
		{"short ascii", "hunter2"},
		{"unicode", "pässwört 日本語 🔒"},
		{"embedded newlines", "line one\nline two\nline three\n"},
		{"exact one block (16 bytes)", "0123456789abcdef"},
		{"spans multiple blocks", strings.Repeat("x", 100)},
	}
	const password = "correct horse battery staple"

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			vaultString, err := Encrypt(c.plaintext, password)
			if err != nil {
				t.Fatalf("Encrypt: %v", err)
			}
			if !strings.HasPrefix(vaultString, Header) {
				t.Fatalf("Encrypt output doesn't start with %q:\n%s", Header, vaultString)
			}
			got, err := Decrypt(vaultString, password)
			if err != nil {
				t.Fatalf("Decrypt: %v", err)
			}
			if got != c.plaintext {
				t.Fatalf("round-trip mismatch: got %q, want %q", got, c.plaintext)
			}
		})
	}
}

// TestEncrypt_FreshSaltEveryCall guards against the exact regression that
// would defeat design-docs/Vault.md's whole "salt problem" mitigation: if
// Encrypt ever reused a salt (or, worse, produced identical ciphertext for
// identical input), the diff/reassembly logic built on top of this package
// would have no reliable way to tell "unchanged" from "changed".
func TestEncrypt_FreshSaltEveryCall(t *testing.T) {
	a, err := Encrypt("same plaintext", "same password")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	b, err := Encrypt("same plaintext", "same password")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if a == b {
		t.Fatal("two encryptions of identical plaintext produced identical ciphertext - salt is not being randomized")
	}
}

func TestDecrypt_WrongPassword(t *testing.T) {
	vaultString, err := Encrypt("a secret value", "the right password")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	_, err = Decrypt(vaultString, "the wrong password")
	if err != ErrHMACMismatch {
		t.Fatalf("Decrypt with wrong password: got err %v, want ErrHMACMismatch", err)
	}
}

func TestDecrypt_CorruptedCiphertext(t *testing.T) {
	vaultString, err := Encrypt("a secret value", "a password")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	corrupted := strings.Replace(vaultString, "a", "b", 1)
	if corrupted == vaultString {
		t.Skip("fixture didn't contain a replaceable byte")
	}
	_, err = Decrypt(corrupted, "a password")
	if err == nil {
		t.Fatal("Decrypt of corrupted ciphertext unexpectedly succeeded")
	}
}

// TestDecrypt_GoldenFixture proves interop with real ansible-vault output,
// not just with itself: golden_1.vault was produced by a real
// `ansible-vault encrypt_string` invocation (see testdata/README - the
// generating command is recorded there), independent of this package.
func TestDecrypt_GoldenFixture(t *testing.T) {
	password := readTestdataFile(t, "golden_password.txt")
	vaultString := readTestdataFile(t, "golden_1.vault")
	wantPlaintext := readTestdataFile(t, "golden_1.plaintext.txt")

	got, err := Decrypt(vaultString, password)
	if err != nil {
		t.Fatalf("Decrypt(golden fixture): %v", err)
	}
	if got != wantPlaintext {
		t.Fatalf("Decrypt(golden fixture) = %q, want %q", got, wantPlaintext)
	}
}

// TestEncrypt_RealAnsibleVaultInterop is the complementary direction: this
// package's own output, decrypted by the real ansible-vault binary. Skips
// (doesn't fail) if ansible-vault isn't installed, matching
// e2e/rerun_test.go's requireE2ETools convention for optional external
// tooling.
func TestEncrypt_RealAnsibleVaultInterop(t *testing.T) {
	binary, err := exec.LookPath("ansible-vault")
	if err != nil {
		t.Skip("ansible-vault not found in PATH, skipping real-binary interop check")
	}

	const password = "interop-check-password"
	const plaintext = "round trip through the real ansible-vault binary"

	vaultString, err := Encrypt(plaintext, password)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	dir := t.TempDir()
	passwordFile := filepath.Join(dir, "password.txt")
	if err := os.WriteFile(passwordFile, []byte(password), 0o600); err != nil {
		t.Fatalf("writing password file: %v", err)
	}
	vaultFile := filepath.Join(dir, "secret.vault")
	if err := os.WriteFile(vaultFile, []byte(vaultString), 0o600); err != nil {
		t.Fatalf("writing vault file: %v", err)
	}

	cmd := exec.Command(binary, "decrypt", vaultFile, "--vault-password-file", passwordFile, "--output", "-")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ansible-vault decrypt failed: %v\noutput:\n%s", err, out)
	}
	if got := string(out); got != plaintext {
		t.Fatalf("ansible-vault decrypted our output to %q, want %q", got, plaintext)
	}
}

func readTestdataFile(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading testdata/%s: %v", name, err)
	}
	return string(data)
}

func TestLabelAndHeaderFor(t *testing.T) {
	cases := []struct {
		header  string
		label   string
		wantErr bool
	}{
		{"$ANSIBLE_VAULT;1.1;AES256", "", false},
		{"$ANSIBLE_VAULT;1.2;AES256;prod", "prod", false},
		{"$ANSIBLE_VAULT;1.2;AES256", "", false},
		{"$ANSIBLE_VAULT;1.1;AES256;prod", "", true}, // 1.1 has no label field
		{"$ANSIBLE_VAULT;1.3;AES256;prod", "", true},
		{"$ANSIBLE_VAULT;1.2;AES128;prod", "", true},
		{"nonsense", "", true},
	}
	for _, c := range cases {
		got, err := Label(c.header + "\n0a0b")
		if (err != nil) != c.wantErr || got != c.label {
			t.Errorf("Label(%q) = %q, %v; want %q, error %v", c.header, got, err, c.label, c.wantErr)
		}
	}
	for label, want := range map[string]string{
		"": Header, DefaultID: Header, "prod": "$ANSIBLE_VAULT;1.2;AES256;prod",
	} {
		if got := HeaderFor(label); got != want {
			t.Errorf("HeaderFor(%q) = %q, want %q", label, got, want)
		}
	}
}

func TestEncryptLabeledRoundTrip(t *testing.T) {
	v, err := EncryptLabeled("s3cret", "pw", "prod")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(v, "$ANSIBLE_VAULT;1.2;AES256;prod\n") {
		t.Fatalf("header: %q", strings.SplitN(v, "\n", 2)[0])
	}
	if got, err := Decrypt(v, "pw"); err != nil || got != "s3cret" {
		t.Fatalf("Decrypt = %q, %v", got, err)
	}
	if l, _ := Label(v); l != "prod" {
		t.Errorf("Label = %q", l)
	}
	v, _ = EncryptLabeled("x", "pw", DefaultID)
	if !strings.HasPrefix(v, Header+"\n") {
		t.Errorf("the default id must give the unlabeled header, got %q", strings.SplitN(v, "\n", 2)[0])
	}
}

func TestDecryptWith(t *testing.T) {
	dev := Secret{ID: "dev", Password: "dev-pw"}
	prod := Secret{ID: "prod", Password: "prod-pw"}
	other := Secret{ID: "default", Password: "prod-pw"} // right password, wrong label
	prodValue, _ := EncryptLabeled("p", prod.Password, "prod")
	plainValue, _ := Encrypt("u", dev.Password)

	if got, used, err := DecryptWith(prodValue, []Secret{dev, prod}); err != nil || got != "p" || used != prod {
		t.Errorf("matching label: %q, %v, %v", got, used, err)
	}
	// The label is a hint, not a check: the right password under another
	// label still decrypts.
	if got, used, err := DecryptWith(prodValue, []Secret{dev, other}); err != nil || got != "p" || used != other {
		t.Errorf("wrong label, right password: %q, %v, %v", got, used, err)
	}
	// An unlabeled value is tried against every password.
	if got, used, err := DecryptWith(plainValue, []Secret{prod, dev}); err != nil || got != "u" || used != dev {
		t.Errorf("unlabeled: %q, %v, %v", got, used, err)
	}
	if _, _, err := DecryptWith(prodValue, []Secret{dev}); !errors.Is(err, ErrHMACMismatch) {
		t.Errorf("no password fits: %v", err)
	}
	if _, _, err := DecryptWith(prodValue, nil); !errors.Is(err, ErrNoPassword) {
		t.Errorf("no secrets: %v", err)
	}
	// The matching label is tried first: with two secrets of that id the
	// order given decides, and a wrong one is skipped.
	both := []Secret{{ID: "prod", Password: "nope"}, prod}
	if got, _, err := DecryptWith(prodValue, both); err != nil || got != "p" {
		t.Errorf("two secrets of one id: %q, %v", got, err)
	}
}

// Real ansible-vault, both directions, for a labeled value.
func TestLabeledRealAnsibleVaultInterop(t *testing.T) {
	binary, err := exec.LookPath("ansible-vault")
	if err != nil {
		t.Skip("ansible-vault not found in PATH")
	}
	dir := t.TempDir()
	pwFile := filepath.Join(dir, "pw")
	if err := os.WriteFile(pwFile, []byte("label-pw\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// ours -> theirs
	ours, err := EncryptLabeled("from tangsible", "label-pw", "prod")
	if err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(dir, "ours.vault")
	if err := os.WriteFile(f, []byte(ours), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(binary, "decrypt", f, "--vault-id", "prod@"+pwFile, "--output", "-").CombinedOutput()
	if err != nil || string(out) != "from tangsible" {
		t.Fatalf("ansible-vault decrypting our labeled value: %v\n%s", err, out)
	}

	// theirs -> ours
	cmd := exec.Command(binary, "encrypt_string", "--vault-id", "prod@"+pwFile, "--encrypt-vault-id", "prod", "from ansible")
	enc, err := cmd.Output()
	if err != nil {
		t.Fatalf("ansible-vault encrypt_string: %v", err)
	}
	// The output is "!vault |\n          $ANSIBLE_VAULT;1.2;AES256;prod\n ...".
	var body []string
	for _, l := range strings.Split(string(enc), "\n")[1:] {
		body = append(body, strings.TrimSpace(l))
	}
	value := strings.Join(body, "\n")
	if l, err := Label(value); err != nil || l != "prod" {
		t.Fatalf("Label of ansible's value = %q, %v\n%s", l, err, value)
	}
	if got, _, err := DecryptWith(value, []Secret{{ID: "prod", Password: "label-pw"}}); err != nil || got != "from ansible" {
		t.Fatalf("decrypting ansible's labeled value: %q, %v", got, err)
	}
}
