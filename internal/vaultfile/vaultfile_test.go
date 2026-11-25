package vaultfile

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"code.aw.net/claude/tangsible/internal/vaultcrypto"
)

func readTestdataFile(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		t.Fatalf("reading testdata/%s: %v", name, err)
	}
	return data
}

func testPassword(t *testing.T) string {
	return strings.TrimSpace(string(readTestdataFile(t, "vault-password.txt")))
}

func TestBuildDecryptedView_RealFixture(t *testing.T) {
	src := readTestdataFile(t, "vault.yml")
	view, err := BuildDecryptedView(src, one(testPassword(t)))
	if err != nil {
		t.Fatalf("BuildDecryptedView: %v", err)
	}

	wantVaulted := map[string]string{
		"db_password": "a top secret db password",
		"api_token":   "another secret, an api token",
	}
	for key, want := range wantVaulted {
		got, ok := view.VaultedBefore[key]
		if !ok {
			t.Errorf("VaultedBefore missing key %q", key)
			continue
		}
		if got != want {
			t.Errorf("VaultedBefore[%q] = %q, want %q", key, got, want)
		}
		if view.SourceContent[key] == "" {
			t.Errorf("SourceContent missing key %q", key)
		}
	}

	if !view.PresentBefore["region"] {
		t.Error("PresentBefore should include the plaintext key 'region'")
	}
	if view.PresentBefore["db_password"] != true {
		t.Error("PresentBefore should also include vaulted keys")
	}

	// The plaintext view must show real plaintext, not any leftover
	// !vault tag or ciphertext.
	if strings.Contains(view.PlaintextYAML, "!vault") {
		t.Errorf("plaintext view still contains a !vault tag:\n%s", view.PlaintextYAML)
	}
	if strings.Contains(view.PlaintextYAML, "ANSIBLE_VAULT") {
		t.Errorf("plaintext view still contains ciphertext:\n%s", view.PlaintextYAML)
	}
	if !strings.Contains(view.PlaintextYAML, "a top secret db password") {
		t.Errorf("plaintext view doesn't show the decrypted db_password value:\n%s", view.PlaintextYAML)
	}
	if !strings.Contains(view.PlaintextYAML, "region: us-east-1") {
		t.Errorf("plaintext view doesn't preserve the plaintext region key:\n%s", view.PlaintextYAML)
	}
	// The fixture's own leading comment block should survive the
	// decode->mutate->encode round-trip (yaml.v3 comment preservation).
	if !strings.Contains(view.PlaintextYAML, "Fixture for internal/vaultfile's tests") {
		t.Errorf("plaintext view lost the source file's comment:\n%s", view.PlaintextYAML)
	}
}

func TestBuildDecryptedView_WrongPasswordLocksEveryValue(t *testing.T) {
	src := readTestdataFile(t, "vault.yml")
	view, err := BuildDecryptedView(src, one("definitely the wrong password"))
	if err != nil {
		t.Fatalf("a password that fits nothing must open the file with placeholders: %v", err)
	}
	if len(view.VaultedBefore) != 0 || len(view.LockedBefore) == 0 {
		t.Errorf("decrypted %d, locked %d", len(view.VaultedBefore), len(view.LockedBefore))
	}
}

func TestBuildDecryptedView_RejectsNestedVault(t *testing.T) {
	src := readTestdataFile(t, "vault-nested.yml")
	_, err := BuildDecryptedView(src, one(testPassword(t)))
	if err == nil {
		t.Fatal("expected BuildDecryptedView to fail loudly on a nested !vault value")
	}
}

func TestBuildDecryptedView_NotAMapping(t *testing.T) {
	_, err := BuildDecryptedView([]byte("- just\n- a\n- list\n"), one("password"))
	if !errors.Is(err, ErrInvalidYAML) {
		t.Fatalf("got err %v, want ErrInvalidYAML", err)
	}
}

func TestBuildDecryptedView_Unparseable(t *testing.T) {
	_, err := BuildDecryptedView([]byte("this: [is not, valid yaml"), one("password"))
	if !errors.Is(err, ErrInvalidYAML) {
		t.Fatalf("got err %v, want ErrInvalidYAML", err)
	}
}

