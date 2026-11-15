# Context Help

Now that we've implemented clicking on variables in debug mode, I think we
can use the same functionality also for something else:

When the user clicks on an option of a module, a small pop-up shall open
containing
* the name of the option
* short description
* default value (if any)
* type

Esc or clicking somewhere else will close the dialog again.

This shall work both for the run verb (and its friend) as well as for debug.

## Decisions (v1)

* **What**: a click on a key in the drill-down's Task definition tab opens a
  pop-up at the click (the same mechanism as the variable pop-up,
  `DebugMode.md`'s "Mouse support"; that pop-up becomes a generic one with
  title, rows and an optional button - this one has no button).
* **Both kinds of key**: a *module option* (`dest` under `copy:`, also under
  `args:`, and suboptions by nesting path) and a *task keyword* (`name`,
  `when`, `register`, `loop`, ... at the task's top level), the latter only
  if `ansible-doc -t keyword` knows it. The same word can be both
  (`name:` at task level vs. `name:` under `user:`), so the click must be
  resolved by structure, not by name.
* **Content**:
  * option: name, first paragraph of the description, type, default (if
    any), choices (if any), required (if so). No aliases (keeps it small).
  * keyword: name, description, what it applies to (Play/Block/Task/...).
    ansible-doc has no default/choices/required for keywords.
* **The module key itself** (`copy:` in `copy: ...`): a click shows the
  module's name and its `short_description` (from `ansible-doc -j`). The key
  is the top-level one matching the task's `action`.
* **Description**: first paragraph only, ansible-doc markup (`I() B() C()
  U() L(text,url) M() O() V() RV()`) stripped to plain text, wrapped and
  capped at about 4 lines with `…`.
* **Where**: Task definition tab only (not Resolved), in every verb that has
  the drill-down: `run`, `rerun`, `role`, `debug` and `revisit` (live
  `ansible-doc`, so only if the module is installed).
* **Mouse-only**; Esc or a click outside closes it. The Docs tab is the
  keyboard route.
* **Nothing opens** for: a key that isn't an option or keyword of that
  module, a non-module action (`block`, `include_role`, `meta`), a module
  `ansible-doc` can't find, free-form modules (`command: echo hi`) and legacy
  `key=value` strings, or a click whose row can't be mapped to a source line
  (e.g. wrapped text).
* **Conflict with the variable pop-up** (debug): a word followed by `:` is a
  key and gets option/keyword help; any other word is a variable candidate.

## Implementation notes

* Data: `ansible-doc -t module -j <action>` (~0.4s) and `-t keyword -j`,
  fetched asynchronously when the drill-down opens and cached per action,
  shared in spirit with the Docs tab's `docsCache` (the Docs tab caches plain
  text, this needs the JSON). A click before the fetch finished opens nothing.
* Structure: parse `sourceIndex[task.Path]` with `yaml.v3`; the click's source
  line is the tab's scroll offset plus the row, verified by comparing the
  row's text (read from tcell's cells, as for variables) with that line. The
  module key is the top-level key matching the task's `action`. Pure function
  (source text, line) -> (kind, nesting path), unit-tested.
* Update `helptables.go`, `Keyboard-shortcuts.md`, `Codemap.md`, Changelog.

## Implemented

`internal/ansibledoc/contexthelp.go` and `taskkeys.go` (data and YAML
classification, pure and unit-tested), `internal/session/livesession_popup.go`
(the pop-up shared with the variable pop-up) and `livesession_contexthelp.go`.
Verified live in tmux in `run` and in `debug`: option (`mode`, `dest`, `state`
with choices), the same word `name` as keyword at task level and as option
under `group:`, the module key, `when`, and a value (nothing opens).

Where it differs from, or adds to, the plan:

* **Click to source line by content, not by row count.** The first idea (scroll
  offset + row, verified against the cells) broke on the first long path: a
  wrapped line earlier in the text shifts every row after it, so options below
  a long `dest:` got no pop-up. A row that starts a source line is a *prefix*
  of it (the whole line, or its first chunk when wrapped), so candidate lines
  are those the row prefixes with the clicked word and ':' at the same column
  (`keyLinesMatching`); all candidates must classify the same, else nothing
  opens.
* **Aliases resolve** to their option (a task may say `path:` for `dest:`); the
  pop-up shows the name as written and no aliases row.
* **Keyword names** come from `ansible-doc -t keyword -l`, picked out line by
  line: that listing is YAML-shaped but contains ANSI codes and unquoted
  `": "`, so a YAML parser rejects it. Then one `-t keyword -j <names>` call.
* **Docs are prefetched** when a drill-down opens (once per action; keywords
  once per session). A click before they arrive, or on a module `ansible-doc`
  doesn't know, opens nothing.
* **Keyword pop-up** shows type and "applies to" next to the description;
  `ansible-doc` has no default/choices/required for keywords.
* The variable pop-up's own click rule stands: a word followed by `:` is a key
  (help), any other word a variable candidate (debug only).
