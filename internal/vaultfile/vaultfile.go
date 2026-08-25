// Package vaultfile builds and reassembles the plaintext editable view of
// an individually-vault-encrypted Ansible variables file (see
// design-docs/Vault.md), and computes the per-key diff between what was
// decrypted before editing and what's there after - the mechanism that
// avoids re-encrypting a value's ciphertext just because it wasn't
// touched (see vaultcrypto's own doc comment on why Vault's random
// per-encryption salt makes that distinction load-bearing).
package vaultfile

import (
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// vaultTag is the YAML tag Ansible uses to mark an individually-encrypted
// scalar value - confirmed empirically (not just assumed) to be preserved
// verbatim as this literal string by yaml.v3's parser.
const vaultTag = "!vault"

const defaultIndentWidth = 2

// ErrInvalidYAML wraps any failure to parse a byte slice as a flat
// top-level YAML mapping - both the original source file and, more
// importantly, the user's post-edit content. Callers distinguish this
// from any other error: content that fails this check has no line number
// to anchor a Problem comment to, so it gets the raw-error-and-reopen-
// unchanged treatment instead (design-docs/Vault.md point 5).
var ErrInvalidYAML = errors.New("vaultfile: not a valid flat top-level YAML mapping")

// DecryptedView is the plaintext editable view of a vault file, plus
// everything Reassemble needs to remember about its pre-edit state.
type DecryptedView struct {
	// PlaintextYAML is what gets written to the editor's temp file.
	PlaintextYAML string
	// VaultedBefore maps a key to its decrypted plaintext value, for
	// every key that was !vault-tagged in the source file.
	VaultedBefore map[string]string
	// SourceContent maps a key to its own exact, untouched raw source
	// text (the "key: value" block, including original indentation,
	// trailing blank lines trimmed) - see keyContentSpans. Reassemble
	// splices this back in verbatim, byte-for-byte, when a vaulted value
	// goes unedited - not just re-encrypting to the same plaintext (see
	// vaultcrypto's own doc comment on why that alone isn't enough to
	// keep the ciphertext identical), and not just reusing the parsed
	// yaml.Node's own Value/Tag/Style either (yaml.v3's own re-emission
	// doesn't preserve original formatting - see detectIndentWidth's doc
	// comment for the concrete case that caught this).
	SourceContent map[string]string
	// PresentBefore records every top-level key that existed in the
	// source file at all, vaulted or not - needed to tell "always been
	// plaintext" (stays plaintext, with a warning) apart from "brand
	// new" (gets encrypted), per design-docs/Vault.md point 3.
	PresentBefore map[string]bool
	// IndentWidth is detected from the source file (see
	// detectIndentWidth) - used both for the temp editable view (via
	// yaml.v3's encoder, whose own indent quirks don't matter there,
	// since that file is never diffed against anything) and, more
	// importantly, to hand-format a freshly re-encrypted or brand-new
	// !vault block in Reassemble, matching the source file's own
	// indentation convention exactly rather than whatever yaml.v3's
	// SetIndent would otherwise produce.
	IndentWidth int
}

// parseTopLevelMapping decodes data into a yaml.Node tree and returns both
// the document node and its real top-level (mapping) node, requiring the
// latter to be a flat mapping - the only shape design-docs/Vault.md's v1
// scope supports. Also returns data split into lines, for callers that
// need to inspect raw source text alongside the node tree
// (detectIndentWidth).
//
// The document node must be kept around and encoded later (see
// encodeNode), not discarded in favor of just the mapping node: confirmed
// empirically that yaml.v3 attaches a file's leading comment block to the
// *document* node's HeadComment, not to the mapping node it wraps -
// encoding the mapping alone silently drops it.
func parseTopLevelMapping(data []byte) (doc, root *yaml.Node, lines []string, err error) {
	doc = &yaml.Node{}
	if err := yaml.Unmarshal(data, doc); err != nil {
		return nil, nil, nil, fmt.Errorf("%w: %v", ErrInvalidYAML, err)
	}
	if len(doc.Content) == 0 {
		return nil, nil, nil, fmt.Errorf("%w: empty document", ErrInvalidYAML)
	}
	// yaml.Unmarshal into a *yaml.Node wraps the real top-level node one
	// level down in a DocumentNode - root here is that actual top-level
	// node. Same convention as internal/source/source.go's indexFile.
	root = doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, nil, nil, fmt.Errorf("%w: top level must be a mapping of variable names to values", ErrInvalidYAML)
	}
	return doc, root, strings.Split(string(data), "\n"), nil
}