// TestReassemble_UnchangedSplicesOriginalCiphertext is the test that
// actually catches a salt-avoidance regression: a naive "decrypt
// everything, re-encrypt everything" implementation would still pass a
// decrypt-based equality check, but would fail this one, since it
// compares the raw !vault node's own Value (the ciphertext itself), not
// just what it decrypts back to.
func TestReassemble_UnchangedSplicesOriginalCiphertext(t *testing.T) {
	src := readTestdataFile(t, "vault.yml")
	password := testPassword(t)
	view, err := BuildDecryptedView(src, one(password))
	if err != nil {
		t.Fatalf("BuildDecryptedView: %v", err)
	}

	// Edit nothing - reassemble the exact plaintext view unchanged.
	result, err := Reassemble(view, []byte(view.PlaintextYAML), newOne(password))
	if err != nil {
		t.Fatalf("Reassemble: %v", err)
	}
	if len(result.Problems) != 0 {
		t.Fatalf("unexpected Problems: %+v", result.Problems)
	}
	if !result.Unchanged {
		t.Error("Unchanged should be true when nothing was edited")
	}

	reassembledView, err := BuildDecryptedView([]byte(result.YAML), one(password))
	if err != nil {
		t.Fatalf("BuildDecryptedView(reassembled): %v", err)
	}

	origRoot := mustRootMapping(t, src)
	newRoot := mustRootMapping(t, []byte(result.YAML))
	_, origContent, _ := keyContentSpans(origRoot, strings.Split(string(src), "\n"))
	for _, key := range []string{"db_password", "api_token"} {
		origCiphertext := mappingScalarValue(t, origRoot, key)
		newCiphertext := mappingScalarValue(t, newRoot, key)
		if origCiphertext != newCiphertext {
			t.Errorf("%q ciphertext changed even though it was never edited:\nbefore: %q\nafter:  %q", key, origCiphertext, newCiphertext)
		}
		if reassembledView.VaultedBefore[key] != view.VaultedBefore[key] {
			t.Errorf("%q decrypted value changed even though it was never edited", key)
		}
		// Stronger than the two checks above: the raw *text* (indentation
		// included), not just the parsed value, must be byte-identical -
		// this is what actually keeps a diff minimal.
		if !strings.Contains(result.YAML, origContent[key]) {
			t.Errorf("%q's raw source text isn't byte-identical in the output", key)
		}
	}
}

// TestReassemble_ChangedValueGetsFreshCiphertext confirms a changed value
// is re-encrypted with a fresh salt every time - if a bug ever reused the
// salt, two saves of the same new plaintext would look identical, which
// this test would catch.
func TestReassemble_ChangedValueGetsFreshCiphertext(t *testing.T) {
	src := readTestdataFile(t, "vault.yml")
	password := testPassword(t)
	view, err := BuildDecryptedView(src, one(password))
	if err != nil {
		t.Fatalf("BuildDecryptedView: %v", err)
	}

	edited := strings.Replace(view.PlaintextYAML, "a top secret db password", "a brand new db password", 1)

	result1, err := Reassemble(view, []byte(edited), newOne(password))
	if err != nil {
		t.Fatalf("Reassemble (1): %v", err)
	}
	result2, err := Reassemble(view, []byte(edited), newOne(password))
	if err != nil {
		t.Fatalf("Reassemble (2): %v", err)
	}
	if result1.Unchanged {
		t.Error("Unchanged should be false when a value was actually edited")
	}

	root1 := mustRootMapping(t, []byte(result1.YAML))
	root2 := mustRootMapping(t, []byte(result2.YAML))
	c1 := mappingScalarValue(t, root1, "db_password")
	c2 := mappingScalarValue(t, root2, "db_password")
	if c1 == c2 {
		t.Fatal("two re-encryptions of the same new plaintext produced identical ciphertext - salt is not being randomized on re-encrypt")
	}

	decrypted1, err := BuildDecryptedView([]byte(result1.YAML), one(password))
	if err != nil {
		t.Fatalf("BuildDecryptedView(result1): %v", err)
	}
	if decrypted1.VaultedBefore["db_password"] != "a brand new db password" {
		t.Errorf("re-encrypted value doesn't decrypt back to the edited plaintext: got %q", decrypted1.VaultedBefore["db_password"])
	}

	// api_token was never touched - must still be preserved verbatim.
	origRoot := mustRootMapping(t, src)
	if mappingScalarValue(t, origRoot, "api_token") != mappingScalarValue(t, root1, "api_token") {
		t.Error("api_token's ciphertext changed even though only db_password was edited")
	}
}

func TestReassemble_NewKeyGetsEncrypted(t *testing.T) {
	src := readTestdataFile(t, "vault.yml")
	password := testPassword(t)
	view, err := BuildDecryptedView(src, one(password))
	if err != nil {
		t.Fatalf("BuildDecryptedView: %v", err)
	}

	edited := view.PlaintextYAML + "new_secret: a brand new secret value\n"
	result, err := Reassemble(view, []byte(edited), newOne(password))
	if err != nil {
		t.Fatalf("Reassemble: %v", err)
	}
	if len(result.Problems) != 0 {
		t.Fatalf("unexpected Problems: %+v", result.Problems)
	}

	root := mustRootMapping(t, []byte(result.YAML))
	valNode := mappingValueNode(t, root, "new_secret")
	if valNode.Tag != vaultTag {
		t.Errorf("new_secret should have been encrypted (tag=%q)", valNode.Tag)
	}

	decrypted, err := BuildDecryptedView([]byte(result.YAML), one(password))
	if err != nil {
		t.Fatalf("BuildDecryptedView(result): %v", err)
	}
	if decrypted.VaultedBefore["new_secret"] != "a brand new secret value" {
		t.Errorf("new_secret decrypts to %q, want the value just added", decrypted.VaultedBefore["new_secret"])
	}
}

