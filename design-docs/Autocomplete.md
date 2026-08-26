# Autocomplete

## Situation

The re-run dialog (`Rerun.md`, `internal/session/tui.go`'s `rerunForm`) has
four fields: `taskField` (single task name), `tagsField`, `skipTagsField`,
`hostsField` - the last three all comma-separated multi-value. Users have
to remember and correctly spell every tag/host name themselves, with no
help from the app even though it usually already knows the full set (every
host that's reported anything so far this run; every tag literally written
in the playbook). Logged as a want in `design-docs/Ideas.md`: "auto-complete
e.g. for entering hosts or tags in rerun dialog."

Two things were confirmed empirically before deciding how to build this,
both worth recording since they shape the design:

* **Tags cannot be sourced from the live event stream at all.** A
  throwaway playbook run through the real `ansible.posix.jsonl` callback
  showed `v2_playbook_on_task_start`'s own `task` object carries `name`/
  `path`/`duration`/`id` - never `tags`, tagged or not. So unlike hosts
  (already tracked live via `PlaybookState.AllHosts`), any tag suggestion
  at all has to come from a static scan of the playbook/role source, not
  from anything `aggregate.go` sees.
* **`tview@v0.42.0`'s own `InputField` already ships a working autocomplete
  drop-down** (`SetAutocompleteFunc`/`SetAutocompletedFunc`, confirmed
  directly against `inputfield.go`) with almost exactly the intended
  keyboard model built in: Down opens it, Up/Down/PgUp/PgDn navigate,
  Enter/Tab pick the current entry, Esc dismisses just the drop-down. This
  is not a case for a hand-rolled replacement the way `treeList` had to
  replace `tview.List` - the built-in behavior is already what's wanted.

## Behavior (decided)

* Scope: `tagsField`, `skipTagsField`, `hostsField`. Not `taskField` - a
  single value with a different completion shape, and not what this was
  asked for.
* Suggestions are purely advisory. Whatever the user has typed is always
  valid to submit, drop-down or no drop-down, matched entry or not - a
  host-limit pattern like `web:!excluded` or a brand-new tag that's never
  been used yet must never be blocked or second-guessed.
* Completion is scoped to **the token currently being typed at the end of
  the field** - the substring after the last comma. Typing a `,` always
  finalizes whatever's there as literal text and starts a fresh token,
  drop-down or not. This is also a hard constraint of `tview`'s own API,
  not just a design choice: `SetAutocompleteFunc`'s callback receives only
  the field's full current text, no cursor position - so completing
  anything other than "the tail of the string" isn't something this
  mechanism can support. Going back to edit an earlier token gets no
  suggestions; accepted, not chased further.
* Matching: case-insensitive prefix match; if that's empty, a substring
  match instead. Candidates already present earlier in the same field are
  excluded (no suggesting - or re-picking - a duplicate). Capped at 8
  entries.
* Hosts: `PlaybookState.AllHosts` - already sorted, deduplicated, and
  growing live; no new plumbing needed; not scanning the inventory
  (`internal/inventory`'s `ListInventoryHosts` exists and would be the easy
  upgrade path later, but was deliberately deferred - see "Not in this
  pass" below).
* Tags: every literal `tags:` value found while walking the playbook/role
  YAML tree (see below), unioned with Ansible's five reserved tag names
  (`always`, `never`, `tagged`, `untagged`, `all`) so those are always
  offered even on a playbook that never uses them.

## Implementation (decided)

### Data plumbing

`internal/source/source.go`'s `BuildTaskSourceIndex` already walks every
task mapping in every YAML file under the playbook's directory tree
(`indexFile` -> `walkMappingForTaskLists`/`indexTaskList` -> `recordNode`)
to build the `TASK:` source index. That walk already has each task's own
YAML mapping node in hand at exactly the point (`recordNode`) needed to
also read its `tags:` key - so tag collection rides the same pass rather
than paying for a second directory walk. Signature becomes:

```go
func BuildTaskSourceIndex(playbookPath string) (TaskSourceIndex, []string)
```

second return is the sorted, deduplicated union of every literal tag
string found (skipping any value containing `{{` - a templated tag name
isn't a literal to suggest) plus the five reserved names. A `tags:` value
can be a scalar (`tags: foo`) or a sequence (`tags: [foo, bar]` or block
form); both are read - at task level, block level, play level, *and* on
each `roles:` entry's own mapping form (`- role: foo` / `- {role: foo,
tags: [...]}` - the bare `- foo` shorthand carries no tags of its own).
The last of those needs its own small walk (`collectRoleTags`) since a
role reference isn't a task and is never reached by the task-list walk
that finds everything else - confirmed missing in the first pass by
testing a real `roles:` block live, a common enough pattern (tagging an
entire role at its inclusion point) to be worth its own explicit case
rather than an accepted gap.

Three call sites need updating for the new second return value:
`internal/session/main.go:236`, `internal/diff/diff.go:140`,
`internal/revisit/revisit.go:476` - the latter two don't need the tag list
at all and just discard it (`sourceIndex, _ := ...`).

`NewLiveTUI` (`internal/session/tui.go:128`) gains a new parameter,
`knownTags []string`, threaded from `main.go` alongside the existing
`sourceIndex` argument since both come from the same call.

### The matching/replacement functions (pure, unit-tested)

Three small functions, independent of `tview`, testable directly:

* `lastToken(text string) (token string, earlier map[string]bool)` - splits
  on the last comma; `token` is the trimmed tail, `earlier` is the set of
  every other (trimmed, non-empty) comma-separated value already in the
  field.
* `matchToken(candidates []string, text string) []string` - `lastToken`,
  then prefix-match (case-insensitive) against `candidates`, substring
  fallback if that's empty, excluding anything in `earlier`, capped at
  `autocompleteMaxEntries` (8). Empty token -> nil (no drop-down on bare
  focus).
* `replaceLastToken(text, replacement string) string` - swaps the tail
  token for `replacement` and appends `", "`, leaving everything before it
  untouched - this is what makes completion "replace only the token being
  typed" instead of `tview`'s own default `SetAutocompletedFunc` behavior,
  which replaces the whole field.

`tagsField`/`skipTagsField` share one `matchTags := func(t string) []string
{ return matchToken(knownTags, t) }` (identical candidate set); `hostsField`
gets its own `matchHosts` closing over `state.AllHosts` (read fresh on
every call via closure, so it stays current as the run discovers more
hosts). Each field:

```go
field.SetAutocompleteFunc(matchX).
	SetAutocompletedFunc(func(text string, index, source int) bool {
		field.SetText(replaceLastToken(field.GetText(), text))
		return source != tview.AutocompletedNavigate
	})
```

returning `false` only for `AutocompletedNavigate` keeps the drop-down open
and live-previews each candidate while arrowing through it (matching
`tview`'s own default navigate-preview behavior, just token-scoped instead
of whole-field); Enter/Tab/Click close it. `SetAutocompleteUseTags(false)`
on all three fields - entries are plain hostnames/tags, and turning tag
parsing off avoids the same "a literal `[` gets misread as a color tag"
class of bug this app already guards against everywhere else dynamic text
meets a tags-aware widget, for no benefit (no per-entry rich styling is
planned - see "Not in this pass").

### The Enter/Escape interception conflict

`SetInputCapture`'s `rerunDialogOpen` branch (`tui.go:2304-2317`)
unconditionally swallows Enter (to call `submitRerun()`, since
`tview.Form` itself treats Enter as "advance focus," never "submit") and
Escape (to call `closeDialogs()`, since `Form`'s own default Escape
behavior is "reset focus to the first item"). Both currently run *before*
the keystroke would ever reach the focused `InputField`'s own
`InputHandler` - exactly where the native autocomplete drop-down's own
Enter-to-select/Escape-to-dismiss logic lives. Left as-is, opening a
drop-down and pressing Enter would submit the whole dialog mid-token
instead of picking a suggestion.

Fix: before swallowing Enter/Escape, check whether the currently focused
field has live matches for its current text, and if so let the event
through untouched instead:

```go
autocompleteOpenNow := func() bool {
	switch {
	case tagsField.HasFocus():
		return len(matchTags(tagsField.GetText())) > 0
	case skipTagsField.HasFocus():
		return len(matchTags(skipTagsField.GetText())) > 0
	case hostsField.HasFocus():
		return len(matchHosts(hostsField.GetText())) > 0
	default:
		return false
	}
}
```

checked in both the `KeyEnter` and `KeyEscape` cases, ahead of the existing
`submitRerun()`/`closeDialogs()` calls (the existing button-focus check
for Enter - `rerunForm.GetFocusedItemIndex()` - stays first, since a
focused button can never have an open drop-down).

Deliberately *not* a mirrored `autocompleteOpen bool` set from inside the
`SetAutocompleteFunc` callbacks, which was the first approach considered:
`InputField.Blur()` clears its drop-down internally
(`i.autocompleteList = nil`) without re-invoking the autocomplete
callback, so an external bool could go stale across a mouse-driven focus
change (click a different field while a drop-down is open) and then wrongly
swallow the *next* Escape on the newly-focused field. Recomputing from the
same pure `matchTags`/`matchHosts` functions the fields themselves use
avoids that staleness entirely - one source of truth, same principle
`aggregate.go` already applies to its own counts ("computed on demand...
to avoid a second source of truth").

### Mouse

`SetMouseCapture`'s `rerunDialogOpen` branch (`tui.go:2759-2766`) currently
only lets a click through when it lands inside `rerunForm.GetRect()`
itself, swallowing everything else so it can't leak to the page underneath.
`InputField.Draw` renders its drop-down at an absolute screen position
directly below the field, entirely independent of the parent `Form`'s own
declared height - and `rerunForm` sits in a small fixed-size
`centeredModal(rerunForm, 56, 13)`, with little headroom below any of its
four stacked fields. A drop-down is therefore likely to render at least
partly *outside* `rerunForm`'s own rect, where today's exact-rect check
would swallow a click on it as a dead click.

Fix: widen the hit-test to also accept a click in the band directly below
`rerunForm`'s rect, sized to the maximum drop-down height:

```go
if x, y := event.Position(); uikit.InRect(x, y, rerunForm) {
	return event, action
}
if rx, ry, rw, rh := rerunForm.GetRect(); x >= rx && x < rx+rw &&
	y >= ry+rh && y < ry+rh+autocompleteMaxEntries+1 {
	return event, action
}
return nil, action
```

`InputField` doesn't expose its own drop-down's rect, so this is a
deliberately generous fixed band rather than a precise one - consistent
with this codebase's existing style of fixed, documented budgets elsewhere
(`splitMinTotalWidth`, `maxHistoryPerPlaybook`). Keyboard selection (Down/
Up/Enter/Tab) is unaffected by any of this and is the primary,
guaranteed-correct path; exact mouse-click behavior against a real render
is a live-verification item (see Phasing).

### Styling

No new color scheme - `tview`'s own default autocomplete styling is used
as-is for this first pass. These are plain host/tag names, not
outcome-colored data, so there's nothing here that maps onto this app's
existing green/yellow/teal/red/maroon outcome palette or the search
feature's black-on-yellow match highlight; inventing a third convention
for a short plain list isn't worth it unless the default actually looks
wrong once it's on screen.

## Not in this pass

* `taskField` autocomplete (different, single-value completion shape).
* Mid-token editing (cursor moved back into an earlier token) - a real
  limitation of `tview`'s callback API (full text only, no cursor
  position), not chased around.
* Ranking suggestions by recency (`.tangsible/state.toml`'s own invocation
  history would be the natural source) over the current plain alphabetical
  order.
* Sourcing hosts from the actual inventory (`internal/inventory`'s
  `ListInventoryHosts`, already built for `tangsible hosts`) instead of
  only what's been observed so far this session - the easy upgrade path
  later, deliberately deferred for this first pass (session-observed only).
* Inline highlighting of the matched substring within each suggestion
  entry.

## Proposed phasing

1. `lastToken`/`matchToken`/`replaceLastToken` plus `source.go`'s tag
   collection, all as pure functions with direct unit tests - no `tview`
   involved yet.
2. Wire `SetAutocompleteFunc`/`SetAutocompletedFunc` onto the three fields;
   fix `SetInputCapture`'s Enter/Escape handling and `SetMouseCapture`'s
   click band.
3. Live verification via tmux (this project's established way of testing
   `tview`/`tcell` behavior that can't be unit tested): does the drop-down
   render legibly against the modal's fixed size, does Enter/Escape
   correctly hand off between "pick a suggestion" and "submit/close the
   dialog," does a click on a suggestion actually land.

## Follow-up (Claude, 2026-08-26)

Implemented and verified live via tmux (`tangsible run` against a
throwaway multi-tag, multi-host fixture). End to end works: typing into
Tags/Hosts shows the drop-down, Down/Up cycle it, Enter/Tab pick the
token, Escape dismisses it without closing the dialog, a second Escape
closes the dialog, and a real re-run with `database` typed into Tags and
`web1, web2` picked into Hosts came back correctly limited to exactly
those - `--tags database --limit web1,web2` reached `ansible-playbook` as
expected. Two real bugs turned up during that testing, both fixed before
calling this done - worth recording since neither was predictable from
reading `tview`'s source alone.

**Live-previewing during arrow navigation broke navigation after one
step.** The original design (see "Selecting an entry" above) called
`replaceLastToken` - which appends a trailing `", "` - on every
`AutocompletedNavigate` callback too, so arrowing through the list would
preview each candidate in the field. Confirmed live: after a single Down
press the drop-down vanished outright, before a second Down could do
anything. Root cause, traced through `inputfield.go`: `InputField`'s own
`InputHandler` captures the field's text before dispatching a keypress and,
in a deferred check, re-runs `Autocomplete()` if that text changed by the
end of the call - a generic "keep suggestions in sync" safety net,
independent of whatever our own `SetAutocompletedFunc` returned. Since our
own preview text now ended in `", "`, the *next* token was empty, our
`matchToken` correctly returned no candidates for it, and that safety net
read "no candidates" as "close the drop-down" - once per keypress. Fixed
by not touching the field's text at all on `AutocompletedNavigate` (return
`false` and do nothing else) - the drop-down's own internal highlight
still moves via `tview`'s native list handling, just not mirrored into the
field until an actual pick.

**Escape only ever dismissed once, then stopped working.** The
`autocompleteOpenNow` check (see "The Enter/Escape interception conflict"
above) recomputes `matchTags`/`matchHosts` against the focused field's
*current* text on every call - correct for avoiding the `Blur()` staleness
problem that section describes, but Escape doesn't change the field's own
text at all, so the very next check (a second Escape) still saw the same
non-empty match set and kept reporting "still open," permanently
swallowing every further Escape and making the dialog un-closable by
keyboard alone in that state. Fixed with a second, narrower bool,
`acDismissed` - set `true` only inside `SetInputCapture`'s own Escape case,
right when it lets an Escape through believing a drop-down is currently
showing; reset `false` by a shared `resetDismissed` wired to all three
fields' own `SetChangedFunc`, so any real text change (typing, or a pick)
starts a fresh interaction. `autocompleteOpenNow` now also returns `false`
whenever `acDismissed` is set, regardless of what the match check alone
would say. Not a fully precise mirror of `tview`'s own private
`autocompleteList` state (a field tabbed away from and back to without any
text change, then opened with a bare Down press, would still misreport
once) - accepted as a documented, narrow gap rather than chased further,
consistent with this codebase's existing style elsewhere.

**Cosmetic note, not a bug:** the drop-down does render past its own
field's row and can overlap the next field's label, or (for `hostsField`,
the last item before the buttons) the Cancel/Re-run buttons row and the
modal's own bottom border - confirmed live, exactly the risk flagged
above. In practice it stayed legible in every case tried (rows don't
character-overlap, just sit visually close) and was judged not worth the
complexity of reserving permanent blank space for a drop-down that isn't
showing most of the time - ordinary floating-dropdown behavior, the same
tradeoff any GUI combobox that overlaps sibling content makes. Left as-is;
revisit only if real use finds it actually confusing rather than just
busy.
