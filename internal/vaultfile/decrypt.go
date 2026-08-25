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
	doc, root, lines, err := parseTopLevelMapping(sourceBytes)
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

		valNode.Tag = ""
		valNode.Value = plaintext
		if strings.Contains(plaintext, "\n") {
			valNode.Style = yaml.LiteralStyle
		} else {
			valNode.Style = 0
		}
	}

	plaintextYAML, err := encodeNode(doc, view.IndentWidth)
	if err != nil {
		return DecryptedView{}, fmt.Errorf("vaultfile: encoding decrypted view: %w", err)
	}
	view.PlaintextYAML = plaintextYAML

	return view, nil
}
