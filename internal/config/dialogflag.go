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

package config

// DialogOverride is an explicit --dialog/--no-dialog choice pulled from
// the command line by ExtractDialogFlag - design-docs/RerunDialog.md's own
// mechanism for overriding whether the re-run dialog opens at a session's
// startup, on top of the run_dialog config preference (RunDialogPreference)
// and each Verb's own built-in default. DialogOverrideUnset means neither
// flag was given - the zero value, so an ordinary invocation with no
// override needs no special-casing at call sites.
type DialogOverride int

const (
	DialogOverrideUnset DialogOverride = iota
	DialogOverrideShow
	DialogOverrideHide
)

// HasDialogFlag reports whether args contains a bare "--dialog" or
// "--no-dialog", without stripping either out - used by every Verb that
// doesn't support them at all (design-docs/RerunDialog.md: everything
// except run/rerun/role) to reject them with a clear usage error, rather
// than silently letting them leak through as an arg a downstream parser -
// or, worse, ansible-playbook itself - would treat as unrecognized.
func HasDialogFlag(args []string) bool {
	for _, a := range args {
		if a == "--dialog" || a == "--no-dialog" {
			return true
		}
	}
	return false
}

// ExtractDialogFlag pulls --dialog/--no-dialog out of args for the three
// Verbs that do support them (run, rerun, role) - the same bare-boolean-
// flag shape as ExtractRerunFlags's own --only-failed etc. (presence
// anywhere is enough, a repeated flag is a no-op), and the same reasoning
// for never letting either reach AppendInvocation's recorded history
// string as ExtractStartAtPlay/ExtractRerunFlags already have for their
// own synthetic flags: every caller strips this before building whatever
// gets persisted (design-docs/RerunDialog.md's "don't persist
// --dialog/--no-dialog into invocation history").
//
// ok is false only if both flags were given together - a contradiction the
// caller reports as a usage error; rest is args unchanged in that case,
// and also when neither flag was present (same "return the identical
// slice, not a copy" convention as ExtractStartAtPlay).
func ExtractDialogFlag(args []string) (override DialogOverride, rest []string, ok bool) {
	sawDialog, sawNoDialog := false, false
	out := make([]string, 0, len(args))
	for _, a := range args {
		switch a {
		case "--dialog":
			sawDialog = true
		case "--no-dialog":
			sawNoDialog = true
		default:
			out = append(out, a)
		}
	}
	switch {
	case sawDialog && sawNoDialog:
		return DialogOverrideUnset, args, false
	case sawDialog:
		return DialogOverrideShow, out, true
	case sawNoDialog:
		return DialogOverrideHide, out, true
	default:
		return DialogOverrideUnset, args, true
	}
}

// ResolveDialogVisibility decides whether the re-run dialog should open at
// a session's startup (design-docs/RerunDialog.md), combining - in
// priority order - an explicit --dialog/--no-dialog CLI override
// (highest), the run_dialog config preference, and finally verbDefault -
// each Verb's own built-in behavior with neither of the other two given:
// true for "rerun", false for "run"/"role".
func ResolveDialogVisibility(override DialogOverride, pref DialogPreference, verbDefault bool) bool {
	switch override {
	case DialogOverrideShow:
		return true
	case DialogOverrideHide:
		return false
	}
	switch pref {
	case DialogPreferenceNever:
		return false
	case DialogPreferenceAlways:
		return true
	default:
		return verbDefault
	}
}
