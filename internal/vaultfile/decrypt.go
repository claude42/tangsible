package vaultfile

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"code.aw.net/claude/tangsible/internal/vaultcrypto"
)

// rejectNestedVault reports an error if any node in n's subtree (not n
// itself) carries the !vault tag - design-docs/Vault.md point 4's v1
// scope decision: only a direct top-level value may be vaulted. Called
// with each top-level value node, so this only ever inspects what's
// nested *inside* it (a list, a nested mapping) - a vaulted value itself
// is always a leaf scalar with no Content to recurse into anyway.
func rejectNestedVault(n *yaml.Node) error {
	for _, c := range n.Content {
		if c.Tag == vaultTag {
			return fmt.Errorf("found a !vault value nested at line %d - only top-level values are supported (design-docs/Vault.md point 4)", c.Line)
		}
		if err := rejectNestedVault(c); err != nil {
			return err
		}
	}
	return nil
}

// BuildDecryptedView parses sourceBytes (a vault file's full content),
// decrypts every top-level !vault-tagged value under password, and
// produces the plaintext editable view an $EDITOR opens on. Fails loudly
// (returns an error, never a partial/best-effort view) on: content that
// isn't a flat top-level YAML mapping; a !vault tag found anywhere other
// than a direct top-level value; a !vault node that fails to decrypt
// (wrong password or corrupted data); or a !vault tag on a non-scalar
// node (malformed input).
func BuildDecryptedView(sourceBytes []byte, password string) (DecryptedView, error) {
	_, root, lines, err := parseTopLevelMapping(sourceBytes)
	if err != nil {
		return DecryptedView{}, err
	}

	_, sourceContent, _ := keyContentSpans(root, lines)

	view := DecryptedView{
		VaultedBefore: map[string]string{},
		SourceContent: sourceContent,
		PresentBefore: map[string]bool{},
		IndentWidth:   detectIndentWidth(lines),
	}

	// The editable view is the source file's own text with only each
	// vaulted entry's lines swapped for its decrypted form - not a yaml.v3
	// re-encoding of the whole tree, which silently drops blank lines
	// between entries (and reformats everything else). Everything not
	// replaced - header, plain entries, comments, blank lines - stays
	// byte-for-byte, so Reassemble's gaps (taken from the edited file) still
	// carry them through to the saved file.
	type replacement struct {
		start, n int // 1-indexed first line and line count of the entry
		text     []string
	}
	var repls []replacement

	for i := 0; i+1 < len(root.Content); i += 2 {
		keyNode := root.Content[i]
		valNode := root.Content[i+1]
		key := keyNode.Value
		view.PresentBefore[key] = true

		if err := rejectNestedVault(valNode); err != nil {
			return DecryptedView{}, fmt.Errorf("vaultfile: %q: %w", key, err)
		}

		if valNode.Tag != vaultTag {
			continue // plain value, nothing to decrypt
		}
		if valNode.Kind != yaml.ScalarNode {
			return DecryptedView{}, fmt.Errorf("vaultfile: %q: !vault tag on a non-scalar value (line %d)", key, valNode.Line)
		}

		plaintext, err := vaultcrypto.Decrypt(valNode.Value, password)
		if err != nil {
			return DecryptedView{}, fmt.Errorf("vaultfile: %q: %w", key, err)
		}

		view.VaultedBefore[key] = plaintext

		text, err := encodePlainEntry(key, plaintext, view.IndentWidth)
		if err != nil {
			return DecryptedView{}, fmt.Errorf("vaultfile: encoding decrypted view: %w", err)
		}
		repls = append(repls, replacement{
			start: keyNode.Line,
			n:     strings.Count(sourceContent[key], "\n") + 1,
			text:  text,
		})
	}

	var out []string
	next := 1
	for _, r := range repls {
		out = append(out, sliceLines(lines, next, r.start-1)...)
		out = append(out, r.text...)
		next = r.start + r.n
	}
	out = append(out, sliceLines(lines, next, len(lines))...)
	view.PlaintextYAML = strings.Join(out, "\n")

	return view, nil
}
