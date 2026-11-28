# Variables

## Situation

Variables can occur in *many* locations within an ansible project. It's often
difficult to understand, where a variable was defined. Precende rules are not
always clear.

## Idea

Introduce new verb tangsible variables. (It was built as a verb first and then
moved into the host views as their Variables tab, see "Moved into the host
views" at the end; the rest of this text describes the verb it started as.)

* It shall crawl all locations in a project where variables could be defined
  (of course some variables are dynamically assigned, this occasions can not
  be identified by a static analysis).
* As a next step, all variables shall be rendered as top-level items of a
  tree. 
  * each line shall display a variable and their value from the definition
    with the highest precedence plus the location where it has been defined
  * vaulted values shall be masked of course
  * in order to avoid cluttering up the tree, if the variable value spans
    more than one line, only the first line shall be shown
* When the user click on a line or presses enter on a line, it shall expand
  and all other definitions of the same variable shall be displayed
  * each line shall display a variable and their value plus the location
    where it has been defined
  * the lines shall be shown in order of precedence
  * vaulted values shall be masked of course
  * in order to avoid cluttering up the tree, if the variable value spans
    more than one line, only the first line shall be shown
* When the user presses "e", an editor with the corresponding file shall be
  opened
  * of course only if the source where the variable has been defined is
    editable
  * after leaving the editor, the tree shall be updated to show any changed
    values


## Discussion (2026-11-26)

Decisions from the first round; the original idea above stays as written.

### Shape of the verb

* `tangsible variables hostname [playbook.yml]`, the same argument shape as
  `tangsible host` (hostname first, then the optional playbook, each found by
  `SplitPlaybookArgsAmongOptions`). The host selector is a tab per host or a
  dropdown at the top; reuse what `hosts`/`inventory` already do for moving
  between hosts rather than inventing a pattern. Open: tabs vs dropdown.
* **v1 scope**: inventory (and its `group_vars`/`host_vars`), the playbook
  directory's `group_vars`/`host_vars`, and role `defaults`/`vars` for the
  roles that apply to the host. **Left out**: play-level variables (`vars:`,
  `vars_files:`, block/task vars), `set_fact`/`register` and `-e` - the last
  three are dynamic, listed as "not shown" at most. Play-level variables
  arrive in v2, see "view and variables" below.
* Roles: `hostname playbook.yml` shows the role variables of the plays that
  apply to the host (the host verb's Plays tab already works out which plays
  and tasks apply), each labelled with its role. A second form,
  `tangsible variables path/to/role` (the argument is a directory with
  `tasks/`; same name-or-path rule as `tangsible role`), shows that role's
  `defaults` and `vars` with no host. Not in the first cut. No separate verb.
* Not seen by a static crawl, listed as such: `include_role`/`import_role`
  with a computed name, dependencies decided at runtime, role parameters in
  the playbook.
* `e` opens the defining file in the editor (the drill-down's existing editor
  command, to be checked for how it gets file and line); only for a source
  that is a file. The tree is refreshed afterwards.

### Accuracy: Ansible is always right

Two sources, so the answer is as close to Ansible's as possible:

* **The winner** comes from Ansible itself: the host verb's Variables tab
  (`HostVerb.md`, as it was then) already runs a stub play with `tangsible_vars.py` and gets
  each variable's winning value and its origin (file, line, `-e`). That line
  is the top row, whatever the static analysis thinks.
* **The other definitions** come from a static crawl, ordered by Ansible's
  precedence, lowest to highest: role `defaults`, inventory `group_vars`
  (`all`, then parent groups before child groups, ties by name or
  `ansible_group_priority`), inventory `host_vars`, playbook `group_vars`,
  playbook `host_vars`, role `vars`. (Play-level levels slot in at v2.)

**Disagreement** = the crawl does not rank Ansible's winning definition on
top, or cannot find it among the definitions it crawled. We are the one that
is wrong then, never Ansible. Show it subtly (a dim marker on the row and a
sentence in its detail), not as an error; the order of the *other* rows is
the uncertain part. Decision: start with the subtle form and see how often it
happens before building more (a counter in the run log, or a test fixture per
case, would tell us). Tests compare the crawl's winner with Ansible's on the
`testdata/` projects.

### view and variables

Two verbs, not one: `view` is a tree of plays and tasks, `variables` a tree
of variables per host, and merging them gives one verb with two unrelated
screens. They share a **static project layer** (parse the playbook: plays,
roles, `vars_files`, `vars`; find `group_vars`/`host_vars`), designed once.
Play-level variables then come through `view`: a **Variables tab** in its task
drill-down, for a chosen host, showing what is in effect at that task (play
`vars:`/`vars_files:`, role vars, block and task vars) - `view` already knows
which play and task is selected. That tab is "v2" of `variables`.

Order of work: `view` first (it needs the static layer), `variables` second.


## As built (2026-11-27)

**Implemented** (as a verb, `tangsible variables [ansible-playbook options]
hostname [playbook.yml]`; since moved, see the end): v1, no play-level
variables. Packages `internal/varscrawl` (data) and `internal/variables`
(screen); fixture `testdata/variables`.

* **One mechanism, not two.** The discussion above planned the winner from the
  host verb's stub play and the other definitions from a static crawl. Both
  now come from one embedded Python script (`varscrawl/loader.py`) that uses
  ansible's own code: `PlaybookCLI(...)._play_prereqs()` for the inventory,
  then, in the order of `C.VARIABLE_PRECEDENCE`, the same calls
  `VariableManager.get_vars` makes - but one source at a time, so each layer
  keeps the file and line of every value (data tags, 2.19+). Ansible's own
  merged answer for the same context (`variable_manager.get_vars(play, host)`)
  comes from the same script, no play runs, and is what the crawl is checked
  against. This is not a static YAML crawl in Go: group trees, `group_vars`
  directories, vault, `-i` handling and role loading are ansible's. See
  `AnsibleInternals.md` #21.
* **The real order of precedence** (lowest first), which is not the simplified
  list in the discussion above: role defaults; inventory file `[all:vars]`;
  inventory file group vars (parents before children); inventory
  `group_vars/all`; playbook `group_vars/all`; inventory `group_vars/<group>`;
  playbook `group_vars/<group>`; inventory file host vars; inventory
  `host_vars`; playbook `host_vars`; (facts, not shown); play vars and
  `vars_files` (v2); role vars; (block/task vars, `set_fact`: not shown);
  `-e`. A playbook's `group_vars` therefore do *not* lose to an inventory's
  `host_vars` file by directory but by level, as above. The first six
  precedence entries are matched by name from ansible's configuration.
* **Inventory beside the playbook.** Ansible reads `group_vars`/`host_vars`
  next to every inventory source and next to the playbook; in a project whose
  inventory sits in the playbook's directory that is one directory read twice,
  and every variable would show the same file as two definitions. The
  playbook's level is higher (it comes after the inventory's at each step), so
  that one is listed and the inventory's level for the same directory is
  skipped (also for two inventory sources in one directory). Without a
  playbook there is no playbook level, so the inventory's stays.
