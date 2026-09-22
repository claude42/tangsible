# Error output on failure

## Status

Implemented.

## Situation

A real user report: when a generation ends in a genuine failure (not a
benign "some host(s) unreachable" run, not a user-requested interrupt),
the live TUI only ever showed "Playbook failed (exit code N)" - no hint
of *why*. The actual `ansible-playbook` stderr was always collected
(`runner.StreamStderr`), but only ever printed *after* the whole session
ends and the alternate screen is torn down - printing to the real
terminal while the TUI still owns it would corrupt the display. So the
one piece of information that actually explains the failure was hidden
behind quitting the tool.

Motivating example: `strategy: free` hard-refuses any module whose
action plugin bypasses the host loop (`ansible.builtin.pause`,
`ansible.builtin.add_host` - see `design-docs/StrategyFree.md`'s own
note). The resulting `[ERROR]:` message, plus the few lines of source
context `ansible-playbook` prints alongside it, used to be completely
invisible until the user pressed `q`.

## Decision

Show it inline, as ordinary rows appended right below the existing red
"Playbook failed" status row - not a modal. Considered and rejected: a
modal dialog would need a new dismiss interaction for something that's
purely passive information, and (since the block is just plain terminal
text, not an overlay) the terminal's own native mouse-selection/copy
already works on it for free - a modal would complicate that for no real
benefit. This mirrors the recap ("Summary") section's own existing
mechanism: more rows in the same scrollable list, nothing new to learn.

Gated on `uikit.GenuineFailure` (the same predicate the auto-jump-to-
failed-host feature already uses) - a clean/benign-unreachable/user-
interrupted run never grows this block.

## Mechanism

`runner.RunOneGeneration` already computed `exitCode` and stored it into
a `*atomic.Int32` *before* `processDone.Store(true)`, specifically so
`rebuild()` (which only reads `exitCode` once it observes `processDone`
true) can see it safely with no separate lock - Go's whole-program
sequential consistency for atomics is what makes that ordering sufficient
on its own. `lastStderr *atomic.Pointer[[]string]` is stored the exact
same way, right alongside it, and threaded through `NewLiveTUI`/
`NewRequestRerun` the same way `exitCode` already is. `NewRequestRerun`'s
own spawn-failure path (`fail()`, a `startAtPlay` that doesn't resolve,
or `SpawnGeneration` itself failing) stores its own Go `error`'s message
there too, so the same block also covers a failure that never reaches
`ansible-playbook` at all, not just an ansible-side one.

No reset is needed when a rerun starts: `processDone.Store(false)`
already hides `rebuild()`'s error-output block regardless of whatever
`lastStderr` still holds, and the next generation's own completion always
overwrites it before `processDone` goes true again - the same "nothing to
reset, the gate already covers it" reasoning `exitCode` itself relies on.

`internal/session/errorrows.go`'s `(*liveSession).errorOutputRows(width)`
builds the actual rows: a blank divider, a plain heading ("Error
output:", styled like the recap section's own "Summary" heading - bold
white, not a failure color, so it's never mistaken for the stderr text
itself), then the collected lines. Two things happen to the lines before
they become rows:

- `runner.FilterRedundantWarnings` - the same filter already applied to
  the post-quit dump - drops any `[WARNING]:`-prefixed line, since that's
  always already visible via the tree's own per-host warning marker.
- `uikit.WrapText(line, width)` - `TreeList`'s own rows are single-line
  and don't wrap (see its own doc comment), so an overlong line (a real
  `[ERROR]:` message is typically one long sentence) would otherwise run
  off the edge of the terminal and be silently clipped. Deliberately
  asymmetric: a line that already fits `width` is left completely
  untouched, whitespace included: `ansible-playbook` also prints short,
  whitespace-significant context lines alongside a real error (a source
  snippet's own indentation, a `^` caret pointing at one column), and
  reflowing those the same way a long sentence gets word-wrapped would
  silently destroy the exact alignment they exist to show. Only a line
  that's actually too long gets re-wrapped via `strings.Fields`, which is
  fine for prose but does not preserve internal whitespace - acceptable
  there since prose has none worth preserving.

Row text still goes through `tview.Escape` before becoming a row, same as
every other row in this tree - stderr is arbitrary, untrusted-shape text,
and a literal `[` (an `[ERROR]:`/`[WARNING]:` prefix is exactly this
shape) would otherwise be misparsed as a color tag by `tview.Print`.

`errorOutputHeadingRowID`/`errorOutputLineRowID` are separate int-based
types (not `uikit.StatusDividerRowID`'s shared zero-size sentinel),
mirroring `recapHeadingRowID`'s own reasoning in `recap.go`: distinct
types are what let `rebuild()`'s identity-based selection-restoration
still tell every row apart, even though several of them share a small
underlying int range. None of these rows carry a `Selected` callback, so
`NextInteractiveRow`'s existing "skip anything with no selected callback"
rule already makes Up/Down/j/k jump over the whole block for free -
exactly the same behavior the status/recap-heading rows already have, no
new code needed for it.

## Verification

Live, via tmux: the motivating `strategy: free` + `pause`-in-a-handler
case, confirming the block appears with correct wrapping and preserved
source-snippet alignment, correct heading/body styling (bold white
heading, plain body - not red), that it's absent for a clean run, that it
correctly disappears the instant an in-session rerun (`r`) starts and
reappears with fresh (not duplicated/stale) content once that rerun
finishes, and that quitting still prints the identical post-quit stderr
dump unaffected. `go test ./...` and `go test -tags e2e ./...` both pass.
Unit tests: `internal/uikit/wraptext_test.go` (the wrap/no-wrap
asymmetry, word-boundary wrapping, an unbroken overlong single word) and
`internal/session/errorrows_test.go` (nil/empty/filtered-to-nothing
cases, heading/body row shape, literal-bracket escaping) - both testable
in complete isolation, no live `*tview.Application` needed.

## Considered, not built

- A one-line summary appended directly onto the "Playbook failed" status
  row itself, using the first real error line. Dropped once a concrete
  mockup showed that row is just as prone to overflowing/clipping as the
  raw stderr was - the wrapped block below already covers this better.
- Reusing the drill-down's tab machinery (search/copy for free) for a
  dedicated "process error" view opened on demand. More machinery than
  the actual problem ("I can't see why it failed without quitting")
  needed; revisit only if the plain-rows version turns out to feel
  insufficient in practice.
- Seeding `lastStderr` for a *revisited* failed run from its own saved
  `<RunID>.stderr` file (`config.WriteRunStderr` already writes it, no
  reader exists yet) - the same display would work unchanged, but this
  session's report was about the live case specifically; a natural,
  low-risk follow-on if wanted later.
