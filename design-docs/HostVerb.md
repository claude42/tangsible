# Host verb

## Idea

Show all information about a host - in a host-centric way. Current
information relevant to a host is scattered about multiple files
(inventory, playbook, host_vars). Show all in one place, deliberately
host-centric in contrast to the group-centric view of the inventory file
itself.

## The two verbs

```
tangsible host <hostname> [<playbook>] [-i ...] [-e ...]
tangsible hosts [<playbook>] [-i ...] [-e ...]
```

Same passthrough-args convention as `template` - `-i`/`-e`/etc. reach
every `ansible-inventory`/`ansible-playbook` invocation either verb
makes. Both are standalone programs (`internal/host/host.go`): their own
`tview.Application`, no jsonl tree, no `NewLiveTUI`, same precedent as
`template`.

`host <hostname>` shows exactly one host's five-tab detail view for the
process's lifetime; `Esc` is inert there (only `q`/Ctrl-C quit), matching
`template`'s own reasoning - too easy to close the whole thing by reflex
while browsing tabs.

`hosts` lists every inventory host and opens the identical detail view
for whichever one is selected - see "Host list and layout" below for
what that actually looks like today (a ping-annotated, optionally
two-paned list, not a plain picker with `Esc` as its only extra
behavior). `Esc` from a full-screen detail view returns to the list
there, the one behavioral difference from standalone `host`.

## The five tabs

**Summary** - live gathered facts, restructured into three sections (see
its own section below).
**Groups** - the host's full transitive inventory-group chain.
**Plays** - which plays/tasks would run for this host, from the target
playbook.
**host_vars** - verbatim contents of every `host_vars/<hostname>` file.
**Everything known** - `ansible-inventory --host <hostname>`'s own
output, merged with whatever facts the Summary tab itself gathered (see
"Everything known" below).

All five are fetched concurrently, each on its own goroutine, the
instant the view opens - not deferred until a tab is first viewed. The
view itself renders immediately with a per-tab loading placeholder; each
goroutine populates its own tab via `app.QueueUpdateDraw` once its own
fetch/subprocess call finishes, so switching tabs never has to wait.

Every tab is now syntax-highlighted rather than plain text: a "key"
(a field label, a YAML/JSON key, a role name prefix, a group name) is
green; the "value" next to it is `lightsteelblue`. See "Coloring" below
for the exact scope and the regex-based, not-a-real-parser mechanics
behind it.

## Summary tab

Three sections, each a plain heading + `"="`-underline (both in the same
green tag, `hostSectionHeading`) followed by its own content:

```
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
IPv4:           10.0.1.1, 172.17.0.1
IPv6:           fd99::1:29db

Recent
======

2026-09-10 10:12 site.yml: ok=189  skipped=50  changed=7  unreachable=0  failed=0  warnings=3  ignored=0
2026-09-07 11:11 site.yml: ok=0    skipped=0   changed=0  unreachable=1  failed=0  warnings=0  ignored=0

Keys
====

Host key (ed25519): ssh-ed25519 AAAA...
```

**Characteristics** (`formatCharacteristics`) is a fixed field list -
Host/FQDN/OS/Distribution/Architecture/Processor/RAM/Virtualization/
IPv4/IPv6 - built from a live `ansible_facts` gather, not static
inventory data alone. `FetchHostFacts` gets that gather:

1. First checks whether a fresh fact-cache entry already covers this
   host (`ansible-inventory --host`, gated on the presence of
   `ansible_architecture` as a canary key) and uses it directly, with no
   connection to the host at all, if so - shown with a `(from fact
   cache)` note. Confirmed empirically that `ansible-inventory --host`
   stops showing a host's cached facts once `fact_caching_timeout`
   expires, so a hit here is guaranteed fresh within whatever window the
   user's own `ansible.cfg` configures.
2. On a miss, runs `HostSummaryStubYAML` (`- hosts: all\n  gather_facts:
   false\n  ignore_unreachable: true\n  tasks:\n    - name: gather
   facts\n      ansible.builtin.setup:\n`) - a real, explicit `setup:`
   task, deliberately **not** the play-level `gather_facts: true`
   shorthand. That shorthand's own implicit "Gathering Facts" task is
   silently skipped with zero jsonl output at all when `gathering =
   smart` and a fact cache is already warm for the host (confirmed
   empirically, a real reported bug) - `gathering`'s smart-skip logic
   only ever applies to that auto-inserted implicit task, never to a
   task the playbook actually writes out itself, which is why the
   explicit `setup:` task doesn't have the problem.
3. `skipLiveGather` (`pingKnown && !pingOK` from `hosts`' own ping dots,
   below - always `false` for standalone `host`, which has no ping
   subsystem) skips step 2 outright for a host already known
   unreachable, showing "Host unavailable." instead of paying for a
   second, doomed connection attempt. Verified against a genuinely
   unreachable host (a `ConnectTimeout=2` TEST-NET-1 address): the tab
   now renders in well under a second instead of waiting out the SSH
   timeout. The cache-check in step 1 still always runs regardless - a
   stale cached fact is still worth showing for a host that's down right
   now.

