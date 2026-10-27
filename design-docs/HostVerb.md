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
**host_vars** - the host's own inventory variables, then verbatim contents
of every `host_vars/<hostname>` file.
**Everything known** - `ansible-inventory --host <hostname>`'s own
output, merged with whatever facts the Summary tab itself gathered (see
"Everything known" below).

All five are fetched concurrently, each on its own goroutine, the
instant the view opens - not deferred until a tab is first viewed. The
view itself renders immediately with a per-tab loading placeholder; each
goroutine populates its own tab via `app.QueueUpdateDraw` once its own
fetch/subprocess call finishes, so switching tabs never has to wait.

`hosts`/`inventory` (not the standalone `host` Verb, which only ever
shows one host once) keep a `HostDetailCache` for the whole session,
keyed by hostname: once a tab's fetch finishes, its rendered text is
stored there, and the next time that host's detail view is built - a
`Left`/`Right`/`n`/`N` hop, a click, or closing and reopening it with
`Esc`/Enter - each tab renders straight from the cache, instantly, no
placeholder, no new `ansible-playbook` invocation. Reported live:
"Gathering facts..." (and "Loading...") reappearing on *every* revisit
to a host already fully loaded moments earlier, not just the first -
there was no session-level caching here at all before this, only
Ansible's own, optional `fact_caching` (see the Summary tab's own
section below), which still pays for a fresh connection/process on every
visit even when it makes the underlying gather itself fast. A tab whose
fetch fails is deliberately never cached - a transient failure
(a flaky connection, say) shouldn't stick for the rest of the session;
the next visit retries it fresh. Never invalidated once cached, matching
the live run drill-down's own `resolveCache`/`docsCache` (survive a
close/reopen within one generation) - quitting and reopening the whole
verb is the way to force a genuinely fresh read.

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

Two-pane behavior here is unified with the run drill-down's own
(design-docs/TwoPanedLayout.md) and with `tangsible inventory`'s
(below): no split pane exists at all until a host is actually opened -
`Enter`, or a click - no matter how wide the terminal is; opening one on
a wide enough terminal (`uikit.SplitMinTotalWidth`, the same threshold
the run drill-down uses) shows the list and the detail view side by
side instead of full-screen. `Esc` and `q` both close the detail view
again, split or full-screen alike, back to the plain list - matching the
run drill-down's own `handleOutputViewQuit`, `q` closes unconditionally
even with an active search, the same precedence `handleOutputViewQuit`
has over `Esc`'s own "clear the search first" nuance there. Only `Ctrl-C`
still quits the whole program while a detail view is open; `q` only quits
outright at the plain list/tree level, same as the run drill-down's own
tree. Narrowing the terminal
mid-session falls back to full-screen detail for whatever host was last
showing; widening back re-enters split mode. A dedicated resize-watcher
goroutine drives this, mirroring `tui.go`'s own `startResizeWatcher` -
this view has no other ticker to piggyback a periodic re-layout on.

Keyboard focus, once a host is open, sits on the active tab's own
content (`detailTabs.Primitive()`) whether split or full-screen - `↑`/
`↓`/PgUp/PgDn scroll the tab, not the host list. `←`/`→` hop to the
previous/next host in the list (a plain alias for `n`/`N`, which already
did this before `←`/`→` did too - this view has only one dimension to
hop along, unlike the run drill-down's own `←`/`→` = host, `n`/`N` =
task split) - intercepted centrally regardless of where real focus sits,
same as `n`/`N` always was. A mouse click on the list pane itself still
moves real focus back onto the list for as long as you keep navigating
there, reverting to the detail pane on the next host selection.

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

Leads with an `inventory` section: the host's own variables from the
inventory itself (`inventory.ListHostVarsExport`, `ansible-inventory --list
--export`, group vars not merged in), rendered as YAML. That export also
folds in host_vars file values, so `inventoryOnlyVars` drops every key a
discovered file defines (the file wins anyway, and is shown below).
Then shows every `host_vars/<hostname>` file verbatim, one section per file -
not a merged/flattened key-value view. Preserves comments/formatting and
needs no variable-precedence/merge logic. File discovery mirrors
ansible-core's `DataLoader.find_vars_files` (`DiscoverHostVarsFiles`): per
directory the first of `<host>`, `<host>.yml`, `.yaml`, `.json` wins, a
directory is walked recursively (hidden files, `~` backups and dotted
subdirectories skipped; `.yml`/`.yaml`/`.json` or no extension) - the file
names `main.yml`/`vault.yml` are only convention. Each line's own `key:` portion
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

# New ideas

This chapter describes some new ideas on top of the currently implemented
functionality of tangsible hosts and tangsible host.

## tangsible inventory

```
tangsible inventory [<playbook>] [-i ...] [-e ...]
```

Brings up a tree view similar to `ansible-inventory --graph`: groups as
nodes, hosts as leaves. A host that's a direct member of more than one
group appears once under each of those groups (matching real `--graph`
output, which renders the group DAG as a tree by duplication) - but it's
pinged only once; the ping-dot result is shared across every duplicate
row of that host, never refetched per occurrence.

Initial expand state reuses `general.default_tree_state` (the same
`"collapsed"`/`"expanded"` key the live run tree already reads), not a
new config key of its own.

