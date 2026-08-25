package vaultfile

import (
	"fmt"
	"strings"

	"code.aw.net/claude/tangsible/internal/vaultcrypto"
)

// Problem is one reason a save can't proceed as-is - either a previously
// vaulted value no longer resolves to a plain string, a new value isn't a
// plain string either, or a !vault tag was typed by hand into the
// editable view (design-docs/Vault.md point 5). Line is the offending
// key's own line number in the edited content; 0 means a whole-file-level
// issue with no line to anchor a comment to (see Reassemble's doc
// comment).
type Problem struct {
	Key  string
	Line int
	Msg  string
}

// ReassembleResult is Reassemble's outcome. Problems non-empty means:
// write nothing, annotate and reopen the editor instead (AnnotateProblems
// does the annotating). Otherwise YAML is the file to write and Warnings
// are printed alongside it. Unchanged is a best-effort "nothing was
// actually re-encrypted" signal, defense in depth alongside (not a
// replacement for) the editor loop's own raw byte-comparison no-op check.
type ReassembleResult struct {
	YAML      string
	Unchanged bool
	Warnings  []string
	Problems  []Problem
}

// Reassemble computes the per-key diff between before (the pre-edit
// decrypted snapshot from BuildDecryptedView) and editedBytes (the file
// as the user left it in the editor), and produces either a ready-to-
// write result or a set of Problems for the caller to annotate and loop
// on.
//
// The result is built by splicing raw source-text spans (keyContentSpans),
// not by re-encoding a mutated yaml.Node tree - see keyContentSpans' own
// doc comment for why: yaml.v3's re-emission doesn't preserve a node's
// original formatting, which meant an earlier version of this function
// reformatted every single vaulted block on every save (a real bug caught
// in live use), not just the ones actually touched, defeating design-docs/
// Vault.md point 2's "diffs are kept minimal" even though the *ciphertext*
// itself was correctly left alone. An unedited key's own content now
// comes from before.SourceContent - the original file's own bytes,
// untouched; an unedited-but-previously-plaintext key's content comes
// from the edited file's own bytes (also untouched, just from the other
// side); only a genuinely changed or brand-new value goes through
// formatVaultBlock, which hand-formats fresh text rather than asking
// yaml.v3 to re-emit anything.
//
// A non-nil error here always means editedBytes isn't parseable as a flat
// top-level YAML mapping at all (wraps ErrInvalidYAML) - genuinely
// different from Problems, which requires a successful parse. The editor
// loop must treat the two differently: a Problem gets an inline comment
// at a known line; an unparseable file has no such line, so the loop
// falls back to showing the raw error and reopening the content
// completely unchanged (design-docs/Vault.md point 5).
func Reassemble(before DecryptedView, editedBytes []byte, password string) (ReassembleResult, error) {
	_, root, lines, err := parseTopLevelMapping(editedBytes)
	if err != nil {
		return ReassembleResult{}, err
	}
	headerLines, editedContent, editedGaps := keyContentSpans(root, lines)

	var result ReassembleResult
	reencrypted := false
	outLines := append([]string{}, headerLines...)

	for i := 0; i+1 < len(root.Content); i += 2 {
		keyNode := root.Content[i]
		valNode := root.Content[i+1]
		key := keyNode.Value

		d := decideKey(key, valNode, before)
		switch d.kind {
		case decisionProblem:
			result.Problems = append(result.Problems, Problem{Key: key, Line: keyNode.Line, Msg: d.problem})
			continue // no content to append - this save won't proceed anyway
		case decisionSplice:
			outLines = appendBlock(outLines, before.SourceContent[key])
		case decisionReencrypt:
			vaultString, err := vaultcrypto.Encrypt(valNode.Value, password)
			if err != nil {
				return ReassembleResult{}, fmt.Errorf("vaultfile: encrypting %q: %w", key, err)
			}
			outLines = append(outLines, formatVaultBlock(key, vaultString, before.IndentWidth)...)
			reencrypted = true
		case decisionPlain:
			if d.warn {
				result.Warnings = append(result.Warnings, fmt.Sprintf("warning: %q is not vault-encrypted, left as plaintext", key))
			}
			outLines = appendBlock(outLines, editedContent[key])
		}
		outLines = appendBlock(outLines, editedGaps[key])
	}

	if len(result.Problems) > 0 {
		return ReassembleResult{Problems: result.Problems}, nil
	}

	result.YAML = strings.Join(outLines, "\n") + "\n"
	result.Unchanged = !reencrypted
	return result, nil
}

// formatVaultBlock hand-formats a fresh "key: !vault |" block, indented
// by indentWidth - deliberately not routed through yaml.v3's encoder (see
// detectIndentWidth's doc comment on why: its SetIndent silently clamps
// to [2, 9], which can't reproduce real ansible-vault's own 10-space
// convention).
func formatVaultBlock(key, vaultString string, indentWidth int) []string {
	indent := strings.Repeat(" ", indentWidth)
	lines := []string{key + ": !vault |"}
	for _, l := range strings.Split(vaultString, "\n") {
		lines = append(lines, indent+l)
	}
	return lines
}

// appendBlock appends block's own lines to dst, splitting on "\n" -
// a no-op for an empty block, so it never introduces a spurious blank
// line for a key with no gap after it.
func appendBlock(dst []string, block string) []string {
	if block == "" {
		return dst
	}
	return append(dst, strings.Split(block, "\n")...)
}
