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

package session

import (
	"strings"

	"code.aw.net/claude/tangsible/internal/playbook"
	"code.aw.net/claude/tangsible/internal/runner"
	"github.com/rivo/tview"
)

// rerunFieldSync owns the re-run dialog's four input fields, three
// checkboxes, and the sync logic between them (design-docs/Rerun.md's
// "Extend rerun dialog") - increment 4 of the NewLiveTUI refactor (see the
// plan this session worked from). Deliberately does NOT own openRerunDialog/
// submitRerun: those stay as liveSession-level closures (the "bridge" into
// the rest of NewLiveTUI's shared state - resetting s.expanded/s.currentID/
// s.resolveCache etc, calling requestRerun, closing dialogs), and
// deliberately does NOT own the *tview.Form container itself
// (liveSession.rerunForm) - that widget is also reached directly by the
// dialog-layout code, SetInputCapture, and SetMouseCapture (none of which
// this increment touches), so it stays a plain liveSession field and is
// passed into this type by reference, the same "shared reference it doesn't
// own outright" shape livesession_tabsearch.go's tabSearchPanel already
// established for tabs/bottomBar.
type rerunFieldSync struct {
	form *tview.Form // shared - liveSession.rerunForm; rebuild() populates it via Clear/AddFormItem

	state                *playbook.PlaybookState
	knownTags            []string
	knownPlayNames       []string
	initialRerunDefaults runner.InitialRerunDefaults

	playField               *tview.InputField
	tagsField               *tview.InputField
	skipTagsField           *tview.InputField
	hostsField              *tview.InputField
	resumeCheckbox          *tview.Checkbox
	onlyFailedCheckbox      *tview.Checkbox
	onlyUnreachableCheckbox *tview.Checkbox

	// currentFailedHosts/currentUnreachableHosts/currentResumePlay are
	// recomputed fresh every time the dialog opens (rebuild, below) from
	// whichever PlaybookState is relevant - the just-finished generation's
	// own live state for every case except the "rerun" verb's very first
	// dialog open, where nothing has run yet in this process and
	// initialRerunDefaults (computed from a replayed run log,
	// runner.ReplayRunLog) is used once instead. The checkboxes' own
	// changed-handlers below always read these three fields' current
	// values, never values captured at construction time.
	currentFailedHosts      []string
	currentUnreachableHosts []string
	currentResumePlay       string

	// syncingPlayField/syncingHostsField mark "this SetText call is a
	// checkbox's own doing, not a keystroke" so playField's/hostsField's
	// own changed-handlers below can tell the difference and skip reacting
	// to their own checkbox-driven writes - without this, checking a box
	// would immediately observe its own write as if the user had just
	// typed it and instantly undo the very check that caused it.
	// suppressPlayClear/suppressHostsClear mark the opposite direction:
	// "this checkbox is being unchecked *because* the user just edited its
	// linked field, not because they clicked the checkbox itself" - per
	// Rerun.md, unchecking a checkbox this way must leave the field's new
	// text alone (the user's own edit), while unchecking it by clicking it
	// directly must clear the field. Both paths fire the exact same
	// SetChangedFunc callback, so this is what tells them apart.
	syncingPlayField   bool
	syncingHostsField  bool
	suppressPlayClear  bool
	suppressHostsClear bool

	// acDismissed tracks whether the user has already Escaped the
	// currently-showing autocomplete drop-down once - needed alongside the
	// plain has-matches check in autocompleteOpenNow, not instead of it.
	// Confirmed live: Escape doesn't change the field's own text, so a
	// naive "does the current text have matches" check still says yes
	// immediately afterward (e.g. text "database" still substring-matches
	// itself), which would swallow every subsequent Escape as "dismiss the
	// drop-down" forever and never actually close the dialog. Reset to
	// false by resetDismissed, wired to all four fields' own
	// SetChangedFunc, so any real text change (typing, or a pick) starts a
	// fresh interaction - only set true from SetInputCapture's own Escape
	// case, right when it lets an Escape through to dismiss what it
	// believes is currently showing (hence exported-within-package field
	// access from tui.go rather than a method: that call site isn't part
	// of this subsystem).
	acDismissed bool

	// appliedInitialRerunFlags is a one-shot latch: --only-failed/--only-
	// unreachable/--resume-where-failed (initialRerunDefaults) only ever
	// get to pre-check a box once, the very first time rebuild runs - a
	// later reopen (the user cancelled and pressed 'r' again) must never
	// re-check a box the user has since unchecked by hand.
	appliedInitialRerunFlags bool
}