// TestReassemble_CommentBeforeNewKeySurvives is the direct regression test
// for a real bug reported from live use: a "# comment" line added directly
// above a brand-new key was silently dropped. Root cause: the comment sat
// at the *tail* of the previous (untouched, api_token) key's own span in
// the edited file, and keyContentSpans classified any non-blank trailing
// line as that key's own *content* - which is discarded entirely for a
// spliced key, since a spliced key's output comes from the *original*
// file's content, not the edited one. Only that key's *gap* (also
// edited-side, but always used regardless of decision) survives - so the
// fix is recognizing a top-level "#" line as gap, not content.
func TestReassemble_CommentBeforeNewKeySurvives(t *testing.T) {
	src := readTestdataFile(t, "vault.yml")
	password := testPassword(t)
	view, err := BuildDecryptedView(src, one(password))
	if err != nil {
		t.Fatalf("BuildDecryptedView: %v", err)
	}

	// api_token (the last key in the plaintext view) is left untouched -
	// it must be spliced - while a comment plus a new key are appended
	// directly after it, with no blank line in between, matching exactly
	// what a user typing in a real editor would produce.
	edited := strings.TrimRight(view.PlaintextYAML, "\n") + "\n# will this work?\ntesting: bla\n"
	result, err := Reassemble(view, []byte(edited), newOne(password))
	if err != nil {
		t.Fatalf("Reassemble: %v", err)
	}
	if len(result.Problems) != 0 {
		t.Fatalf("unexpected Problems: %+v", result.Problems)
	}

	if !strings.Contains(result.YAML, "# will this work?") {
		t.Errorf("comment was dropped from the output:\n%s", result.YAML)
	}
	// It must also end up in the right place: directly above testing's
	// own key line, not orphaned somewhere else.
	lines := strings.Split(result.YAML, "\n")
	for i, l := range lines {
		if l == "# will this work?" {
			if i+1 >= len(lines) || !strings.HasPrefix(lines[i+1], "testing:") {
				t.Errorf("comment survived but isn't directly above 'testing:':\n%s", result.YAML)
			}
			return
		}
	}
	t.Fatal("comment line not found at all")
}

func TestReassemble_ExistingPlaintextStaysPlaintextWithWarning(t *testing.T) {
	src := readTestdataFile(t, "vault.yml")
	password := testPassword(t)
	view, err := BuildDecryptedView(src, one(password))
	if err != nil {
		t.Fatalf("BuildDecryptedView: %v", err)
	}

	edited := strings.Replace(view.PlaintextYAML, "region: us-east-1", "region: eu-west-1", 1)
	result, err := Reassemble(view, []byte(edited), newOne(password))
	if err != nil {
		t.Fatalf("Reassemble: %v", err)
	}
	if len(result.Problems) != 0 {
		t.Fatalf("unexpected Problems: %+v", result.Problems)
	}

	root := mustRootMapping(t, []byte(result.YAML))
	valNode := mappingValueNode(t, root, "region")
	if valNode.Tag == vaultTag {
		t.Error("an already-plaintext key must never be auto-encrypted")
	}
	if valNode.Value != "eu-west-1" {
		t.Errorf("region = %q, want the edited value", valNode.Value)
	}

	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "region") {
		t.Errorf("expected exactly one warning mentioning 'region', got %v", result.Warnings)
	}
}

func TestReassemble_TypeChangeIsAProblemNotSilent(t *testing.T) {
	src := readTestdataFile(t, "vault.yml")
	password := testPassword(t)
	view, err := BuildDecryptedView(src, one(password))
	if err != nil {
		t.Fatalf("BuildDecryptedView: %v", err)
	}

	edited := strings.Replace(view.PlaintextYAML, "db_password: a top secret db password", "db_password: 12345", 1)
	result, err := Reassemble(view, []byte(edited), newOne(password))
	if err != nil {
		t.Fatalf("Reassemble: %v", err)
	}
	if result.YAML != "" {
		t.Error("YAML must be empty when there are Problems - nothing should be written")
	}
	if len(result.Problems) != 1 {
		t.Fatalf("expected exactly one Problem, got %+v", result.Problems)
	}
	if result.Problems[0].Key != "db_password" {
		t.Errorf("Problem.Key = %q, want db_password", result.Problems[0].Key)
	}
	if result.Problems[0].Line <= 0 {
		t.Errorf("Problem.Line = %d, want a positive line number", result.Problems[0].Line)
	}
}

func TestReassemble_HandTypedVaultTagIsAProblem(t *testing.T) {
	src := readTestdataFile(t, "vault.yml")
	password := testPassword(t)
	view, err := BuildDecryptedView(src, one(password))
	if err != nil {
		t.Fatalf("BuildDecryptedView: %v", err)
	}

	edited := view.PlaintextYAML + "hand_typed: !vault |\n  not a real vault block\n"
	result, err := Reassemble(view, []byte(edited), newOne(password))
	if err != nil {
		t.Fatalf("Reassemble: %v", err)
	}
	if len(result.Problems) != 1 || result.Problems[0].Key != "hand_typed" {
		t.Fatalf("expected exactly one Problem for hand_typed, got %+v", result.Problems)
	}
}