// keyContentSpans computes, for a flat top-level mapping, each key's own
// raw source text - split into its own content (the "key: value" block,
// trailing blank/comment lines trimmed off) and the gap that follows it
// verbatim up to the next key or EOF (blank lines, and any top-level
// comment introducing the *next* entry) - plus headerLines, everything
// before the very first key (title comments, blank lines).
//
// Span boundaries use the same rule internal/source/source.go's
// recordNode already established: a key's own span ends at the next
// key's own start line, or EOF for the last one. Unlike recordNode (which
// only ever looks up one node's own text and discards everything else),
// this keeps the trimmed-off tail around too, since Reassemble needs to
// reconstruct a whole document, not just answer "what does this one key
// look like."
//
// This - reconstructing a document by splicing raw text spans rather
// than re-encoding a mutated yaml.Node tree - is what actually delivers
// design-docs/Vault.md point 2's "diffs are kept minimal": yaml.v3's own
// re-emission doesn't preserve a node's original formatting (confirmed
// the hard way - see detectIndentWidth's doc comment), so re-encoding the
// whole tree on every save reformatted every single vaulted block, not
// just the ones actually touched, even though the ciphertext *value* was
// correctly left alone. Splicing raw bytes for anything unedited sidesteps
// the whole problem: there's no re-emission step to introduce drift.
//
// The gap/content split matters more than it looks: Reassemble always
// takes a key's *gap* from the edited file, regardless of that key's own
// decision (splice/reencrypt/plain), but takes a spliced (unedited) key's
// *content* from the original file instead - so anything wrongly
// classified as the trailing content of a key that ends up spliced is
// silently discarded on save, since a spliced key's own edited-side
// content is never used. This was a real bug, caught live: a "# comment"
// line added directly above a brand-new key was swallowed by the
// *previous* (unedited, spliced) key's own trailing content instead of
// being recognized as a gap-line introducing the next entry - only a
// top-level comment (starting at column 0, `strings.HasPrefix(line, "#")`
// without trimming first) counts here, deliberately: an *indented* line
// starting with "#" could legitimately be part of a multi-line secret's
// own literal content, not a comment at all.
func keyContentSpans(root *yaml.Node, lines []string) (headerLines []string, contents, gaps map[string]string) {
	contents = map[string]string{}
	gaps = map[string]string{}

	firstLine := len(lines) + 1
	if len(root.Content) > 0 {
		firstLine = root.Content[0].Line
	}
	headerLines = sliceLines(lines, 1, firstLine-1)

	for i := 0; i+1 < len(root.Content); i += 2 {
		key := root.Content[i].Value
		start := root.Content[i].Line
		end := len(lines) + 1
		if i+2 < len(root.Content) {
			end = root.Content[i+2].Line
		}
		span := sliceLines(lines, start, end-1)

		contentEnd := len(span)
		for contentEnd > 0 {
			line := span[contentEnd-1]
			if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
				contentEnd--
				continue
			}
			break
		}
		contents[key] = strings.Join(span[:contentEnd], "\n")
		gaps[key] = strings.Join(span[contentEnd:], "\n")
	}
	return headerLines, contents, gaps
}

// sliceLines returns lines[from-1:to] (1-indexed, inclusive), clamped to
// lines' own bounds. An empty span (from > to) returns nil.
func sliceLines(lines []string, from, to int) []string {
	if to > len(lines) {
		to = len(lines)
	}
	if from > to {
		return nil
	}
	return lines[from-1 : to]
}

// isPlainString reports whether n is a scalar that resolves to YAML's
// !!str tag - i.e. an ordinary string value, as opposed to a number,
// bool, null, mapping, or sequence. Confirmed empirically that yaml.v3
// populates this implicit tag on every scalar node, not just ones decoded
// further into a typed value.
func isPlainString(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && n.Tag == "!!str"
}

// hasVaultTagAnywhere reports whether n itself, or anything in its
// subtree, carries the !vault tag. Used both to reject a source file with
// a non-top-level vaulted value (design-docs/Vault.md point 4) and to
// detect a !vault tag hand-typed into the editable view, which never
// contained one to begin with (point 5).
func hasVaultTagAnywhere(n *yaml.Node) bool {
	if n.Tag == vaultTag {
		return true
	}
	for _, c := range n.Content {
		if hasVaultTagAnywhere(c) {
			return true
		}
	}
	return false
}

// detectIndentWidth is a documented heuristic, not a general YAML-style
// sniffer: it looks for the first block-scalar header line (ending in "|"
// or ">") and measures its first content line's own indent, since that's
// the only indentation a flat top-level vault file (v1's whole scope)
// ever actually contains. Falls back to defaultIndentWidth if none is
// found - same "good enough, not chased further" spirit as this
// codebase's other documented heuristics (e.g. tui.go's taskLabel
// truncation).
//
// Deliberately *not* clamped to yaml.v3's own SetIndent range ([2, 9] -
// confirmed against its source, apic.go's yaml_emitter_set_indent, which
// silently resets anything outside that range back to 2 with no error).
// That clamp only matters to callers that feed this value into
// SetIndent - the temp editable view does (harmlessly - its own cosmetic
// formatting is never diffed against anything), but Reassemble's
// formatVaultBlock hand-formats text directly instead, precisely so it
// isn't bound by that limitation: real ansible-vault's own
// encrypt_string output indents a vault block by 10 spaces, past the top
// of yaml.v3's usable range, and a freshly re-encrypted or brand-new
// block can now match that exactly.
func detectIndentWidth(lines []string) int {
	for i, line := range lines {
		trimmed := strings.TrimRight(line, " \t")
		if !strings.HasSuffix(trimmed, "|") && !strings.HasSuffix(trimmed, ">") {
			continue
		}
		for j := i + 1; j < len(lines); j++ {
			next := lines[j]
			if strings.TrimSpace(next) == "" {
				continue
			}
			indent := len(next) - len(strings.TrimLeft(next, " "))
			if indent > 0 {
				return indent
			}
			break
		}
	}
	return defaultIndentWidth
}

// encodeNode encodes doc (the document node, not the mapping node it
// wraps - see parseTopLevelMapping's doc comment on why that distinction
// matters for comment preservation) back to YAML text with the given
// indent width.
func encodeNode(doc *yaml.Node, indentWidth int) (string, error) {
	var buf strings.Builder
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(indentWidth)
	if err := enc.Encode(doc); err != nil {
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	return buf.String(), nil
}