// newRerunFieldSync builds the four fields and three checkboxes, wires
// every sync/autocomplete handler between them, and registers form as the
// *tview.Form they'll be added to by rebuild(). form/state are shared
// references this type doesn't own; knownTags/knownPlayNames/
// initialRerunDefaults are copied in since NewLiveTUI's own parameters
// aren't needed anywhere outside this subsystem (confirmed by grep before
// this extraction) and so were never hoisted onto liveSession itself.
func newRerunFieldSync(form *tview.Form, state *playbook.PlaybookState, knownTags, knownPlayNames []string, initialRerunDefaults runner.InitialRerunDefaults) *rerunFieldSync {
	r := &rerunFieldSync{
		form:                 form,
		state:                state,
		knownTags:            knownTags,
		knownPlayNames:       knownPlayNames,
		initialRerunDefaults: initialRerunDefaults,
	}

	// There used to be a "Start with task" field here too, gated by its own
	// checkbox in the original design - dropped for two independent
	// reasons. First, design-docs/Rerun.md's "Extend rerun dialog" item 1:
	// task names aren't unique (most playbooks are thin per-play tasks
	// naming the role they call, which repeats across plays), so
	// --start-at-task can't reliably target one exact position - a user
	// can still pass it by hand as a raw ansible-playbook passthrough arg,
	// just with no dedicated field/autocomplete for it here. Second, and
	// still relevant to the checkboxes below even with the field itself
	// gone: live testing of that original "Start at task" checkbox found a
	// genuine tview quirk - InputField.SetDisabled unconditionally calls
	// its own finished(-1), which, once any real Tab/Enter has happened
	// anywhere in the form's lifetime, Form's shared handler replays as a
	// stray navigation key (confirmed against inputfield.go/form.go's own
	// default case for a negative key) - so toggling the checkbox silently
	// advanced focus by one, *in addition to* whatever Tab the user pressed
	// right after, landing text one field over from where it was typed.
	// Checkbox.SetDisabled has the exact same unconditional finished(-1)
	// call (checkbox.go) - so "Resume where failed"/"Only failed"/"Only
	// unreachable" below deliberately never call SetDisabled on anything:
	// they write into playField/hostsField instead of disabling them, and
	// omit themselves from the form entirely (rebuild, below) rather than
	// appearing disabled, when there's nothing for them to do.
	//
	// playField (design-docs/StartWithPlay.md) is a freeform single-value
	// field - empty means "whole playbook." Unlike Tags/Hosts, an empty
	// match here isn't passed straight through to ansible-playbook:
	// requestRerun resolves it into a trimmed, temporary copy of the
	// playbook itself before spawning (runner.NewRequestRerun), rather
	// than any flag ansible-playbook understands natively.
	r.playField = tview.NewInputField().SetLabel("Start with play: ")
	r.tagsField = tview.NewInputField().SetLabel("Limit tags to: ")
	r.skipTagsField = tview.NewInputField().SetLabel("Skip tags: ")
	r.hostsField = tview.NewInputField().SetLabel("Limit hosts to: ")

	r.resumeCheckbox = tview.NewCheckbox().SetLabel("Resume where failed")
	r.onlyFailedCheckbox = tview.NewCheckbox().SetLabel("Only failed")
	r.onlyUnreachableCheckbox = tview.NewCheckbox().SetLabel("Only unreachable")

	r.resumeCheckbox.SetChangedFunc(r.resumeCheckboxChanged)
	r.onlyFailedCheckbox.SetChangedFunc(func(checked bool) {
		r.setHostsFieldFromCheckboxes(checked, r.onlyUnreachableCheckbox.IsChecked())
	})
	r.onlyUnreachableCheckbox.SetChangedFunc(func(checked bool) {
		r.setHostsFieldFromCheckboxes(r.onlyFailedCheckbox.IsChecked(), checked)
	})

	// Autocomplete (design-docs/Autocomplete.md): tagsField/skipTagsField
	// share one candidate list (knownTags, built once by source.go's
	// BuildTaskSourceIndex - a static scan of the playbook/role YAML tree,
	// the only way to source any tag at all, since Ansible's own event
	// stream never carries a task's tags); hostsField reads state.AllHosts
	// fresh on every call via r.matchHosts, so its own candidates keep
	// growing as the run discovers more hosts; playField shares
	// knownPlayNames' static-scan origin (source.ListTopLevelPlayNames).
	// matchTags/matchHosts/matchPlay are also called directly from
	// autocompleteOpenNow (below), itself called from SetInputCapture -
	// recomputed from the field's own current text on every check rather
	// than mirrored into a bool set only inside these callbacks, since
	// InputField.Blur() clears its drop-down internally without
	// re-invoking this callback, which would let a mirrored flag go stale
	// across a mouse-driven focus change.
	r.wireAutocomplete(r.tagsField, r.matchTags, replaceLastToken)
	r.wireAutocomplete(r.skipTagsField, r.matchTags, replaceLastToken)
	r.wireAutocomplete(r.hostsField, r.matchHosts, replaceLastToken)
	r.wireAutocomplete(r.playField, r.matchPlay, func(current, picked string) string { return picked })

	// playField/hostsField each combine resetDismissed with the "the user
	// just edited me directly, not via a checkbox - break the link" half
	// of the sync logic above (SetChangedFunc holds exactly one callback,
	// so these can't be wired separately). syncingPlayField/
	// syncingHostsField distinguish a checkbox's own SetText call (skip
	// reacting) from a real keystroke (react by unchecking); see their own
	// doc comments above for why suppressPlayClear/suppressHostsClear are
	// also needed alongside them.
	r.playField.SetChangedFunc(r.playFieldChanged)
	r.tagsField.SetChangedFunc(r.resetDismissed)
	r.skipTagsField.SetChangedFunc(r.resetDismissed)
	r.hostsField.SetChangedFunc(r.hostsFieldChanged)

	return r
}