// TestReassemble_MultipleProblemsAnnotatedInOnePass is the "supporting
// multiple simultaneous annotated problems" requirement from
// design-docs/Vault.md point 5 - fixing problem #1 shouldn't just surface
// problem #2 on the very next save.
func TestReassemble_MultipleProblemsAnnotatedInOnePass(t *testing.T) {
	src := readTestdataFile(t, "vault.yml")
	password := testPassword(t)
	view, err := BuildDecryptedView(src, one(password))
	if err != nil {
		t.Fatalf("BuildDecryptedView: %v", err)
	}

	edited := view.PlaintextYAML
	edited = strings.Replace(edited, "db_password: a top secret db password", "db_password: 12345", 1)
	edited = strings.Replace(edited, "api_token: another secret, an api token", "api_token: [1, 2, 3]", 1)

	result, err := Reassemble(view, []byte(edited), newOne(password))
	if err != nil {
		t.Fatalf("Reassemble: %v", err)
	}
	if len(result.Problems) != 2 {
		t.Fatalf("expected 2 Problems, got %+v", result.Problems)
	}

	annotated := AnnotateProblems([]byte(edited), result.Problems)
	annotatedText := string(annotated)
	commentCount := strings.Count(annotatedText, "# TANGSIBLE VAULT:")
	if commentCount != 2 {
		t.Fatalf("expected 2 annotation comments, got %d:\n%s", commentCount, annotatedText)
	}

	// Both original lines must still be present and each annotation must
	// sit directly above the line it's about, with correct line numbers
	// even after an earlier (later-numbered) insertion.
	lines := strings.Split(annotatedText, "\n")
	for i, line := range lines {
		if strings.Contains(line, "# TANGSIBLE VAULT:") && strings.Contains(line, "db_password") {
			if i+1 >= len(lines) || !strings.Contains(lines[i+1], "db_password: 12345") {
				t.Errorf("db_password annotation isn't directly above its offending line:\n%s", annotatedText)
			}
		}
		if strings.Contains(line, "# TANGSIBLE VAULT:") && strings.Contains(line, "api_token") {
			if i+1 >= len(lines) || !strings.Contains(lines[i+1], "api_token: [1, 2, 3]") {
				t.Errorf("api_token annotation isn't directly above its offending line:\n%s", annotatedText)
			}
		}
	}
}