Navigation reuses `tangsible run`'s own tree keymap wherever the mapping
is actually meaningful: Enter/Right/Left expand/collapse a group node
(matching the run tree's task-row behavior) or open a host leaf's detail
view (matching a host-row's Enter there); `E`/`C` expand/collapse
everything; Home/End/`G`/Ctrl-A/Ctrl-E jump to first/last row. `n`/`N`
and `y` have no sensible equivalent here (no "task" to hop between, no
single tab's content to copy) and are left out entirely. `/` (in-tab
search) could make sense but is deliberately left out of this first
iteration too.

Group nodes render like the run tree's play rows (white/bold) - no
green/lightsteelblue key-value split, since a group node is just a name
with nothing to split into key and value, and no collapsed/expanded
chevron either (the run tree has none, so this view doesn't invent one).
A group's name is followed by its own unique host count in parentheses -
every host anywhere beneath it, direct or via nested groups, counted
once no matter how many branches repeat it; a group with no hosts at all
anywhere beneath it renders in `nothing_reported` instead of the usual
white/bold, the same "nothing to act on here" signal that color already
carries elsewhere. Host leaves render exactly like `tangsible hosts`' own
list rows: a ping dot leading the hostname (gray in flight, green
reachable, red unreachable), fired concurrently once per unique host the
instant the tree renders.

Enter on a host leaf opens the exact same five-tab detail view
`tangsible hosts` opens on Enter, including that view's own two-pane
split layout on a wide enough terminal (tree pane + detail pane side by
side) - not a separate, simpler picker. No split pane exists at all
until a host is actually opened this way, matching `hosts`' own and the
run drill-down's own identical rule (design-docs/TwoPanedLayout.md).
Esc from that detail view returns to the tree, split or full-screen
alike, cursor back on the host leaf that opened it (matching `hosts`'
own Esc-to-list behavior).

`<playbook>` is optional for the same reason it's optional for `host`/
`hosts`: the detail view's Plays tab needs one to probe against. For now
this behaves exactly like `host`/`hosts` with no playbook given - no
special-casing for `inventory` specifically. Worth revisiting later
whether a missing playbook should instead just hide the Plays tab
outright (for all three verbs, not just this one).

**Implemented** (`internal/host/inventory.go`). A few decisions beyond
the Q&A above, made while building it:

- `ansible-inventory --list`'s own universal `all` group is never
  rendered as its own row - `all`'s children become the tree's own
  (possibly several) top-level rows directly, matching "the tree shall
  initially only show the top-level groups" literally and the same
  reasoning the Groups tab already applies to `all` (a line with nothing
  informative to say).
- `Left` goes one step further than the run tree's own Left ever needed
  to: on an *already-collapsed* group, it climbs to that group's own
  parent and collapses it, moving the cursor there - the run tree never
  needed this (a task row has no parent of its own to climb to), but a
  tree that's actually deep benefits from it. `E`/`C` get the equivalent
  cursor-safety the live tree's own `collapseAll` already has - the
  cursor snaps to its own top-level ancestor first, so collapsing
  everything never strands it on a row about to disappear.
- Split-mode keyboard focus: an earlier version of this kept focus on the
  tree while split, the opposite of `hosts`' own choice, reasoning that
  the tree's own hierarchical navigation was the primary interaction and
  had to keep working via the keyboard. Revisited and reversed once
  `hosts`/`inventory`/the run drill-down's own two-pane behavior were
  unified to all work the same way (design-docs/TwoPanedLayout.md): no
  split pane exists at all until a host is actually opened, and once one
  is, focus moves to the open detail view's own tabs - `↑`/`↓`/PgUp/PgDn
  scroll its content, `Esc` closes it (split or full-screen alike, no
  longer full-screen-only), and `←`/`→` hop to the previous/next host
  *occurrence* in the tree's own full depth-first order (`allHostNodesInOrder`,
  see below), force-expanding whichever ancestor groups are collapsed
  along the way (`expandAncestors`) so the destination always lands on a
  visible row.
  Tree-level `Right`/`Left`/`E`/`C` (expand/collapse) only apply while no
  detail view is open at all - once one is, they're superseded by the
  detail view's own keymap, the same way the run drill-down has no
  expand/collapse-all concept while its own output view is open either.
  A mouse click on the tree pane still moves real focus back onto it for
  as long as you keep navigating there, reverting to the detail view's
  tabs on the next host selection.
- `n`/`N`/`←`/`→` inside the opened detail view all hop through
  `allHostNodesInOrder`'s own full depth-first tree order - "the host
  that would come next/previous vertically if the tree were completely
  expanded," literally, walking real tree *occurrences* rather than a
  deduplicated/alphabetical hostname list. An earlier version hopped
  through `inventory.FlattenInventoryHosts`' own flat, deduplicated order
  instead (the same list `hosts` hops through), landing on a repeated
  host's first occurrence by name - reported live as the tree "jump[ing]
  randomly around" once a host's several occurrences were spread across
  different branches, since advancing by *name* can skip straight past
  whichever occurrence actually sits next to the current one on screen.
  `←`/`→` are a plain alias for `n`/`N` here (this tree has only one
  dimension to hop along, unlike the run drill-down's own `←`/`→` = host,
  `n`/`N` = task split) - kept as an addition, not a replacement, since
  `n`/`N` already worked this way before `←`/`→` did too.