func (r *rerunFieldSync) setPlayField(text string) {
	r.syncingPlayField = true
	r.playField.SetText(text)
	r.syncingPlayField = false
}

// setHostsFieldFromCheckboxes takes both checkboxes' checked state as
// explicit parameters rather than querying onlyFailedCheckbox.IsChecked()/
// onlyUnreachableCheckbox.IsChecked() itself - confirmed live (and against
// checkbox.go's own SetChecked): SetChecked invokes the "changed" callback
// *before* updating its own internal checked field, so a checkbox reading
// its own IsChecked() from inside its own changed-handler sees the state
// it's *leaving*, not the one it's being set to - the resumeCheckbox ->
// onlyFailedCheckbox cascade in resumeCheckboxChanged is exactly such a
// call, and silently computed an empty hosts list until this was passed
// explicitly instead. Each of the two checkboxes' own handlers passes its
// own new checked value directly and only ever queries the *other* one's
// IsChecked() (safe - neither checkbox's own SetChecked call is ever
// nested inside the other's).
func (r *rerunFieldSync) setHostsFieldFromCheckboxes(onlyFailed, onlyUnreachable bool) {
	if r.suppressHostsClear {
		return // this uncheck came from hostsField's own edit handler
		// below - it already holds the text the user just typed, and
		// recomputing here would fight that edit.
	}
	hosts := resolvedRerunHosts(onlyFailed, onlyUnreachable, r.currentFailedHosts, r.currentUnreachableHosts)
	r.syncingHostsField = true
	r.hostsField.SetText(strings.Join(hosts, ","))
	r.syncingHostsField = false
}