// TestStripAnnotations covers the two real bugs it exists to prevent: a
// stale annotation stacking a duplicate on top of itself if the same
// problem persists into another round, and a stale annotation lingering
// permanently in the file once its problem is actually fixed.
func TestStripAnnotations(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			"removes an annotation line",
			"region: us-east-1\n# TANGSIBLE VAULT: \"db_password\" was vault-encrypted before editing and must remain a plain string to be saved\ndb_password: 12345\n",
			"region: us-east-1\ndb_password: 12345\n",
		},
		{
			"removes multiple annotations",
			"# TANGSIBLE VAULT: a\nfoo: 1\n# TANGSIBLE VAULT: b\nbar: 2\n",
			"foo: 1\nbar: 2\n",
		},
		{
			"leaves a user's own comment alone",
			"# a genuine user comment\nfoo: bar\n",
			"# a genuine user comment\nfoo: bar\n",
		},
		{
			"no annotations present",
			"foo: bar\n",
			"foo: bar\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := string(StripAnnotations([]byte(c.input)))
			if got != c.want {
				t.Errorf("StripAnnotations() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestReassemble_KeyRemoved(t *testing.T) {
	src := readTestdataFile(t, "vault.yml")
	password := testPassword(t)
	view, err := BuildDecryptedView(src, one(password))
	if err != nil {
		t.Fatalf("BuildDecryptedView: %v", err)
	}

	lines := strings.Split(view.PlaintextYAML, "\n")
	var kept []string
	skipping := false
	for _, l := range lines {
		if strings.HasPrefix(l, "api_token:") {
			skipping = true
			continue
		}
		if skipping && (l == "" || strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t")) {
			continue
		}
		skipping = false
		kept = append(kept, l)
	}
	edited := strings.Join(kept, "\n")

	result, err := Reassemble(view, []byte(edited), newOne(password))
	if err != nil {
		t.Fatalf("Reassemble: %v", err)
	}
	if len(result.Problems) != 0 {
		t.Fatalf("unexpected Problems: %+v", result.Problems)
	}
	if strings.Contains(result.YAML, "api_token") {
		t.Errorf("removed key api_token should be gone entirely:\n%s", result.YAML)
	}
}

func TestReassemble_EditedContentUnparseable(t *testing.T) {
	src := readTestdataFile(t, "vault.yml")
	password := testPassword(t)
	view, err := BuildDecryptedView(src, one(password))
	if err != nil {
		t.Fatalf("BuildDecryptedView: %v", err)
	}

	_, err = Reassemble(view, []byte("this: [is not, valid"), newOne(password))
	if !errors.Is(err, ErrInvalidYAML) {
		t.Fatalf("got err %v, want ErrInvalidYAML", err)
	}
}

// TestReassemble_RealAnsibleVaultCanReadReencryptedValue proves the
// yaml.v3 re-encoding path (not just vaultcrypto.Encrypt in isolation)
// produces a !vault block the real ansible-vault CLI can still decrypt -
// i.e. the block-scalar tag/style/indentation this package emits is
// genuinely valid, not just self-consistent. Skips (doesn't fail) if
// ansible-vault isn't installed, matching e2e/rerun_test.go's
// requireE2ETools convention.
func TestReassemble_RealAnsibleVaultCanReadReencryptedValue(t *testing.T) {
	binary, err := exec.LookPath("ansible-vault")
	if err != nil {
		t.Skip("ansible-vault not found in PATH, skipping real-binary interop check")
	}

	src := readTestdataFile(t, "vault.yml")
	password := testPassword(t)
	view, err := BuildDecryptedView(src, one(password))
	if err != nil {
		t.Fatalf("BuildDecryptedView: %v", err)
	}

	edited := strings.Replace(view.PlaintextYAML, "a top secret db password", "verified via the real ansible-vault binary", 1)
	result, err := Reassemble(view, []byte(edited), newOne(password))
	if err != nil {
		t.Fatalf("Reassemble: %v", err)
	}
	if len(result.Problems) != 0 {
		t.Fatalf("unexpected Problems: %+v", result.Problems)
	}

	// result.YAML is a normal file with one key individually vaulted, not
	// a whole-file vault - `ansible-vault view`/`decrypt` only operate on
	// the latter, so extract just the re-encrypted key's own vault block
	// (header+body) and decrypt that in isolation, same technique
	// vaultcrypto's own interop test uses.
	newRoot := mustRootMapping(t, []byte(result.YAML))
	vaultBlock := mappingScalarValue(t, newRoot, "db_password")

	dir := t.TempDir()
	vaultFile := filepath.Join(dir, "db_password.vault")
	if err := os.WriteFile(vaultFile, []byte(vaultBlock), 0o600); err != nil {
		t.Fatalf("writing vault block file: %v", err)
	}
	passwordFile := filepath.Join(dir, "password.txt")
	if err := os.WriteFile(passwordFile, []byte(password), 0o600); err != nil {
		t.Fatalf("writing password file: %v", err)
	}

	cmd := exec.Command(binary, "decrypt", vaultFile, "--vault-password-file", passwordFile, "--output", "-")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ansible-vault decrypt failed: %v\noutput:\n%s", err, out)
	}
	if got := string(out); got != "verified via the real ansible-vault binary" {
		t.Errorf("ansible-vault decrypted our re-encrypted value to %q, want the edited plaintext", got)
	}
}

// TestReassemble_ReencryptedBlockUsesSourceIndentWidth confirms a freshly
// re-encrypted value's block is hand-formatted at the *actual* detected
// indent width, not yaml.v3's SetIndent-clamped approximation of it. Real
// ansible-vault's own encrypt_string convention is 10 spaces, one past
// the top of yaml.v3's usable [2, 9] range - formatVaultBlock sidesteps
// that entirely by not going through SetIndent at all (see its own doc
// comment), so this should now match exactly rather than settling for the
// closest achievable value.
func TestReassemble_ReencryptedBlockUsesSourceIndentWidth(t *testing.T) {
	src := readTestdataFile(t, "vault.yml") // uses real ansible-vault's own 10-space convention
	password := testPassword(t)
	view, err := BuildDecryptedView(src, one(password))
	if err != nil {
		t.Fatalf("BuildDecryptedView: %v", err)
	}
	if view.IndentWidth != 10 {
		t.Fatalf("IndentWidth = %d, want 10", view.IndentWidth)
	}

	edited := strings.Replace(view.PlaintextYAML, "a top secret db password", "a brand new db password", 1)
	result, err := Reassemble(view, []byte(edited), newOne(password))
	if err != nil {
		t.Fatalf("Reassemble: %v", err)
	}

	lines := strings.Split(result.YAML, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "db_password: !vault") && i+1 < len(lines) {
			got := len(lines[i+1]) - len(strings.TrimLeft(lines[i+1], " "))
			if got != 10 {
				t.Errorf("freshly re-encrypted block is indented %d spaces, want 10:\n%s", got, result.YAML)
			}
			return
		}
	}
	t.Fatal("no re-encrypted db_password block found in output")
}

// TestReassemble_UntouchedKeyIsByteIdenticalToSource is the direct
// regression test for a real bug reported from live use: adding one new
// key made every *other* vaulted block look completely rewritten in a
// diff, because Reassemble re-encoded the whole document through
// yaml.v3, which silently reformats indentation on every re-emission
// (see detectIndentWidth's doc comment). The ciphertext *value* was
// actually untouched - decrypting to the same plaintext - but that's not
// what a "minimal diff" (design-docs/Vault.md point 2) means: this
// asserts the untouched key's *raw source text*, byte for byte, survives
// unchanged in the output - not just something that decrypts the same.
func TestReassemble_UntouchedKeyIsByteIdenticalToSource(t *testing.T) {
	src := readTestdataFile(t, "vault.yml")
	password := testPassword(t)
	view, err := BuildDecryptedView(src, one(password))
	if err != nil {
		t.Fatalf("BuildDecryptedView: %v", err)
	}

	edited := view.PlaintextYAML + "new_key: a brand new secret\n"
	result, err := Reassemble(view, []byte(edited), newOne(password))
	if err != nil {
		t.Fatalf("Reassemble: %v", err)
	}
	if len(result.Problems) != 0 {
		t.Fatalf("unexpected Problems: %+v", result.Problems)
	}

	_, origContent, _ := keyContentSpans(mustRootMapping(t, src), strings.Split(string(src), "\n"))
	for _, key := range []string{"db_password", "api_token"} {
		if !strings.Contains(result.YAML, origContent[key]) {
			t.Errorf("%q's original raw text doesn't appear byte-for-byte in the result:\nwant substring:\n%s\ngot:\n%s", key, origContent[key], result.YAML)
		}
	}
}