* **Which roles**: those of every play whose host pattern matches the host
  (the pattern is evaluated against the inventory; a templated one is skipped
  with a note). Several applying plays are treated as one set; the check
  against ansible's answer is made only when exactly one or no play applies.
* **Disagreement** (`Variable.Disagree`): ansible's winner differs from the
  crawl's first definition. Ansible's definition moves to the top (or is added
  as `ansible's answer` when the crawl never found it), the variable shows a
  `?`, and expanded it says the order below the first row is our
  reconstruction. On the fixture and in the tests the two always agree.
* **Booleans and null** carry no origin tag in ansible; their file is found by
  searching the layer's files for the key (last file first) and they are
  compared with ansible's answer by value.
* **Screen**: a tree like the `inventory` verb's - `Enter`/click expands a
  variable to its definitions (winner marked `●`), `E`/`C` all, host chosen
  with `←`/`→` (no wraparound) or the `h` picker (a modal list, not tabs: it
  scales to many hosts), per-host results cached for the session, expansion
  kept by variable name across hosts, the cursor kept by variable and
  definition across a reload. `e` opens the definition's file (no line
  number: as in the drill-downs) and reloads everything when the editor
  returns; `R` reloads. Values are one line (a multi-line string is cut with
  `…`), vaulted ones `<vaulted>`. Not built: search (`/`), copy.