func (r *rerunFieldSync) resumeCheckboxChanged(checked bool) {
	if checked {
		r.setPlayField(r.currentResumePlay)
		r.onlyFailedCheckbox.SetChecked(true) // cascades into
		// setHostsFieldFromCheckboxes via onlyFailedCheckbox's own handler
		// - a real, intended cascade, not a field edit, so no suppress
		// flag needed here.
		return
	}
	if !r.suppressPlayClear {
		r.setPlayField("")
	}
	// Unlike playField above, Only failed's own uncheck here is
	// unconditional - not gated on suppressPlayClear - per Rerun.md:
	// unchecking "Resume where failed" always takes "Only failed" down
	// with it, whether that happened by clicking the checkbox directly or
	// as the side effect of editing playField just above. A no-op if it's
	// already unchecked (the user broke that half of the link separately -
	// see onlyFailedCheckbox's own handler for why that's allowed to
	// stand on its own).
	r.onlyFailedCheckbox.SetChecked(false)
}

func (r *rerunFieldSync) playFieldChanged(text string) {
	r.resetDismissed(text)
	if !r.syncingPlayField && r.resumeCheckbox.IsChecked() {
		r.suppressPlayClear = true
		r.resumeCheckbox.SetChecked(false)
		r.suppressPlayClear = false
	}
}

func (r *rerunFieldSync) hostsFieldChanged(text string) {
	r.resetDismissed(text)
	if !r.syncingHostsField && (r.onlyFailedCheckbox.IsChecked() || r.onlyUnreachableCheckbox.IsChecked()) {
		r.suppressHostsClear = true
		r.onlyFailedCheckbox.SetChecked(false)
		r.onlyUnreachableCheckbox.SetChecked(false)
		r.suppressHostsClear = false
	}
}

func (r *rerunFieldSync) matchTags(text string) []string  { return matchToken(r.knownTags, text) }
func (r *rerunFieldSync) matchHosts(text string) []string { return matchToken(r.state.AllHosts, text) }
func (r *rerunFieldSync) matchPlay(text string) []string {
	return matchTaskName(r.knownPlayNames, text)
}

// wireAutocomplete's apply func decides how a picked suggestion gets
// written back into the field: replaceLastToken for the comma-separated
// multi-value fields (Tags/Skip tags/Hosts - only the token currently
// being typed is replaced, everything before the last comma is carried
// through untouched), or a plain whole-field replace for playField, which
// - unlike the other three - has only ever one value to begin with, so
// there's no earlier token to preserve.
func (r *rerunFieldSync) wireAutocomplete(field *tview.InputField, match func(string) []string, apply func(current, picked string) string) {
	field.SetAutocompleteFunc(match).
		SetAutocompleteUseTags(false). // plain hostnames/tags/task names,
		// and avoids a literal '[' in one being misread as a color tag -
		// the same class of bug this app guards against everywhere else
		// dynamic text meets a tags-aware widget.
		SetAutocompletedFunc(func(text string, index, source int) bool {
			// Navigating (arrow keys) only moves the highlight within the
			// drop-down itself - deliberately not previewed into the
			// field's own text. Confirmed live: replaceLastToken's own
			// trailing ", " empties the *next* token, which InputField's
			// own "text changed -> re-run Autocomplete()" housekeeping
			// (inputfield.go) sees as "no more matches" and silently
			// closes the drop-down after a single Down press - so a live
			// preview here would make navigation stop working after one
			// step, not just look different.
			if source == tview.AutocompletedNavigate {
				return false
			}
			field.SetText(apply(field.GetText(), text))
			return true
		})
}