func TestDetectIndentWidth(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want int
	}{
		{"ten space (real ansible-vault convention)", "foo: !vault |\n          line one\n          line two\n", 10},
		{"four space", "foo: !vault |\n    line one\n", 4},
		{"no block scalar at all", "foo: bar\nbaz: qux\n", defaultIndentWidth},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := detectIndentWidth(strings.Split(c.yaml, "\n"))
			if got != c.want {
				t.Errorf("detectIndentWidth() = %d, want %d", got, c.want)
			}
		})
	}
}

func mustRootMapping(t *testing.T, data []byte) *yaml.Node {
	t.Helper()
	_, root, _, err := parseTopLevelMapping(data)
	if err != nil {
		t.Fatalf("parseTopLevelMapping: %v", err)
	}
	return root
}

func mappingValueNode(t *testing.T, root *yaml.Node, key string) *yaml.Node {
	t.Helper()
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == key {
			return root.Content[i+1]
		}
	}
	t.Fatalf("key %q not found in mapping", key)
	return nil
}

func mappingScalarValue(t *testing.T, root *yaml.Node, key string) string {
	t.Helper()
	return mappingValueNode(t, root, key).Value
}

// layoutSource builds a vault file with blank lines and comments between
// its entries (the shape a hand-maintained group_vars file really has) and
// the decrypted view of it.
func layoutSource(t *testing.T, password string, secrets map[string]string) (src string, view DecryptedView) {
	t.Helper()
	block := func(key string) string {
		enc, err := vaultcrypto.Encrypt(secrets[key], password)
		if err != nil {
			t.Fatalf("Encrypt(%s): %v", key, err)
		}
		return strings.Join(formatVaultBlock(key, enc, 10), "\n")
	}
	src = "# header\n\nplain: one\n\n# note on a\n" + block("secret_a") +
		"\n\n# note on b\n" + block("secret_b") + "\n\nlast: 3\n"
	view, err := BuildDecryptedView([]byte(src), one(password))
	if err != nil {
		t.Fatalf("BuildDecryptedView: %v", err)
	}
	return src, view
}

// TestBlankLinesAndCommentsSurviveRoundTrip pins the layout guarantee: the
// editable view keeps blank lines and comments between entries, so an
// untouched file saves back byte-for-byte and an edit only changes the
// edited entry's own block.
func TestBlankLinesAndCommentsSurviveRoundTrip(t *testing.T) {
	password := testPassword(t)
	src, view := layoutSource(t, password, map[string]string{"secret_a": "alpha", "secret_b": "beta"})

	wantView := "# header\n\nplain: one\n\n# note on a\nsecret_a: alpha\n\n# note on b\nsecret_b: beta\n\nlast: 3\n"
	if view.PlaintextYAML != wantView {
		t.Errorf("editable view:\n%q\nwant:\n%q", view.PlaintextYAML, wantView)
	}

	result, err := Reassemble(view, []byte(view.PlaintextYAML), newOne(password))
	if err != nil {
		t.Fatalf("Reassemble (no edit): %v", err)
	}
	if !result.Unchanged || result.YAML != src {
		t.Errorf("unedited file did not save back byte-for-byte (Unchanged=%v):\n%q\nwant:\n%q", result.Unchanged, result.YAML, src)
	}

	edited := strings.Replace(view.PlaintextYAML, "secret_a: alpha", "secret_a: rotated", 1)
	result, err = Reassemble(view, []byte(edited), newOne(password))
	if err != nil {
		t.Fatalf("Reassemble (edit): %v", err)
	}
	if result.Unchanged {
		t.Error("Unchanged should be false after editing secret_a")
	}
	if strings.Count(result.YAML, "\n\n") != strings.Count(src, "\n\n") {
		t.Errorf("blank lines changed:\n%s", result.YAML)
	}
	for _, keep := range []string{"# header\n\nplain: one\n\n# note on a\n", "\n\n# note on b\n" + view.SourceContent["secret_b"] + "\n\nlast: 3\n"} {
		if !strings.Contains(result.YAML, keep) {
			t.Errorf("result lost untouched text %q:\n%s", keep, result.YAML)
		}
	}
}

// TestBuildDecryptedView_StringLookalikesStayQuoted: a secret that happens
// to read as a number, bool or multi-line text must come back from the
// editor as the same string, not as a different YAML type.
func TestBuildDecryptedView_StringLookalikesStayQuoted(t *testing.T) {
	password := testPassword(t)
	_, view := layoutSource(t, password, map[string]string{"secret_a": "12345", "secret_b": "line one\nline two"})

	for _, want := range []string{`secret_a: "12345"`, "secret_b: |"} {
		if !strings.Contains(view.PlaintextYAML, want) {
			t.Errorf("view missing %q:\n%s", want, view.PlaintextYAML)
		}
	}
	result, err := Reassemble(view, []byte(view.PlaintextYAML), newOne(password))
	if err != nil {
		t.Fatalf("Reassemble: %v", err)
	}
	if len(result.Problems) != 0 || !result.Unchanged {
		t.Errorf("unedited look-alike secrets should round-trip cleanly: problems=%v unchanged=%v", result.Problems, result.Unchanged)
	}
}