A live gather's own `Unreachable`/`Failed` result (as opposed to a
`skipLiveGather` skip) renders its own maroon/red heading in place of
the field list, same as any other Unreachable/Failed status elsewhere in
the app.

**Recent** lists the last few times any playbook or role recorded in
`state.toml` touched this host, newest first, with the exact same
`ok`/`skipped`/`changed`/`unreachable`/`failed`/`warnings`/`ignored`
counts the live recap shows (`hostCountsFor`, mirroring
`internal/session/recap.go`'s own `recapForHost`). Deliberately spans
every playbook/role ever recorded, not just the one this invocation's own
`<playbook>` argument names - inconsistent with the Plays tab's own
single-playbook scope, kept anyway since which playbooks have touched
this host at all is itself useful information. Bounded scan, not bounded
results: replaying invocations until `RecentRunsShowLimit` (5) matches
turn up could otherwise mean opening every invocation of every playbook
ever run in the project for a rarely-targeted host, so only the
`RecentRunsScanLimit` (20) most recent invocations project-wide (newest
first, across every playbook/role) are ever even looked at. An
invocation with no `RunID` (never saved, or pruned) is silently skipped.

Streamed in via `StreamRecentRuns`, one line at a time, *after*
Characteristics/Keys already render - not collected all at once first.
Replaying up to 20 run logs can genuinely take several seconds on a
project with a long history (measured ~8s on real hardware), and
Characteristics/Keys have nothing to do with that scan at all; blocking
the whole tab on it made it feel hung for no reason.

**Keys** lists every SSH host key type gathered (`ed25519`/`ecdsa`/
`rsa`/`dsa`, modern to legacy) - omitted entirely if none were gathered.

## Host list and layout (`tangsible hosts`)

Each row shows a connectivity dot leading the hostname (`<dot>  <name>`,
not trailing it): gray while that row's own `ansible.builtin.ping`
(`HostPingStubYAML`) is still in flight, green once it succeeds, red
otherwise. Every row's ping fires concurrently the instant the list
renders and updates independently the moment its own result lands - no
batching, no "wait for all to finish" gate.

On a wide enough terminal (`uikit.SplitMinTotalWidth`, the same
threshold the run drill-down's own two-pane layout uses), the list and
the detail view show side by side instead of full-screen, auto-selecting
whatever the list cursor already sits on with no `Enter` needed. Moving
the list cursor (arrows, `n`/`N`) immediately retargets and refetches the
detail pane for the newly-selected host - genuine live-sync, not just a
preview. Narrowing the terminal mid-session falls back to full-screen
detail for whatever host was last showing (`Esc` from there returns to
the plain list); widening back re-enters split mode showing whatever the
list cursor currently sits on. A dedicated resize-watcher goroutine
drives this, mirroring `tui.go`'s own `startResizeWatcher` - this view
has no other ticker to piggyback a periodic re-layout on.

Keyboard focus in split mode sits on the active tab's own content
(`detailTabs.Primitive()`), matching full-screen mode exactly - `↑`/`↓`/
PgUp/PgDn scroll the tab, not the host list. Host-to-host navigation
while split works via `n`/`N` (intercepted centrally regardless of where
real focus sits) and via a mouse click on the list pane itself (which
moves real focus back onto the list for as long as you keep navigating
there, reverting to the detail pane on the next host selection).

Known, accepted simplifications, not chased further: the split-mode
detail footer keeps saying "esc: back to list" if a resize later drops
out of split while that same detail is still open (`TabSearchBar` has no
way to update its own hint text after construction); a mouse click
landing on the detail pane's own content in split mode can steal real
keyboard focus away from the list until the next host selection resets
it, unlike the run drill-down's own `handleOutputViewMouse`, which goes
further to prevent that specific class of bug.

## Groups tab

Shows the host's full transitive group chain, not just direct
membership - a host in `web`, itself a child of `prod`, shows both `web`
and `prod` (and `all`), each annotated `(direct)` or `(via <child>)`.
`all` gets no annotation at all: a "(via `<child>`)" detail there is
always technically true (some child led the BFS to it) but never
actually informative, since it's equally true of every child group
regardless of which one this specific host uses.

Each line's own group name is green, its `(direct)`/`(via ...)` detail
`lightsteelblue` - the same key/value split used everywhere else on
these pages (`all`, with no detail at all, is just the green name alone).

## Plays tab

Reuses the `--list-tasks --list-hosts` probe already built for the
top-bar progress indicator, narrowed via `--limit <hostname>` (both
flags are required together - `--list-tasks` alone ignores `--limit`).
Inherits that mechanism's caveat that a dynamic `include_tasks:` won't
expand. A role-sourced task's own leading `<role> : ` prefix (confirmed
empirically against real `--list-tasks` output) is highlighted green,
the task name itself `lightsteelblue`; a plain play-level task (no such
prefix) renders unstyled.

## host_vars tab

Shows every `host_vars/<hostname>` file verbatim, one section per file -
not a merged/flattened key-value view. Preserves comments/formatting and
needs no variable-precedence/merge logic. Each line's own `key:` portion
is highlighted green, the value `lightsteelblue` - including a bare
block-sequence list item with no key of its own (`- alpha`, previously
left fully unstyled since it has no colon at all to match against) and a
multi-line block scalar's own continuation lines (an
`ansible-vault`-shaped `somekey: !vault |` followed by an indented,
multi-line blob - previously only the `!vault |` opener itself was ever
colored, every line after it was raw, unstyled text). The block-scalar
case is tracked across lines: once a key/list-item's own value is a bare
block-scalar indicator (`|`/`>`, optionally chomp-marked and/or
tag-prefixed), every following line that's blank or indented further
than that opener is colored as its own continuation, until indentation
drops back down.

