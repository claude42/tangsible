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