func (r *rerunFieldSync) resetDismissed(string) { r.acDismissed = false }

// autocompleteOpenNow reports whether the currently focused re-run dialog
// field has any live autocomplete matches for its current text - used by
// SetInputCapture to decide whether Enter/Escape should reach the field's
// own native drop-down handling (pick/dismiss) or, as today, submit/close
// the whole dialog.
func (r *rerunFieldSync) autocompleteOpenNow() bool {
	if r.acDismissed {
		return false
	}
	switch {
	case r.playField.HasFocus():
		return len(r.matchPlay(r.playField.GetText())) > 0
	case r.tagsField.HasFocus():
		return len(r.matchTags(r.tagsField.GetText())) > 0
	case r.skipTagsField.HasFocus():
		return len(r.matchTags(r.skipTagsField.GetText())) > 0
	case r.hostsField.HasFocus():
		return len(r.matchHosts(r.hostsField.GetText())) > 0
	default:
		return false
	}
}

// rebuild re-derives currentFailedHosts/currentUnreachableHosts/
// currentResumePlay and rebuilds form's own item list to match - called
// every time the dialog opens (openRerunDialog, tui.go), not just once at
// construction, since "which hosts failed/were unreachable last time" is
// tied to whichever generation most recently finished, not a sticky user
// preference the way playField/tagsField/hostsField's own text is.
//
// len(state.Plays) == 0 is what distinguishes "nothing has run yet in this
// process" (only ever true for the "rerun" verb's very first dialog open -
// a "run"/"role" session's first 'r' press, and a revisit session's own
// replayed state, both already have real plays by the time a dialog can
// open at all) from every other case, where state itself - live or
// replayed - is always the right source over initialRerunDefaults, which
// only that one first-open case ever populates.
//
// tview.Form.Clear(false) only clears the form's own item list (f.items =
// nil) - confirmed via form.go - it never touches the underlying
// *InputField/*Checkbox instances' own text/checked state, so re-adding
// the same instances below is exactly as safe as never having removed
// them.
func (r *rerunFieldSync) rebuild() {
	if len(r.state.Plays) == 0 {
		r.currentFailedHosts = r.initialRerunDefaults.FailedHosts
		r.currentUnreachableHosts = r.initialRerunDefaults.UnreachableHosts
		r.currentResumePlay = resumablePlayName(r.initialRerunDefaults.ResumePlay, r.knownPlayNames)
	} else {
		r.currentFailedHosts = r.state.FailedHosts()
		r.currentUnreachableHosts = r.state.UnreachableHosts()
		r.currentResumePlay = resumablePlayName(r.state.EarliestFailingPlay(), r.knownPlayNames)
	}

	r.form.Clear(false)
	r.form.AddFormItem(r.playField)
	if r.currentResumePlay != "" {
		r.form.AddFormItem(r.resumeCheckbox)
	}
	r.form.AddFormItem(r.tagsField)
	r.form.AddFormItem(r.skipTagsField)
	r.form.AddFormItem(r.hostsField)
	if len(r.currentFailedHosts) > 0 {
		r.form.AddFormItem(r.onlyFailedCheckbox)
	}
	if len(r.currentUnreachableHosts) > 0 {
		r.form.AddFormItem(r.onlyUnreachableCheckbox)
	}

	if !r.appliedInitialRerunFlags {
		r.appliedInitialRerunFlags = true
		if r.initialRerunDefaults.CheckResumeWhereFailed {
			r.resumeCheckbox.SetChecked(true)
		} else if r.initialRerunDefaults.CheckOnlyFailed {
			r.onlyFailedCheckbox.SetChecked(true)
		}
		if r.initialRerunDefaults.CheckOnlyUnreachable {
			r.onlyUnreachableCheckbox.SetChecked(true)
		}
	}
}