## Everything known tab

`ansible-inventory --host <hostname>`'s own pretty-printed JSON output,
highlighted the same way (a quoted key green, its value - including a
bare array element with no key of its own, e.g. `"web",` inside a
`"tags": [...]` array - `lightsteelblue`).

This call never connects to the host itself - it only reads local state
(inventory/group_vars/host_vars) plus whatever's already sitting in the
configured fact cache, if `fact_caching` is set up *and* the cache
already has a fresh entry for this host from *any* prior real gather,
this session or not. So on its own, this tab's content is "declared
inventory data, plus whatever's already cached, if anything" - not live
either way, but not purely static either.

That alone produced a real, confusing inconsistency: Summary (above) can
show real gathered facts even when the fact cache started out completely
empty, because it runs its own live `setup:` gather directly and reads
the result off the event stream, never through the cache. Everything
Known's own `ansible-inventory --host` call is fast and local, and
almost always finishes *before* Summary's live gather does - so within a
single view-open, Everything Known could show no facts at all while
Summary, moments later, showed a full set. Fixed by having Summary's own
goroutine hand its resolved `ansible_facts` over to Everything Known's
goroutine once known (a buffered `summaryFacts` channel, always sent to
exactly once, `nil` included, so Everything Known's own receive can
never block forever on a Summary that errored or hit its own
`skipLiveGather` case). Everything Known still renders immediately from
its own `ansible-inventory --host` call at unchanged speed, then
re-renders a second time once those facts arrive, merged on top
(`mergeFactsIntoEverythingKnown`, facts winning on any key collision).

Two alternatives were considered and rejected: making Everything Known's
own fetch wait for Summary to finish first would have kept it a strictly
verbatim dump, but only helps when `fact_caching` is configured at all,
and makes the tab noticeably slower whenever Summary needs a live
gather; leaving the race undocumented-but-accepted was rejected as
genuinely confusing in practice.

## Coloring

Every "key" (a field label, a YAML/JSON key, a role-name prefix, a group
name, a section heading) renders `[green]`; the "value" next to it
renders `[lightsteelblue]`, current weight - no bold/dim attribute
anywhere. `uikit.SectionLabel`'s own orange-coded headings (the run
drill-down's Task/Output/Errors/Details sections) are untouched; `host.go`
has its own `hostSectionHeading` instead of calling that helper, since
these pages have no outcome palette of their own to avoid colliding
with.

This landed here after several rounds of live-feedback-driven iteration
(plain bold; dim, reverted because mosh doesn't render `SGR 2` at all;
bright-white-bold-labels-with-plain-silver-values; `steelblue`; finally
`lightsteelblue`) - the specific colors changed each time, never the
underlying mechanism or which seven-plus call sites it applied to. One
durable, reusable finding survived every iteration: in a `tview` tag
(`[fg:bg:flags]`), a bare `-` as the *entire* flags field resets every
attribute back to baseline in one go (confirmed by reading
`github.com/rivo/tview`'s own tag parser) - which is what makes a plain
`[-]`/`[-::-]"` closing tag safe and generic regardless of which single
flag or color the matching opening tag actually set.

## Implementation notes

* `host`/`hosts` are dispatched fully separately from `run`/`rerun`/
  `role`, before any of that shared machinery runs - own arg parsing, own
  `os.Exit`, no shared setup.
* The task-result JSON's own `ansible_facts` nests every fact under its
  full `ansible_<name>` key (`ansible_fqdn`, `ansible_processor`, ...),
  not the short name a templated `debug: var: ansible_facts` view would
  suggest - that's a different, prefix-stripped shape Jinja exposes
  later. `FactString`/`FactStringList` add the `ansible_` prefix in one
  place rather than at each call site.
* `hosts`' own list and the standalone `host <name>` view share one
  `BuildHostDetailPrimitive` - they differ only in what `Esc` does and
  (for `hosts`) whether ping-dot state feeds into `skipLiveGather`, both
  wired by the caller, not by that function itself.

claude --resume 052c0089-f402-41e9-94ec-dc001028b53b