// one is a single unlabeled password as the secrets BuildDecryptedView takes.
func one(password string) []vaultcrypto.Secret {
	return []vaultcrypto.Secret{{ID: vaultcrypto.DefaultID, Password: password}}
}

// newOne is the password a brand-new value is encrypted with.
func newOne(password string) *vaultcrypto.Secret {
	return &vaultcrypto.Secret{ID: vaultcrypto.DefaultID, Password: password}
}

// multiIDSource builds a vault file with a value per vault id: one under
// "dev", one under "prod", one unlabeled (encrypted with the dev password).
func multiIDSource(t *testing.T) []byte {
	t.Helper()
	block := func(key, label, pw, plain string) string {
		v, err := vaultcrypto.EncryptLabeled(plain, pw, label)
		if err != nil {
			t.Fatal(err)
		}
		return formatVaultBlockText(key, v)
	}
	return []byte("# secrets\n" +
		block("dev_pass", "dev", "dev-pw", "d-secret") + "\n" +
		block("prod_pass", "prod", "prod-pw", "p-secret") + "\n" +
		block("old_pass", "", "dev-pw", "o-secret") + "\nplain: visible\n")
}

func formatVaultBlockText(key, vaultString string) string {
	return strings.Join(formatVaultBlock(key, vaultString, 2), "\n")
}

var (
	devSecret  = vaultcrypto.Secret{ID: "dev", Password: "dev-pw"}
	prodSecret = vaultcrypto.Secret{ID: "prod", Password: "prod-pw"}
)

func TestMultiID_BothPasswordsDecryptEverythingAndKeepLabels(t *testing.T) {
	src := multiIDSource(t)
	view, err := BuildDecryptedView(src, []vaultcrypto.Secret{devSecret, prodSecret})
	if err != nil {
		t.Fatal(err)
	}
	if len(view.LockedBefore) != 0 || view.VaultedBefore["prod_pass"] != "p-secret" || view.VaultedBefore["old_pass"] != "o-secret" {
		t.Fatalf("view: %+v", view)
	}
	if got := view.UsedBefore["prod_pass"]; got.Label != "prod" || got.Password != "prod-pw" {
		t.Errorf("UsedBefore[prod_pass] = %+v", got)
	}
	if got := view.UsedBefore["old_pass"]; got.Label != "" || got.Password != "dev-pw" {
		t.Errorf("UsedBefore[old_pass] = %+v", got)
	}

	// Changing the prod value re-encrypts it under prod, not under the
	// secret meant for new values.
	edited := strings.Replace(view.PlaintextYAML, "p-secret", "p-changed", 1)
	res, err := Reassemble(view, []byte(edited), &devSecret)
	if err != nil || len(res.Problems) != 0 {
		t.Fatalf("Reassemble: %v %+v", err, res.Problems)
	}
	again, err := BuildDecryptedView([]byte(res.YAML), []vaultcrypto.Secret{devSecret, prodSecret})
	if err != nil {
		t.Fatal(err)
	}
	if again.VaultedBefore["prod_pass"] != "p-changed" || again.UsedBefore["prod_pass"].Label != "prod" {
		t.Errorf("after the edit: %+v", again.UsedBefore["prod_pass"])
	}
	// And only that one changed: the others are byte-identical.
	for _, key := range []string{"dev_pass", "old_pass"} {
		if !strings.Contains(res.YAML, view.SourceContent[key]) {
			t.Errorf("%s was not spliced back unchanged", key)
		}
	}
}

func TestMultiID_LockedValueShowsPlaceholderAndSurvivesASave(t *testing.T) {
	src := multiIDSource(t)
	view, err := BuildDecryptedView(src, []vaultcrypto.Secret{devSecret})
	if err != nil {
		t.Fatal(err)
	}
	if view.LockedBefore["prod_pass"] != "prod" || len(view.LockedBefore) != 1 {
		t.Fatalf("LockedBefore = %v", view.LockedBefore)
	}
	if !strings.Contains(view.PlaintextYAML, "prod_pass: '<vaulted: prod>'") || strings.Contains(view.PlaintextYAML, "p-secret") {
		t.Fatalf("view:\n%s", view.PlaintextYAML)
	}
	if got := view.LockedLabels(); got["prod"] != 1 {
		t.Errorf("LockedLabels = %v", got)
	}

	// Edit a value we can decrypt; the locked one is left alone and must
	// come back byte for byte.
	edited := strings.Replace(view.PlaintextYAML, "d-secret", "d-changed", 1)
	res, err := Reassemble(view, []byte(edited), &devSecret)
	if err != nil || len(res.Problems) != 0 {
		t.Fatalf("Reassemble: %v %+v", err, res.Problems)
	}
	if !strings.Contains(res.YAML, view.SourceContent["prod_pass"]) {
		t.Errorf("the locked value was not spliced back unchanged:\n%s", res.YAML)
	}
	after, err := BuildDecryptedView([]byte(res.YAML), []vaultcrypto.Secret{devSecret, prodSecret})
	if err != nil || after.VaultedBefore["prod_pass"] != "p-secret" || after.VaultedBefore["dev_pass"] != "d-changed" {
		t.Errorf("round trip: %v %v", err, after.VaultedBefore)
	}
}

