package vaultfile

import (
	"sort"
	"strings"
)

// AnnotationPrefix marks a comment line AnnotateProblems itself inserted,
// as opposed to one the user wrote. Exported so callers can recognize and
// strip stale annotations - see StripAnnotations - before annotating
// again (to avoid stacking duplicates across reopen rounds) and before
// treating a save as successful (so a fixed problem's own now-irrelevant
// annotation doesn't linger in the file forever).
const AnnotationPrefix = "# TANGSIBLE VAULT: "

// AnnotateProblems inserts a comment directly above each problem's line
// into editedBytes, so reopening the editor shows the user exactly what's
// wrong and where - the visudo/crontab-e-style "reopen with an inline
// annotation, never lose edits" loop from design-docs/Vault.md point 5.
//
// Insertion runs bottom-to-top by line number, so inserting a comment for
// a later problem never shifts the line number of an earlier one still
// waiting to be annotated - this is what lets a single pass correctly
// annotate multiple simultaneous problems, per the doc's explicit
// requirement.
//
// Callers are expected to have already run StripAnnotations on
// editedBytes - this function itself doesn't do that, so it stays a pure
// "add these annotations" operation independent of that policy.
//
// A problem with Line == 0 (a whole-file-level issue with no specific
// line to anchor to) is skipped here - Reassemble never actually produces
// one of those itself; that case is Reassemble's ErrInvalidYAML return,
// handled separately by the caller.
func AnnotateProblems(editedBytes []byte, problems []Problem) []byte {
	lines := strings.Split(string(editedBytes), "\n")

	sorted := make([]Problem, 0, len(problems))
	for _, p := range problems {
		if p.Line > 0 {
			sorted = append(sorted, p)
		}
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Line > sorted[j].Line })

	for _, p := range sorted {
		idx := p.Line - 1
		if idx < 0 || idx > len(lines) {
			continue
		}
		comment := leadingWhitespace(lines, idx) + AnnotationPrefix + p.Msg
		lines = append(lines[:idx], append([]string{comment}, lines[idx:]...)...)
	}

	return []byte(strings.Join(lines, "\n"))
}

// StripAnnotations removes every line AnnotateProblems itself previously
// inserted (identified by AnnotationPrefix). Meant to be called on the
// editor's output at the top of every reopen-loop round, before anything
// else looks at it: without this, a stale annotation from an earlier
// round would either stack a duplicate on top of itself (if the same
// problem still isn't fixed - AnnotateProblems has no way to know one is
// already there) or linger permanently in the saved file (if the problem
// *was* fixed - it's no longer relevant, but nothing else would ever
// remove it). Both were real bugs caught in live use, not hypothetical.
func StripAnnotations(data []byte) []byte {
	lines := strings.Split(string(data), "\n")
	kept := lines[:0]
	for _, l := range lines {
		if strings.HasPrefix(strings.TrimLeft(l, " \t"), AnnotationPrefix) {
			continue
		}
		kept = append(kept, l)
	}
	return []byte(strings.Join(kept, "\n"))
}

func leadingWhitespace(lines []string, idx int) string {
	if idx < 0 || idx >= len(lines) {
		return ""
	}
	line := lines[idx]
	trimmed := strings.TrimLeft(line, " \t")
	return line[:len(line)-len(trimmed)]
}
