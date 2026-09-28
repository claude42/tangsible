# Host verb

## Idea

Show all information about a host - in a host-centric way. Current
information relevant to a host is scattered about multiple files (inventory,
playbook, host_vars). Show all in one place.

The view shall be deliberately host-centric in contrast to the group-centric
view of the inventory file.

## Implementation

### tangsible host

Call tangsible with new verb "host":

  tangsible host <hostname> [<playbook>] [-i ...] [-e ...]

Same passthrough-args convention as `template` (`Tangsible template.md`'s own
`[-e...]`) - `-i`/`-e`/etc. need to reach every `ansible-inventory`/
`ansible-playbook` invocation this verb makes, the same way they already do
for `run`/`template`.

This is a completely separate part, similar to the "template" verb: its own
standalone `tview.Application`, no jsonl tree, no `NewLiveTUI` - built the
same way `template.go`'s `runTemplateTUI` is.

Tangsible shows five tabs

* Summary page (contents still TBD, FQDN, IP addresses, operating system, etc.)
* All inventory groups the host belongs to
* All plays in the specified playbook (or default playbook) that would be run
  for this host
* All host variables set in host_vars/<hostname>/*
* Everything that's know about the host (variables, gathered facts, etc.) -
  basically the output of ansible-inventory --host <hostname>

#### Content summary page

Host:           hostname
FQDN:           hostname.do.main
OS:             Name, version
Distribution:   Name, Version
Architecture:   x86_64
Processor:      AMD Ryzen...
RAM:            xx GB
Virtualization: VM|Container|...|Bare Metal
IPv4:           x.x.x.x, ...
IPv6:           y:y:y::y, ...
Host key:       ssh host public key

### tangsible hosts

Call tangsible with new verb "hosts":

  tangsible hosts [<playbook>] [-i ...] [-e ...]

* Tangsible shows a list of all hosts 
* The user can navigate with the cursor. Once they select a host, the same
  view shall open as when calling tangsible <hostname> from the command line
* esc brings the user back to the host list

## Findings from discussion

Went through the existing `template` verb (`template.go`, design-docs/
Tangsible template.md) as the stated precedent before deciding anything
below, since several of its pieces are directly reusable:

* Verb dispatch: `template` is fully split off in `main.go`, before any of
  the run/rerun/role machinery (`procH`, playbook resolution, the live jsonl
  pipeline, `NewLiveTUI`) even runs. `host`/`hosts` do the same - own
  `parseHostArgs`-style arg parsing, own `os.Exit(runHostVerb(...))`, no
  shared setup with run/rerun/role.
* The five (and the hosts list's own) tabs reuse `tabs.go`'s existing
  `tabbedPane` - the same widget `template.go` and the drill-down view
  already use for their own tabs.
* `ansible-inventory --list` is already wired up in `template.go`
  (`resolveInventoryHost`/`flattenInventoryHosts`) for host resolution -
  reusable groundwork for the hosts list and the Groups tab, though it
  currently only walks group→hosts, not the reverse (see Groups tab below).

Decisions (each an open question this doc didn't originally answer):

* **Summary tab: live gathered facts, not static inventory data alone.**
  Implemented as a stub playbook (same shape as `template`'s own stub),
  `delegate_to` not needed here since nothing is rendered/written locally.
  **Correction, found only after shipping and getting a real bug report**:
  the stub does *not* use the play-level `gather_facts: true` shorthand -
  it calls `ansible.builtin.setup:` as an ordinary, explicit task instead
  (`gather_facts: false` at the play level). Originally this doc assumed
  `gather_facts: true` would transparently respect a configured fact
  cache "with zero special-casing" - wrong. Reproduced empirically: with
  `gathering = smart` in `ansible.cfg` (a common, real setup) and a fact
  cache already warm for the host - populated by *any* prior playbook run
  at all, not necessarily this one - `gather_facts: true`'s own implicit
  "Gathering Facts" task is silently skipped altogether, producing zero
  jsonl output for it: no task-start, no runner event, nothing at all,
  even though the play declares it explicitly. An explicit `setup:` task
  doesn't have this problem - it's an ordinary task, always executes,
  always fires a real event - because `gathering`'s smart-skip logic only
  ever applies to the auto-inserted implicit task the `gather_facts:` play
  keyword creates, never to a task the playbook actually writes out
  itself. Still respects a configured fact cache exactly as intended, just
  at the module's own internal level rather than by skipping the task
  pre-emptively.

  That fix, on its own, meant every Summary view paid for a live
  connection even when a perfectly fresh cache already existed - a
  regression against the original "if fact caching is activated, retrieve
  from there, otherwise retrieve from host" intent above, since the fix
  had to stop relying on ansible's own (buggy, silent) skip logic to get
  that behavior for free. Recovered explicitly instead: `fetchHostSummary`
  first runs `ansible-inventory --host <hostname>` (the same call
  "Everything known" makes - see that tab's own entry below) and checks
  it for `ansible_architecture` (part of ansible's default gather_subset,
  present whenever any real gather has ever happened) as a presence
  check for a usable cache entry. A hit is used directly, with no
  connection to the host at all - confirmed empirically that
  `ansible-inventory --host` stops showing a host's cached facts once
  `fact_caching_timeout` expires, so a hit here is guaranteed fresh
  within whatever window the user's own ansible.cfg configures, not
  arbitrarily stale. A miss (no cache configured, or none yet for this
  host) falls straight through to the live `setup:` task with no
  extra error surfaced for the miss itself. The tab shows a small
  `(from fact cache)` note when it took the fast path, nothing extra
  when it connected live - the two are a real difference in what
  "Summary" means on a given view, worth being honest about rather than
  showing identical output either way.
* **`host` and `hosts` stay two distinct verbs**, as originally specced -
  not collapsed into one verb with hostname made optional.
* **host_vars tab shows verbatim file contents**, one section per file
  (`host_vars/<hostname>/*`), not a merged/flattened key-value view -
  matches `source.go`'s existing "show the real YAML as written" convention
  for the drill-down's own `TASK:` section, preserves comments/formatting,
  and needs no variable-precedence/merge logic.
* **Groups tab shows the full transitive chain**, not just direct
  membership - a host in `web`, itself a child of `prod`, shows both `web`
  and `prod` (and `all`). Needs new code: `flattenInventoryHosts`
  (`template.go`) only walks group→hosts to build one flat host set: this
  tab needs the reverse, in effect one host→ancestor-groups index, built
  from the same `ansible-inventory --list` JSON by walking the group tree
  from `all` and recording every group whose subtree reaches this host.
* **All five tabs' data is fetched concurrently**, each on its own
  goroutine, starting the instant the host view opens - not deferred until
  a tab is first viewed, and not fetched serially. The view itself renders
  immediately with a per-tab loading placeholder; each tab's own goroutine
  populates its content and updates that tab via `app.QueueUpdateDraw` once
  its own fetch/subprocess call finishes - the same async-update mechanism
  `resolved.go`/`ansibledoc.go` already use for the drill-down view's own
  Resolved/Docs tabs, just kicked off eagerly for every tab at once instead
  of lazily per tab-open. This is what makes switching tabs never have to
  wait (the fetch is already in flight or done by the time you look) while
  also never blocking the view's own appearance on the slowest one (Summary,
  now that it needs a real ansible-playbook run).
* **Esc is inert when `tangsible host <hostname>` is invoked directly from
  the command line** - same reasoning as `template`'s own view (Esc used to
  quit there too, but that made it too easy to close the whole thing by
  reflex while just browsing tabs). Only q/Ctrl-C quit in that case. This is
  unrelated to - and doesn't change - the `hosts` verb's own list-then-detail
  flow, where Esc from the per-host view still returns to the list per this
  doc's original spec; the per-host view needs to know which of the two ways
  it was reached to pick the right Esc behavior.
* **Plays tab** reuses `progress.go`'s existing `ansible-playbook ...
  --list-tasks --list-hosts` probe, narrowed via `-l <hostname>` - inheriting
  that mechanism's own already-documented caveats (doesn't expand a dynamic
  `include_tasks:`; `--list-tasks` alone ignores `-l`/`--limit`, only
  `--list-hosts` applies it, so both flags are needed together, exactly as
  `progress.go` already does).
* **"Everything known" tab** is a new `ansible-inventory --host <hostname>`
  invocation - distinct from the `--list` call `template.go` already makes
  elsewhere in the app; nothing today calls `--host`.

## Status

Implemented (`host.go`, `host_test.go`) - both verbs, all five tabs, per
the decisions above. Live-verified via tmux against a scratch project with
nested inventory groups (`web`/`db` under `prod` under `all`) and both
host_vars shapes (`host_vars/<host>.yml` and `host_vars/<host>/*.yml`):
Summary shows real gathered facts (including the live host running this
session, correctly classified as a Container); Groups shows the full
transitive chain with correct `(direct)`/`(via ...)` annotations; Plays
shows exactly the one play/task that would run for each host; host_vars
shows both files verbatim, comments included; Everything known matches
`ansible-inventory --host` merged with host_vars (see below for a
correction on what else that can include); and `hosts`' own
list→Enter→Esc-back flow works as specced.

One real bug caught only by this live verification, not by unit tests
alone: the task-result JSON's own `ansible_facts` nests every fact under
its full `ansible_<name>` key (e.g. `ansible_fqdn`, `ansible_processor`),
not the short name (`fqdn`, `processor`) an earlier empirical check
against `debug: var: ansible_facts` had suggested - that debug output is
the templated, prefix-stripped view Jinja exposes later, a different
shape from the raw module result this feature actually reads. Fixed in
`factString`/`factStringList` (host.go) by prefixing every lookup with
`ansible_` in one place, rather than at each call site.

Three more findings, all from real usage after shipping, none caught by
either unit tests or the live verification above:

* **Groups tab's "all" annotation was confusing, not wrong.** A "(via
  <child>)" detail on the universal "all" group is always technically
  true (some child led the BFS to it) but never actually informative,
  since it's equally true of every child group regardless of which one a
  given host happens to use - it reads as a claim about *why* this host
  is in "all" that isn't real. Fixed: "all" now renders with no
  parenthetical at all; every other group still gets its own
  "(direct)"/"(via ...)" detail.
* **Summary tab silently returned nothing for a real remote host** - see
  the corrected "Summary tab" decision above for the root cause
  (`gathering = smart` + a warm fact cache silently skipping the implicit
  `gather_facts: true` task) and fix (an explicit `setup:` task instead).
  Diagnosing this from a report alone (no direct access to the reporting
  user's inventory/ansible.cfg) took several rounds of empirical
  reproduction attempts before landing on the actual cause - along the
  way, `fetchHostSummary`'s own "no result reported" error message was
  made permanently more diagnostic (reports how many jsonl events were
  parsed and for which other hostnames, if any).
* **"Everything known" tab's own doc comment overclaimed "never includes
  gathered facts."** A user noticed real facts (`ansible_bios_date`,
  `ansible_board_vendor`, ...) showing up on that tab and asked how,
  correctly doubting the earlier claim. Confirmed empirically: `--host`
  genuinely never connects to the host itself, but when `fact_caching` is
  configured in `ansible.cfg` *and* the cache already has an entry for
  that host (from any prior real gather, this session or not),
  `ansible-inventory --host` merges those cached facts into its own
  output too, same as it merges host_vars/group_vars. So this tab's
  content is: always declared inventory data, plus whatever's already
  sitting in the fact cache, if anything - not live either way, but not
  purely static either. Comment corrected in `fetchHostEverythingKnown`
  (host.go); no behavior change needed, since verbatim passthrough was
  already the right thing to do regardless of what's in the output.
  parsed and for which other hostnames, if any), which would have
  shortened this considerably had it existed from the start.

## New ideas

### Host list

* In the host list, show a green or red dot behind the host name
  * dot should be green if ansible.builtin.ping was successful for this host
  * dot should be red otherwise

* when using tangsible hosts, a similar two-pane layout shall be used (if
  there's enough horizontal space) as is currently being used for tangsible
  run's drilldown view

### Summary

* Restructure Summary page (see below)
* Extract from the previous runs the last times tangsible has run a playbook
  that included that specific host and visualize this in the Summary tab




Characteristics
===============

Host:           nirvana
FQDN:           nirvana.aw.net
OS:             Linux, 6.12.107+deb13-amd64
Distribution:   Debian, 13.7
Architecture:   x86_64
Processor:      Intel(R) Xeon(R) CPU E5-2650 v4 @ 2.20GHz
RAM:            1.9 GB
Virtualization: VM
IPv4:           10.0.1.1, 172.17.0.1, 130.185.249.9, 172.19.0.1, 172.18.0.1
IPv6:           fd99::1:29db, 2a04:92c7:2:2f4::29db

Recent
======

2026-09-10 10:12 site.yml:       ok=189  skipped=50  changed=7  unreachable=0  failed=0  warnings=3  ignored=0
2026-09-08 01:23 demo.yml:       ok=1    skipped=0   changed=9  unreachable=0  failed=0  warnings=0  ignored=0
2026-09-07 11:11 site.yml:       ok=0    skipped=0   changed=0  unreachable=1  failed=0  warnings=0  ignored=0

Keys
====

Host key (ed25519): ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIKk2zbib2TXQc0gRabiu7RmnVA9qmm+xBSf/NErF1Bp/
Host key (ecdsa):   ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAyNTYAAABBBFZraX+3826cNR7DR6yJ8aqOHmhJ8L3No52E26gAchtpuvtfcbR21CAhLxyM6xu14aaTk6+1LQG3j/TCfEKsE6I=
Host key (rsa):     ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQCliYu71VW8g7u3cMOh99JOi2uZwEoYsOP5MS0aGCTJHHWU9RaKo6LOWbTyHKQqcCq3vZcRJcv913W9KvjtGoSNkghX5sIEJgUTxyc0jFG+WA0mTTVpNX9ZExFrjr23KYnyXAHqeEZI0/
NRkKly0MnNnDZhxq/cSw1DeyZJfL7wuD1k4Us0qqhHgOUmVt8NOhGbLfxJDV0c+7BboAZEkjXB9E+Ca/S22wT0zAvQIqjOCzYacLHSehAqCaJy8bCmnS1YkPisZTRSdutqeBKFFxlPVgL0MSWGRUFGeKThtbbE/4MbrkRTPwrL/
TZwTOpN7f8VYqQYQFlLwKOJBtB5FMgl1GatHcbsB6zawTD0RFgnUWzENxooKDH8Dj3xh4E5OeimqkK6lPE3yc0qfBKPJutyQCF2rUbZ6jMqq1zNafjVIboiqK6GoguabUxSj9BCnm8lGc0wSO8UvPkN+lQo+oTPrlJI5PkhzNkrtOD11bOjsqC49X3fj+RdfXJbaplp2
SM=

### Decisions (from discussion)

* **Host list ping dots run concurrently, update lazily per row.** Every
  row's `ansible.builtin.ping` fires the instant the list itself renders -
  same "kick off eagerly, populate via `app.QueueUpdateDraw` once done"
  pattern the five detail tabs already use (`BuildHostDetailPrimitive`),
  just one goroutine per row instead of one per tab. A row shows its dot
  the moment its own ping lands, independent of every other row's timing -
  no batching, no "wait for all to finish" gate. The Summary tab drops its
  own ping line entirely now that the list already shows it per host - no
  duplicate ping for the currently-selected host.
* **"Recent" reuses `recapForHost` (recap.go) against a replayed historical
  `PlaybookState`, not a new counting mechanism.** Same replay
  `revisit.OpenRevisitEntry` already does for a saved
  `.tangsible/runs/<RunID>.jsonl` - build a fresh `PlaybookState` from the
  stored jsonl, then `recapForHost(state, hostname)` for the exact same
  counts the live recap shows, `ignored` included: the bundled callback
  plugin already emits `ignore_errors` and `TaskNode.Ignored`/`recap.go`
  already carry it end-to-end (confirmed directly against `events.go`'s
  `hasIgnoreErrors`) - an earlier assumption during this discussion that
  this was still an open gap was wrong.
* **Deliberately spans every playbook/role in `state.toml`'s `History`**,
  not just the one this invocation's own `<playbook>` argument names -
  accepted as inconsistent with `host`/`hosts`'s own single-playbook
  argument (which the Plays tab still uses) but kept anyway, since which
  playbooks have touched this host at all is itself useful information,
  more useful than a narrower but consistent view.
* **Bounded scan, not bounded results.** Finding "the last 5 times this
  host was touched" means replaying candidate invocations until 5 matches
  turn up, which could otherwise mean opening every invocation of every
  playbook ever run in the project for a rarely-targeted host. Capped at
  the 20 most recent invocations project-wide (newest-first across every
  `PlaybookHistory` entry, not 20 per playbook - `MaxHistoryPerPlaybook`
  already bounds the per-playbook side of this separately), stopping early
  once 5 host-matches are found. An invocation with no `RunID` (never
  saved, or cleared by `PruneMissingRunLogs`) is silently skipped, the same
  "best-effort, never an error" convention every other run-log consumer in
  this app already follows.
* **Two-pane `hosts` layout live-syncs the detail pane on cursor move,
  split mode only.** Moving the list cursor immediately retargets and
  refetches the detail pane's tabs for the newly-selected host - no Enter
  needed - matching the run drill-down's own split-mode live-sync
  (`showOutputWithOrigin`). Full-screen mode (narrow terminal, or
  `tangsible host <hostname>` invoked directly) keeps today's Enter-gated
  behavior unchanged; only split mode gets this. No debounce on rapid
  cursor movement for now - deliberately left unbuilt until it's actually
  felt to be a problem in live use, not guessed at up front.

## Status: New ideas

Implemented (`host.go`, `host_test.go`), all four items above. Live-verified
via tmux against a scratch inventory (three `ansible_connection=local`
hosts):

* Ping dots render green/lazily as each host's own `ansible.builtin.ping`
  lands, independent of the others' timing; confirmed colored correctly via
  `tmux capture-pane -e`, not just presence of the `●` glyph.
* `tangsible hosts` on a wide terminal opens straight into split mode,
  auto-selecting the first host with no Enter needed; moving the cursor
  (arrow keys and `n`/`N`) live-retargets the detail pane in real time, tab
  selection preserved across the hop.
* Narrowing the terminal mid-session falls back to full-screen detail for
  whatever host was last showing (Esc from there returns to the plain
  list); widening back re-enters split mode showing whatever the list
  cursor currently sits on. Both transitions verified live via
  `tmux resize-window`.
* Summary restructured into Characteristics/Recent/Keys sections
  (`uikit.SectionLabel`-headed); Recent correctly shows "no recorded runs
  found" with an empty `state.toml`, and correctly shows a real
  `ok=1 skipped=0 changed=0 ...` line once a real `tangsible run` populated
  one - confirmed by actually running one against the scratch inventory and
  reopening `tangsible host` afterward, not just via the unit tests
  (`TestCollectRecentRunCandidates`/`TestHostCountsFor`/
  `TestFormatRecentRunLines`) covering the pure logic underneath it.

Known, accepted simplifications (documented inline in `host.go`, not
chased further): the split-mode detail footer keeps saying "esc: back to
list" if a resize later drops out of split while that same detail is still
open (`TabSearchBar` has no way to update its own hint text after
construction); a mouse click landing on the detail pane's own `TextView`
content in split mode can steal real keyboard focus away from the list
until the next host selection resets it, unlike the run drill-down's own
`handleOutputViewMouse`, which goes further to prevent that specific class
of bug.

## Follow-up findings, all from live use after the above shipped

* **Dot placement reversed: leads the row, not "behind the host name" as
  originally specced.** `HostRowText` now renders `<dot>  <hostname>`, not
  the other way around - a live look at both showed leading dots easier to
  scan down a column.
* **Dot starts gray, not simply absent, while that row's own ping is still
  in flight.** `HostRowText`'s `pingKnown`/`pingOK` pair is unchanged, but
  the rendering is: gray when `!pingKnown`, green/red once it is - every
  row always shows *a* dot from the moment the list itself renders, never
  a blank gap that later gets a dot inserted.
* **The Summary tab took ~8s on a real project - traced to the "Recent"
  scan, not the live fact gather.** Reported live, then confirmed by
  reading the code: the original all-at-once `fetchRecentRuns` blocked the
  *entire* Summary tab's own single `fetch()` call on replaying up to
  `RecentRunsScanLimit` run logs before returning anything at all, even
  though Characteristics/Keys have nothing to do with that scan and were
  already sitting in memory by the time it started. Fixed by splitting the
  Summary tab's own goroutine in two: `FetchHostFacts` (facts only, same
  cost as before) renders and shows Characteristics/Keys immediately, then
  `StreamRecentRuns` - `fetchRecentRuns`'s own incremental sibling, calling
  `onMatch` per line found rather than collecting all of them first -
  appends each Recent line to the already-visible tab as it arrives, via a
  `summaryDoc` that re-renders the combined page from whichever pieces are
  known so far. `ScrollToBeginning()` is deliberately only called once,
  right after Characteristics/Keys first appear - calling it again on
  every later Recent-line update would yank the view back to the top out
  from under anyone who scrolled down while it was still streaming in.
* **A host already known unreachable (a red dot) shouldn't be reconnected
  to just to open its Summary tab.** Reported live: clicking a red-dot host
  still paid for a full, doomed `ansible-playbook`/`setup:` connection
  attempt before falling back to the cache-check's own "no usable cache"
  path. Fixed by threading the hosts list's own already-known `pingKnown`/
  `pingOK` for that host into `BuildHostDetailPrimitive` (`false, false`
  for the standalone `host` Verb, which has no ping subsystem at all) and
  on into `FetchHostFacts`'s new `skipLiveGather` parameter
  (`pingKnown && !pingOK`) - the cheap, connection-free cache-check
  (`ansible-inventory --host`) still always runs first regardless (a
  stale cached fact is still worth showing for a host that's down right
  now), only the live gather itself is skipped, replaced by a plain note
  explaining why. Live-verified against a genuinely unreachable host
  (`192.0.2.254`, a `ConnectTimeout=2` TEST-NET-1 address): the Summary tab
  now renders in well under a second instead of waiting out the SSH
  timeout.

## Second round of follow-up findings

* **The skipped-live-gather note is now just "Host unavailable."**,
  replacing the earlier full sentence - shorter reads better for what's
  otherwise a one-line status, matching "Unreachable"/"Failed"'s own
  terse, bold headings right above it.
* **Missing blank line before "Recent" whenever Characteristics was one of
  the three pre-rendered notes** (the skip-note above, or a live
  Unreachable/Failed result) - `summaryDoc.render()`'s own blank-line
  insertion between sections only works because `formatCharacteristics`'
  normal field list already ends its own last line in `\n`; none of the
  three pre-rendered strings did. Fixed by giving each of them their own
  trailing `\n` too, matching what the normal case already had for free.
* **Split mode had the same class of focus bug `livesession_mouse.go`'s
  own `handleOutputViewMouse` already fixed once for the run drill-down.**
  Reported live: once a tab was showing, ↑/↓ still moved the *list's* own
  cursor, not the tab's own content - there was no way to scroll a tab at
  all while split. Root cause: `layout()`'s own split-mode branch called
  `app.SetFocus(list)`, unlike its full-screen "detail" branch right below
  it, which already correctly focuses `detailTabs.Primitive()` - an
  inconsistency between the two layouts, not a deliberate choice. Fixed by
  making split mode focus `detailTabs.Primitive()` too, exactly matching
  full-screen mode: ↑/↓/PgUp/PgDn now scroll the active tab's own content
  in both layouts. Host-to-host navigation while split still works via
  `n`/`N` (`handleKey` intercepts those regardless of where real focus
  sits, same as it always has) and via a mouse click on the list pane
  itself (`TreeList`'s own `MouseHandler` moves real focus back onto
  `list` when clicked, reverting to `detailTabs` on the next host
  selection). Live-verified via tmux: five consecutive `Down` presses
  while viewing "Everything known" left the header reading the same host
  throughout (previously it would have hopped four rows down the list);
  `n`/`N` still hopped hosts correctly in between.

## Cosmetic pass: green for keys/headlines, lightsteelblue for values

All of this verb's own headings/labels/keys render green (`[green]`); the
values next to them render `lightsteelblue`, current-weight (no bold/dim
attribute at all) - `uikit.SectionLabel`'s own orange is untouched, still
used by the drill-down's own Task/Output/Errors/Details sections, which
have a real reason to stay color-coded (see its own doc comment) -
`host.go` gets its own `hostSectionHeading` instead of calling it.
`lightsteelblue` is one of tcell's many extended/X11 named colors (confirmed
present in `github.com/rivo/tview`'s underlying color table), not a
base-16 ANSI name - renders as a 256-color/RGB escape, not a plain
`3x`/`9x` SGR code, confirmed live (`\x1b[38;5;117m` in the actual
captured output, not a bare single-digit color code).

**Fourth attempt at this cosmetic pass overall - three earlier iterations
each got reverted after live feedback, each one covering the exact same
seven call sites, never a different set:**
1. Bold only, same color as the rest of the text throughout - "doesn't
   look like I imagined."
2. Dim (`tcell.AttrDim`/ANSI SGR 2) instead of bold - reverted for two
   reasons at once: mosh doesn't render SGR 2 at all (a real
   terminal-support gap, not a preference), and even outside mosh it
   still didn't read the way it was pictured.
3. Bright white + bold for keys/headlines, plain silver (dimmer,
   unbolded) for values - a real, deliberate two-tone split, just not the
   two tones actually wanted.

Every iteration reused the identical structural split (a key/headline
immediately followed by its value, always styled differently, never the
same treatment for both) - only the specific `[fg:bg:flags]` tag content
ever changed. The functions carrying this out were renamed once, early on
(`boldXxx` → `dimXxx`), then settled on plain names with no style word
baked in (`highlightRolePrefix`/`highlightYAMLKeys`/`highlightJSONKeys`),
specifically so a *further* restyling - this one included - needs no
rename at all, just new tag content. One substantive, durable finding
from the second iteration still matters for any future one: a bare `-` as
the *entire* flags field of a tview tag resets every attribute flag back
to baseline in one go (`github.com/rivo/tview`'s own tag parser,
`strings.go`) - which is what makes `[-]"`/`[-::-]"` closing tags safe and
generic across any of these style swaps, needing no change themselves
regardless of what the matching opening tag actually sets.

* `hostSectionHeading` replaces `uikit.SectionLabel("orange", ...)` for
  every section header host.go renders - Summary's own Characteristics/
  Recent/Keys, each host_vars file's own path, each Play's own name. The
  heading itself is `[green]`; the "=" underline stays unstyled.
* `formatCharacteristics`/`formatHostKeyLines`/`formatRecentRunLines`
  highlight each row's own label (`Host:`, `FQDN:`, ..., `Host key
  (ed25519):`, and each Recent line's own `<time> <name>:` prefix)
  `[green]`, then wrap the value itself in `[lightsteelblue]...[-]`.
* **Plays tab**: `highlightRolePrefix` highlights a task line's own
  leading "`<role> : `" prefix `[green]`, drops the rest of the line (the
  task name) to `[lightsteelblue]` - confirmed empirically (a real
  role-using playbook's own `--list-tasks` output) that ansible prefixes
  a role-sourced task with its role's name and " : ", never present for a
  task defined directly in the play (left entirely unstyled then, since
  there's no key/value split to make). Splits on the first " : " only.
* **host_vars tab**: `highlightYAMLKeys` highlights each line's own
  "key:" portion `[green]` and the rest of the line `[lightsteelblue]`,
  reusing `uikit.YamlKeyLine` (the drill-down's own Task-section regex)
  rather than that helper's own `ColorizeYAML` wrapper (orange-plus-bold) -
  same "good enough, not a real parser" caveat that regex's own doc
  comment already states.
* **Everything known tab**: `highlightJSONKeys` is `highlightYAMLKeys`'s
  own sibling for this tab's actual shape - pretty-printed JSON, not
  YAML, so a new `jsonKeyLine` regex matches a quoted `"key":` instead of
  YAML's bare `key:`. Same line-based, not-a-real-parser approach.

All four pure formatters (`highlightRolePrefix`/`highlightYAMLKeys`/
`highlightJSONKeys`/`hostSectionHeading`) have unit tests, rewritten each
time the styling changed; the whole pass was also live-verified via tmux
every time (`tmux capture-pane -e`, checking the literal SGR/256-color
codes rather than just eyeballing rendered color) against both a plain
task and a real role-using playbook.

## Fifth attempt: `lightsteelblue`, plus real coverage gaps found by using it

`lightsteelblue` replaced `steelblue` for every value color above (a
one-line rename, same tag mechanism, same seven-plus call sites) - but
using the restyled pages for real (not just the original test fixtures)
surfaced three genuine content-coverage gaps in `highlightYAMLKeys`/
`highlightJSONKeys` themselves, not just more color-swapping:

* **The heading underline now shares the heading's own color tag**
  (`hostSectionHeading`) - one `[green]...[-]` span covering both the
  label line and the `"="` line below it, rather than a colored heading
  over a plain-default underline.
* **A bare YAML list item had no key to color, so it stayed fully
  unstyled** - `- alpha`/`- [a, b]` under a list key never matches
  `uikit.YamlKeyLine` at all (no colon anywhere on the line); only a
  list's own flow-style opening on its *owning key's* line (e.g.
  `mylist: [`) was ever colored. Fixed with a second pattern,
  `yamlListItemLine` (`^(\s*-\s+)(.+)$`), tried only after
  `uikit.YamlKeyLine` has already failed to match - a mapping-shaped item
  (`- key: value`) is already handled by that regex's own optional
  leading-marker group and never reaches this one.
* **A multi-line block scalar's own continuation lines had no key to
  color either, so only the opener itself ever was** - a real,
  motivating case: `somekey: !vault |` followed by an indented, multi-line
  base64 blob (`ansible-vault encrypt_string`'s own real output shape) -
  the `!vault |` opener is `uikit.YamlKeyLine`'s own `m[4]` and was
  already colored, but every line after it is raw text with no key/list
  shape at all. Fixed by tracking state across lines
  (`blockScalarIndent`): once a key or list-item's own value matches
  `yamlBlockScalarOpener` (a bare `|`/`>`, optionally chomp-marked and/or
  tag-prefixed - `!vault` being the motivating tag, not the only one this
  matches), every following line that's blank or indented further than
  that opener is colored as its own continuation, until indentation drops
  back down - at which point normal key/list-item matching resumes
  exactly where it left off.
* **A bare JSON array element had the identical gap, mirrored**: `"web",`
  inside a `"tags": [...]` array never matches `jsonKeyLine` (no colon at
  all), so only the array's own opening `[` (part of its owning key's
  `m[4]`) was ever colored. Fixed the same way as the YAML list-item
  case: a second pattern, `jsonValueLine`, tried only after `jsonKeyLine`
  has failed, matching a bare quoted string/number/`true`/`false`/`null`
  optionally followed by a trailing comma.

**Groups tab was entirely uncolored until now** - a gap independent of
the styling-iteration history above, since this tab was never touched by
any earlier pass. `FetchHostGroups` now colors each line's own primary
group name green and its `(direct)`/`(via <group>)` detail
`lightsteelblue`, the same key/value split as everywhere else on this
verb's pages - `"all"` (which carries no detail at all, see its own
existing doc comment on why) is just the green group name alone.

All of the above (list items, block scalars, array elements, the merged
underline color, and the Groups tab) were live-verified via tmux against
a real, purpose-built scratch `host_vars` file combining a plain scalar,
a bare-scalar list, a mapping-shaped list, and a real
`ansible-vault`-shaped multi-line block scalar in one file, plus a
nested inventory group chain (`web` → `prod` → `all`) for the Groups tab -
not just the unit tests' own smaller fixtures.

claude --resume 052c0089-f402-41e9-94ec-dc001028b53b