func TestMultiID_LockedValueCannotBeChangedButCanBeDeleted(t *testing.T) {
	view, err := BuildDecryptedView(multiIDSource(t), []vaultcrypto.Secret{devSecret})
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(view.PlaintextYAML, "<vaulted: prod>", "guess", 1)
	res, err := Reassemble(view, []byte(changed), &devSecret)
	if err != nil || len(res.Problems) != 1 || res.Problems[0].Key != "prod_pass" {
		t.Fatalf("changing a locked value: %v %+v", err, res.Problems)
	}

	var kept []string
	for _, l := range strings.Split(view.PlaintextYAML, "\n") {
		if !strings.HasPrefix(l, "prod_pass:") {
			kept = append(kept, l)
		}
	}
	res, err = Reassemble(view, []byte(strings.Join(kept, "\n")), &devSecret)
	if err != nil || len(res.Problems) != 0 || strings.Contains(res.YAML, "prod_pass") {
		t.Fatalf("deleting a locked value: %v %+v\n%s", err, res.Problems, res.YAML)
	}
}

// Only a password for another vault id: every value is locked, the file still
// opens, a new value is encrypted with that password and the locked ones are
// saved unchanged.
func TestMultiID_NoPasswordFitsAnyValueStillOpensAndAcceptsNewValues(t *testing.T) {
	src := multiIDSource(t)
	other := vaultcrypto.Secret{ID: "qa", Password: "qa pw"}
	view, err := BuildDecryptedView(src, []vaultcrypto.Secret{other})
	if err != nil {
		t.Fatal(err)
	}
	if len(view.VaultedBefore) != 0 {
		t.Fatalf("decrypted %v", view.VaultedBefore)
	}
	res, err := Reassemble(view, []byte(view.PlaintextYAML+"qa_secret: hello\n"), &other)
	if err != nil || len(res.Problems) != 0 {
		t.Fatalf("%v %+v", err, res.Problems)
	}
	back, err := BuildDecryptedView([]byte(res.YAML), []vaultcrypto.Secret{other, devSecret, prodSecret})
	if err != nil || back.VaultedBefore["qa_secret"] != "hello" || back.UsedBefore["qa_secret"].Label != "qa" {
		t.Fatalf("%v %+v", err, back.UsedBefore)
	}
	for k := range view.LockedBefore {
		if back.SourceContent[k] != view.SourceContent[k] {
			t.Errorf("locked value %s changed", k)
		}
	}
}

func TestMultiID_NewValueUsesTheNewSecretOrIsAProblem(t *testing.T) {
	view, err := BuildDecryptedView(multiIDSource(t), []vaultcrypto.Secret{devSecret, prodSecret})
	if err != nil {
		t.Fatal(err)
	}
	edited := view.PlaintextYAML + "fresh: brand new\n"
	res, err := Reassemble(view, []byte(edited), &prodSecret)
	if err != nil || len(res.Problems) != 0 {
		t.Fatalf("Reassemble: %v %+v", err, res.Problems)
	}
	after, _ := BuildDecryptedView([]byte(res.YAML), []vaultcrypto.Secret{devSecret, prodSecret})
	if after.VaultedBefore["fresh"] != "brand new" || after.UsedBefore["fresh"].Label != "prod" {
		t.Errorf("new value: %+v", after.UsedBefore["fresh"])
	}
	res, err = Reassemble(view, []byte(edited), nil)
	if err != nil || len(res.Problems) != 1 || res.Problems[0].Key != "fresh" {
		t.Errorf("without a secret: %v %+v", err, res.Problems)
	}
}

// An empty file (or one with only whitespace or comments) is a mapping with
// no variables: the editor opens on it, and what is added gets encrypted.
func TestEmptyFileIsAnEmptyMappingAndNewValuesAreEncrypted(t *testing.T) {
	password := testPassword(t)
	for name, src := range map[string]string{
		"empty":      "",
		"whitespace": "\n  \n",
		"comments":   "# vault vars\n",
	} {
		t.Run(name, func(t *testing.T) {
			view, err := BuildDecryptedView([]byte(src), one(password))
			if err != nil {
				t.Fatalf("BuildDecryptedView: %v", err)
			}
			if view.PlaintextYAML != src {
				t.Errorf("view = %q, want the source %q", view.PlaintextYAML, src)
			}
			same, err := Reassemble(view, []byte(src), newOne(password))
			if err != nil || len(same.Problems) != 0 || !same.Unchanged {
				t.Fatalf("an unedited empty file: %+v, %v", same, err)
			}

			result, err := Reassemble(view, []byte(src+"db_password: s3cret\n"), newOne(password))
			if err != nil || len(result.Problems) != 0 {
				t.Fatalf("Reassemble: %+v, %v", result, err)
			}
			if src == "" && strings.HasPrefix(result.YAML, "\n") {
				t.Errorf("a leading blank line in %q", result.YAML)
			}
			if !strings.HasPrefix(result.YAML, src) {
				t.Errorf("the comment header was lost: %q", result.YAML)
			}
			back, err := BuildDecryptedView([]byte(result.YAML), one(password))
			if err != nil || back.VaultedBefore["db_password"] != "s3cret" {
				t.Errorf("db_password did not round-trip: %+v, %v", back.VaultedBefore, err)
			}
		})
	}
}