* **The file view.** `Enter` on a *definition* row (not the variable's own row,
  which still expands) opens one tab with the file as it is on disk
  (`variables/filepane.go`): the definition's lines marked in the selected-row
  style (the key's line and, for a top-level key, the indented lines and list
  items of its value), YAML keys highlighted, scrolled so the marked line is on
  screen - from the top when it is on the first screen, else a third of the way
  down. Wide terminals (`uikit.SplitMinTotalWidth`) use the two-pane layout of
  the other views, the list left (its columns laid out for its own width), the
  divider draggable; narrower ones show the pane alone. `Esc`/`q` close it
  (`Esc` first ends a search), arrows/paging scroll, `/` `n` `N` search, `y`
  copies the whole file, `e` edits it and the data is read again afterwards -
  the pane then shows the same definition (found again by variable name and
  position in its list) and closes if it is gone. A click on another
  definition in the two-pane layout shows that file instead. Not shown: a file
  encrypted as a whole with ansible-vault (a notice instead - the file's own
  ciphertext is not useful), binary or over-1-MB files; a definition that is
  no file (`-e`) says so in the bottom bar. No line-number gutter (so `y`
  copies the file as it is).
* **Left out of v1, as planned**: play `vars:`/`vars_files:`, block and task
  vars, `set_fact`/`register`, facts; `variables path/to/role` (no host);
  `--ask-vault-pass` (use a password file); execution environments (host
  side, like the other standalone verbs); `include_role` with a computed name;
  group_vars next to roles (`PLAYBOOK_VARS_ROOT=all` search paths).
* **Next** (v2): the Variables tab of `view`'s drill-down for a chosen host
  (play and task level), built on the same layers.


## Moved into the host views (2026-11-28)

The verb is gone; its screen is the **Variables tab** (third) of `host`,
`hosts` and `inventory`, replacing the old tab that ran a stub play with an
action plugin and showed only the winning value and its origin as YAML (the
action plugin, its stub and `runner.ActionPluginEnv` are deleted). The verb was
never released, so nothing needed to stay. Reasons: the host views already have
the hostname, the optional playbook and the ansible options; they already hop
between hosts (the verb's `←`/`→`/`h` picker duplicated that); and the crawl
runs nothing and never contacts the host, so the tab works for an unreachable
host and is faster than a play.

* **`variables.Pane`** is the tab's body: the tree, and over the same space the
  file page after `Enter` on a definition. `BuildHostDetailPrimitive` builds
  one per visit to a host (so the pane is new on every host hop); the shared
  `variables.Cache` of the session (`HostDetailCache.variables`) holds each
  host's result (errors are never cached) and which variables are open (by
  name, across hosts), so a revisit is instant.
* **Keys.** The tab body is not a text view, so the three views offer it keys
  first (`variablesKey` in `host.go`, after an active search was cleared and
  before their own `Esc`/arrows): in the list `e` edit, `R` read again, `E`/`C`
  open/close all, `/` says there is no search; in a file `Esc` back (the host
  views' `Esc` would close the whole detail view), `e` edit, and `←`/`→`/`n`/`N`
  do nothing (the file is not the host's; `n`/`N` still step through matches
  while a search is active). `Enter`, arrows and paging reach the list or the
  file through the normal focus. `q` stays the view's own (closes the detail
  view / quits the standalone `host`).
* **Search and copy** work through `TabbedPane.ActiveTextView`'s
  `BodyTextView()` hook: in a file it is the file's text (searchable, `y`
  copies it), in the list a hidden text view with a plain listing (`y` copies
  the variables with their definitions, `plainListing`).
* **Layout.** No two-pane layout inside the tab (it is already the right pane
  of the host views): the file takes the tab's width. The columns of the list
  are laid out for the tab's width, re-laid when it changes (`paneRoot.Draw`).
* **Playbook.** The host views' playbook argument (default `./site.yml`) is the
  crawl's; a playbook that does not exist leaves only the inventory side.
* `tangsible_variables` in the callback plugin stays: the crawl's loader uses
  it to mask vaulted values.

