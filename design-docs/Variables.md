# Variables

## Situation

Variables can occur in *many* locations within an ansible project. It's often
difficult to understand, where a variable was defined. Precende rules are not
always clear.

## Idea

Introduce new verb tangsible variables.

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
  (`HostVerb.md`) already runs a stub play with `tangsible_vars.py` and gets
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
