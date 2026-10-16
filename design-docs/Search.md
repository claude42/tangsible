# Search

## Situation

Several views now show blocks of text large enough that eyeballing them for
one string is genuinely painful:

* The main drill-down view's own tabs (`internal/session/tui.go`) - Output,
  Errors, Task definition, Resolved, Docs, Diff, Details. Details (the full
  result as pretty-printed JSON) and a large Diff tab are routinely hundreds
  of lines for a chatty module or a big file.
* `tangsible diff`'s own drill-down (`internal/diff/diff.go`) - the same
  kind of tab set, built the same way.
* `tangsible hosts`' host-detail view (`internal/host/host.go`) - Summary/
  Groups/Plays/HostVars/Everything. "Everything" is a full facts dump -
  likely the single largest blob of text anywhere in the app.
* `tangsible template`'s Rendered/Source tabs (`internal/template/template.go`).

None of these can currently be searched. This proposes one general "find
text in the tab I'm looking at" feature, built once and reused across all
four, rather than four bespoke ones.

**Scope note, up front:** this proposes search *within the currently active
tab* - the same thing `less`/`vim`'s own `/` does to a single buffer, not a
search across every tab of a session at once (e.g. "does *any* tab of this
task's drill-down contain X"), and not a search across a run's whole history
the way the tree's own existing "Contents" filter already works one level
up (see below). If what's actually wanted is broader than "search the tab
I'm looking at," that's a different, bigger feature - flagging this
explicitly since "a general full text search feature for all these cases"
could reasonably be read either way, and the rest of this document assumes
the narrower reading.

## Behavior (proposed)

* `/` opens a one-line search prompt at the bottom of the screen, scoped to
  the current tab's own text only.
* Enter runs the search: every match in the tab gets highlighted, and the
  view jumps to the first match.
* `n`/`N` jump to the next/previous match, wrapping at either end (small,
  bounded content - the same "wraparound is the natural gesture" reasoning
  `TabbedPane.Next`/`Prev` already uses for cycling tabs, `tabs.go:123-129`,
  unlike the main tree's own deliberate no-wraparound row navigation).
* Esc clears the search (removes highlights, closes the prompt if still
  open).
* Matching is case-insensitive substring, same rule the tree's own existing
  "Contents" search filter already uses (`taskMatchesSearch`,
  `Filters.md`) - one matching convention across the app, not two.
* A "no matches" / "match X of Y" indicator somewhere in the chrome (footer
  bar is the natural slot - every one of the four surfaces already has one).
* Switching tabs, or the active tab's own content changing under an open
  search (a different host/task selected, a host switched in `template`,
  an async fetch in `hosts` landing), clears the search - see "Does a
  search survive content changing under it?" below.

## Implementation (proposed)

### The core mechanism: tview's own region/highlight primitives, not hand-rolled

This app has hand-rolled tview primitives before when the built-in one
genuinely lacked something (`treelist.go`'s `treeList` replaced
`tview.List` specifically because `List.Draw()` unconditionally re-clamps
scroll to the cursor, with no way to decouple panning from selection - see
that file's own doc comment). Search doesn't need that: `tview.TextView`
already ships exactly this feature, confirmed directly against
`textview.go` (`github.com/rivo/tview@v0.42.0`):

* `SetRegions(true)` turns on region-tag parsing. Region tags use the same
  square-bracket syntax as the `[color]...[-]` tags already used throughout
  this app's output rendering, just with a quoted ID: `["match-3"]foo[""]`.
  Region IDs must match `[a-zA-Z0-9_,;: \-\.]+` - plain identifiers like
  `match-3` are fine.
* `Highlight(regionIDs...)` marks the given regions highlighted (rendered
  with foreground/background swapped); `GetHighlights()` reads the current
  set back.
* `ScrollToHighlight()` scrolls the view to bring the currently-highlighted
  region into view on the next draw - this *is* "jump to match," built in.
* `GetRegionText(id)` reads a region's own text back out, tags stripped.

The doc comment for this API says outright: *"Regions can be highlighted...
This can be used to display search results, for example."* Nothing here
needs a custom widget - this is a case where the built-in primitive already
does the job.

### A shared component, not four bespoke ones

All four surfaces already funnel through the identical combination of
`uikit.TabbedPane` (`internal/uikit/tabs.go`) plus one `tview.TextView` per
tab - confirmed directly at each call site (`tui.go:429`, `diff.go:699`,
`host.go:991`, `template.go:350`). That's the strongest argument for one
shared piece of logic rather than four copies. Proposed: a new
`internal/uikit` type - name TBD, `TextSearch` for now - that:

1. Takes the tab's own plain text (before `[color]`/region tags) and a
   case-insensitive query, and produces (a) that text with
   `["match-N"]...[""]` region tags wound around every match, ready to feed
   straight into `SetText`, and (b) the ordered list of match IDs.
2. Wraps `Highlight`/`ScrollToHighlight`/current-match-index bookkeeping
   into `Next()`/`Prev()` methods - deliberately named to match
   `TabbedPane`'s own convention (`tabs.go:128-129`), for the same reason
   `RevisitStatusLabel`/`RevisitStatusColor` stay named consistently with
   each other: readers already primed by one name shouldn't have to learn a
   different verb for the sibling concept.

One real wrinkle: every tab's content already carries this app's own
`[color]...[-]` tags (`tview.Escape()`'d text plus inline color, per
CLAUDE.md's "Color in the output view" section) - so match-finding can't
run against the raw tagged string directly (a match could straddle a tag
boundary, or the literal characters of a tag could accidentally match the
query). Proposed approach: search against a tags-stripped version of the
string for finding match *offsets*, then walk the original tagged string
inserting `["match-N"]`/`[""]` pairs at the corresponding raw-text
positions. Tags are simple linear on/off markers (they don't nest), so this
should compose cleanly with the existing color tags as long as insertion
points never land inside another tag's own bracket span - worth a dedicated
unit test (a match that happens to span, or sit right next to, an existing
color tag boundary) once this is built, not just eyeballed.

### `TabbedPane` needs one new accessor

`ActiveName()` exists (`tabs.go:116`) but nothing lets a caller get the
*content* of the active tab generically - each of the four call sites
currently only knows this from its own locally-held slice at construction
time. Proposed: `TabbedPane.ActiveTextView() (*tview.TextView, bool)`
(`ok=false` if there are no tabs, or the active one isn't a `*tview.TextView`
- every tab at all four call sites currently is one, but no need to assume
that structurally). This is what lets the shared search controller be
driven generically ("search whatever's currently frontmost") instead of
every call site re-deriving which widget that is.

### Per-surface wiring differs in one important way

The four surfaces don't rebuild their `TextView`s the same way, and that
matters for "does a search survive":

* **Drill-down (`tui.go`) and `diff.go`**: every host/task navigation
  rebuilds all tabs' `TextView`s from scratch (`renderOutputTabs`,
  `tui.go:610-619`; `showDiffOutput`, `diff.go:715-724`) - the widget
  instance itself doesn't survive navigation, only the tab *position* does.
* **`host.go` and `template.go`**: tabs are built once
  (`BuildHostDetailPrimitive`, `host.go:976-1024`;
  `RunTemplateTUI`, `template.go:340-360`); content arrives later via a
  plain `SetText` call on the same long-lived `TextView` (an async fact
  fetch landing, or `template`'s own `h` "change host" - `template.go:354`
  advertises this - re-rendering in place).

Proposed rule, for one behavior to explain rather than two: **any change to
the active tab's own content clears an active search**, regardless of
whether that change came from a full widget rebuild or an in-place
`SetText`. Simpler to state and to implement (one hook: "search state gets
reset whenever this content is (re)written"), even though the host/template
case could technically re-run the same query against the new text instead
of dropping it, since it's arguably "the same document, updated" rather
than "a different document." Flagged as an open question below rather than
decided here.

### Keybinding

`/` is confirmed genuinely free on all four surfaces today: unbound
entirely in `host.go`/`template.go`/`diff.go` (checked directly - no
`Rune() == '/'` case exists in any of them), and in `tui.go` it's bound
*only* to the main tree's own row-filtering search dialog
(`tui.go:2391`), which is unreachable once a drill-down/tab view is
already frontmost - so there's no collision even there. It also reuses a
mnemonic the user already has from that existing filter, even though the
mechanism underneath is different (that one decides which tree *rows* show;
this one jumps within text that's already fully visible).

**`n`/`N` is not free, and this needs an explicit decision.** Two of the
four surfaces already bind `n`/`p` to something else while a tab view is
open:

* Drill-down (`tui.go:2279-2284`): `n`/`p` (and `N` as an alias for `p`)
  step to the next/previous *task* for the current host. The comment
  there already acknowledges the tension: *"Shift-N is a plain alias for
  'p' here... echoing vim's own n/N... even though n/p here aren't
  search-related."*
* Host detail (`host.go:1254-1259`): `n`/`p` step to the next/previous
  *host*.

(`diff.go` and `template.go` have no existing `n`/`p` binding, so those two
are free.)

Proposed resolution: make `n`/`N` **context-sensitive** - while a search is
currently active in the frontmost tab (a live query with at least one
highlighted match), they mean next/previous match; otherwise they fall back
to their existing task-hop/host-hop meaning. This isn't a new pattern for
this app - `SetInputCapture` already reinterprets keys by mode constantly
(`viewingOutput`/`filterDialogOpen`/`searchDialogOpen` branches all do
exactly this) - but it is a real, noticeable behavior change (the same
physical key doing two different things depending on invisible state)
worth explicit sign-off rather than assuming. The alternative is picking
entirely different keys for match-navigation and leaving `n`/`p` alone
everywhere, at the cost of a less familiar (non-vim) gesture.

## Open questions

1. **Scope**: confirm "search within the tab I'm currently looking at" is
   the intended scope (see the Situation section's own note) - not a
   cross-tab or cross-run search.
2. **`n`/`N` collision**: context-sensitive reuse (proposed above) vs.
   different dedicated keys for match-navigation, leaving task-hop/host-hop
   alone unconditionally.
3. **Does a search survive content changing under it?** Proposed: no,
   always clear (one rule, not two) - but the host/template case could
   plausibly re-run the same query instead, since it's often "the same
   document, updated" rather than "a different document." Worth deciding
   explicitly rather than defaulting silently.
4. **Where does the "match X of Y" / "no matches" indicator live?** Footer
   bar is the natural slot (all four surfaces have one) - would it replace
   the existing keybinding-hint text while a search is active (same
   "temporarily swap the footer" precedent `splitBottomBarText` already
   uses in `tui.go`'s two-pane mode) or share space with it some other way?
5. **Regex, or plain substring only?** The tree's own existing search
   filter is deliberately plain substring, "not chased further" per this
   app's own established style elsewhere. Proposing the same restraint
   here unless there's a concrete reason text search specifically needs
   more.
6. **Cross-tab match indicator**: even scoped to "search only the active
   tab" (per the scope note above), would it be worth a small hint that
   *other* tabs also contain the term - e.g. a marker in the tab bar itself
   - without actually searching them until visited? A possible later
   extension, not proposed as part of this first cut, but worth flagging
   since it's the natural next step if the narrower scope feels
   incomplete in practice.

## Proposed phasing

1. The shared `uikit` component (tag-aware match-finding + region-tag
   insertion + `Next`/`Prev`/highlight bookkeeping) plus the
   `TabbedPane.ActiveTextView()` accessor, with direct unit tests -
   independent of any wiring, testable without a live `tview.Application`.
2. Wire it into the drill-down view (`tui.go`) first - highest-traffic
   surface, and the one with the trickiest "content rebuilt on every
   navigation" behavior to get right (see "does a search survive," above).
3. `diff.go` - same shape as the drill-down, should mostly fall out of
   phase 2's design once that's settled.
4. `host.go` and `template.go` - simpler "long-lived TextView, content
   arrives later" case; likely the smallest of the four once the shared
   component exists.

Also worth a line in `Keyboard-shortcuts.md` once implemented, per that
file's own role as this app's keybinding source of truth.

## Answers to open questions

Hi, here are my answers to all the open questions I could find in the
document

Your scope is exactly what I had in mind - search in a single tab, not across
tabs.

n/N vs. n/p - I think we should only use n/N and forget about the p - also in
the tree view search and host detail. Let's make this consistent internally and to external
applications.

I like the proposal to make this context sensitive. In case it might be
confusing I will be the first to notice and then we can think of a different
approach.

As of now, I would say "search does not survive content change uner it".

Location of "match X of Y". Good question. You didn't really discuss exactly
where the search field itself would live (you just wrote "at the bottom of
the screen"). I could see two versions
* either we open an additional bar after pressing / on the second to lowest
  row of the screen and this would house both the search input box as well as
  x/y indicator.
* or we re-use the current bottom bar to house both. In either case I would
  use a distinct color (maybe black on yellow) to visualize the mode change
  (and use the same coloring to highlight matches to make the connection
  clear.
Which option would you choose or do you have different suggestions?

Plain substring search, no regex

No cross-tab match indicator

## Follow-up (Claude, 2026-08-25)

Everything's settled except the bar-placement question, which was a direct
"which would you choose" - answering that below, plus one scope
confirmation on the n/N change.

### n/N, dropping p entirely

Confirming the exact scope, since it's bigger than just this new feature:
today there are **three** separate `n`/`p` (`N` as an existing alias for
`p`) pairs, not the two this document originally listed:

1. Main tree, bare (not drilled down): `navigateMainTask` (`tui.go:2376-2383`).
2. Drill-down/output view: `navigateOutputTask` (`tui.go:2279-2284`) - the
   one this document already called out.
3. Host detail: `navigateHostDetail` (`host.go:1254-1259`) - also already
   called out.

All three already have the identical shape: `n` next, `p` *or* `N`
previous. Per your answer, all three drop the plain `p` case, keeping only
`n`/`N` - a small, mechanical change (one `case` line removed from each of
the three `switch` statements), but it does mean the footer/bottom-bar hint
text that currently reads "p/n: prev/next task" in the tree and drill-down
(confirmed live via tmux earlier this session) needs updating to "n/N: ...",
and `CLAUDE.md`'s own "`n`/`p` to hop to the next/previous task" line needs
the same fix - flagging both now so neither gets missed as a leftover
during implementation.

### Bar placement: recommending option (b), reusing the bottom bar

Leaning toward reusing the existing bottom bar rather than adding a new
row, for a few reasons:

* **Vertical space is scarce and this app is already deliberate about not
  spending it carelessly** - the same economy that motivated things like
  `Morehosts.md`'s summary-count fallback when a task's own host list
  doesn't fit. A fourth permanently-reserved chrome row (on top of
  header/tab-bar/footer) is a real, ongoing cost even on the screens where
  search is never used, whereas reusing the footer costs nothing until a
  search is actually active.
* **There's already a shipped precedent for exactly this** -
  `splitBottomBarText` in `tui.go` already swaps the bottom bar's entire
  meaning depending on mode (two-pane split vs. normal). Reusing that same
  "the footer's meaning changes with what's currently active" pattern
  means no new layout mechanism, just a new case of one that already
  exists.
* **It matches `less`/`vim`/`tmux` copy-mode's own search UX** - all three
  take over the terminal's actual last line for the `/` prompt rather than
  opening a new one, so this also happens to be the more familiar gesture,
  not just the cheaper one.

The one real implementation wrinkle: the footer is currently a plain,
non-interactive `tview.TextView`; typing a live query needs an actual
`tview.InputField` (the tree's own `searchInput`, `tui.go`, is the existing
example of genuine text entry in this app - hand-rolling character-by-
character editing instead would be reinventing what `InputField` already
does correctly). Proposed: swap a real `InputField` into the footer's Flex
slot while composing the query, then swap back to a plain `TextView`
showing `match X of Y` (or "no matches") plus the remaining keybinding
hints once Enter is pressed - visually it reads as "the bottom bar became a
search box, then became a status line," with no new row ever added. Same
technique at all four call sites (`tui.go`/`diff.go`/`host.go`/
`template.go`), since all four already build their own footer the same way.

One small sanity check on the color, not an objection: black-on-yellow is
distinct from the existing selected-row convention (`PureBlack` on
lightgray - close in spirit as an "inverted/highlighted" scheme, but a
different enough color pair that the two won't read as the same thing),
which is good. The one thing worth being deliberately aware of: yellow
already means the "Changed" outcome throughout this app's existing
green/yellow/teal/red/maroon palette (tree rows, host rows, the drill-down
status line). Using it again here is chrome-background, not
outcome-foreground-text, so it's a different visual pattern, not a literal
collision - but worth actually looking at once this is built (a search
active inside an Output tab that also happens to show `"changed": true`
nearby) rather than assuming it reads cleanly just because the two uses are
technically different.

Last implementation detail worth flagging now rather than discovering
later: "highlight every match, and indicate which one is current" is two
slightly different visual states, not one. Proposed default: every match
gets the explicit black-on-yellow color tag (not just tview's own
automatic highlight-inverts-existing-colors behavior, which would give
each match whatever its *surrounding* text's colors happen to invert to,
not a consistent yellow); the *current* match is additionally passed to
`Highlight()`/`ScrollToHighlight()` so it's the one that scrolls into view
- happy to make the current match visually louder still (e.g. genuinely
inverted on top of the yellow) if plain "it's the one currently on screen"
doesn't read clearly enough once this is live, but proposing the simpler
version first.
