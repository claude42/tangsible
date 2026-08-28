# Start With Play

## Situation

`ansible-playbook` supports `--start-at-task` to start a playbook at a
specific task and let it run until its end. The significant drawback is
that task names do not need to be unique, so it can't reliably target one
exact position - especially since most playbooks consist of thin tasks
that just call a role, and that role's own entry task name tends to repeat
across plays (e.g. every play's first task named after the role it
installs).

There is no equivalent `--start-at-play` flag at all.

## Idea

Rather than editing the playbook (inserting uniquely-named marker tasks,
as originally proposed here, with all the caveats that implies - polluting
a tracked file with tooling artifacts, needing idempotent detection so a
re-run doesn't insert a marker twice, and interacting awkwardly with
`--tags`/`--skip-tags`), build an *ephemeral* trimmed copy of the playbook
at invocation time: everything before the chosen play is dropped, and
`ansible-playbook` is pointed at that copy instead of the original. The
user's own tracked file is never touched.

Concretely, this app's own YAML parsing (`internal/source/source.go`,
already used to back the drill-down view's `TASK:` section) already knows
where each top-level play starts. Given that, "start with play N" is just:

1. Read the original playbook's own lines.
2. Slice from the chosen play's own start line through EOF.
3. Write that slice to a new file, in the *same directory* as the original
   playbook - not a system temp directory - so that any `roles:`/
   `include_tasks`/`import_tasks` reference resolved relative to the
   playbook's own directory still finds exactly what it would have found
   for the original file.
4. Run `ansible-playbook` against that file instead.
5. Remove it once that generation finishes.

No unique marker names, no file mutation, no tag interaction to worry
about - a play tagged for skipping is unaffected either way, since nothing
about tag evaluation changes; only *which plays exist in the file at all*
does.

## v1 scope

Only a playbook's own **top-level** plays are addressable this way. A
playbook that pulls in further plays via `import_playbook` is out of
scope for v1 - the target play might not even live in the file being
truncated, and finding/trimming it would mean walking the same
`import_playbook` call graph this app has deliberately avoided modeling
elsewhere (`internal/source/source.go`'s own doc comment makes the
analogous call for role/include resolution: classify files by shape
rather than trace references). Treated the same way as `strategy: free`
elsewhere in this app - a known, documented gap, not a partial
implementation pretending to be complete.

A play with no `name:` at all can't be targeted this way either - there's
nothing for the user to type or pick from an autocomplete list. Same
restraint already applied to task-name collection
(`design-docs/Autocomplete.md`'s `collectTaskName`).

Duplicate play names within one file are inherently ambiguous, exactly
like duplicate task names are for `--start-at-task` itself - the first
match (in file order) wins, and isn't specially reported as ambiguous.

## UI: another field in the re-run dialog

Rather than a separate command or keybinding, "start with play" is a new
field in the existing re-run dialog (`Rerun.md`,
`design-docs/Autocomplete.md`), alongside "Start with task": a
**"Start with play"** field, first in the form (logically: pick a play,
then optionally narrow further with a task name within whatever's left),
autocompleted the same way "Start with task" already is - sourced from a
static scan of the playbook's own top-level plays, not the live event
stream (mirroring why task-name/tag autocomplete is sourced statically:
it has to work in the `rerun` verb's very first dialog, before any
generation has run).

The two fields compose rather than being mutually exclusive: choosing a
play trims the file to that play onward; a task name typed alongside it
is then passed to `--start-at-task` exactly as today, now against the
trimmed file - "start partway into task X, which happens to live in play
N" falls out for free, with no special-casing between the two fields.

Leaving the field empty runs the whole playbook, same "empty means no
restriction" convention every other field in this dialog already follows.

If the typed play name doesn't match any top-level play in the file
(mistyped, or a play that genuinely has no name), the generation fails
immediately with a clear synthetic error rather than silently falling
back to running the whole, untrimmed playbook - the same "fail
visibly rather than guess" posture `NewRequestRerun` already applies to a
`SpawnGeneration` failure elsewhere.

## CLI form

`tangsible run --start-at-play <name>` offers the same mechanism from the
command line, not just the rerun dialog - useful for scripting, or simply
not wanting to wait for the whole playbook to open the TUI before
re-running from the dialog. `--start-at-play` is Tangsible's own synthetic
flag, understood by no real `ansible-playbook`: it's stripped out of the
passthrough argument list before anything else touches it, so it's never
recorded into `.tangsible/state.toml`'s invocation history (a future bare
`tangsible rerun` replays history's own `Rest` verbatim, straight at
`ansible-playbook` - which would reject an unrecognized flag) and never
reaches `SpawnGeneration` itself. A bad play name fails immediately, with
a clear stderr message and a non-zero exit, before anything is recorded to
history and before ansible-playbook (or the TUI) is ever involved - the
same "resolve and validate everything before recording" ordering `run`
already uses for "no playbook could be resolved."

`rerun --start-at-play <name>` also works, pre-filling the dialog's own
Play field the same way `-l`/`--tags` already pre-fill Hosts/Tags -
extracted before `ResolveRerun` ever sees the rest of the args (same
`ExtractStartAtPlay` helper `run` uses, same reason: it must never survive
into `ResolveRerun`'s own `Rest`, which a later bare `tangsible rerun`
replays verbatim, straight at `ansible-playbook`). Unlike `run`, `rerun`
never resolves/validates it on the spot - there's no generation to spawn
yet at that point, just the dialog to pre-fill - so a bad play name isn't
caught until the dialog is confirmed, at which point it fails exactly the
same way a hand-typed one in the Play field already would (see
`runner.NewRequestRerun`'s own `TrimPlaybookToPlay` handling). Also never
recorded into history, for the same reason it's stripped out to begin
with - a later bare `tangsible rerun` has nothing to fall back to for it,
unlike Tags/Hosts.

## A drill-down gap this surfaced, and the fix

Spawning against a trimmed copy means every `RawEvent`'s `task.path` for a
task defined *directly in the top-level playbook* (as opposed to one
reached via `roles:`/`include_tasks`/`import_tasks`, whose own file is
untouched) now points into the trimmed copy's own path and line numbers -
not the original file a session's `TaskSourceIndex` was built from at
startup. Left unfixed, the drill-down view's "Task definition" tab would
silently disappear for exactly those tasks (a lookup miss there is
already treated as "nothing to show," never an error - so this degraded
silently rather than crashing, which is what let it go unnoticed until
checked directly).

Fixed with `source.MergeSourceIndex(dst, trimmedPlaybookPath)`: re-indexes
the trimmed file and merges its entries into the existing index, in
place - safe with no locking because it always runs synchronously, on the
same single event-loop goroutine that both `state.Reset()` and every
`TaskSourceIndex` read already run on. Applied both by
`runner.NewRequestRerun` (the rerun dialog's own path) and by `run`'s CLI
handling above - the same fix, needed identically by both entry points
since they share the same underlying trim-and-spawn mechanism.

## Implementation notes

- `internal/source`: `ListTopLevelPlayNames(playbookPath) []string` (named
  plays only, in file order - the autocomplete candidate list) and
  `TrimPlaybookToPlay(playbookPath, playName string) (tempPath string,
  cleanup func(), ok bool, err error)` (the actual trim-and-write step,
  called once the dialog is confirmed).
- `internal/runner`'s `NewRequestRerun` gains a `startAtPlay` parameter:
  non-empty means spawn against `TrimPlaybookToPlay`'s output instead of
  the original playbook path, clean up the temp file once that generation
  finishes, but keep recording invocation history/outcomes against the
  *original* playbook path throughout - a trimmed copy is a spawn-time
  implementation detail, never the identity a rerun is tracked under.
- `internal/config`'s `ExtractStartAtPlay` pulls the CLI's own
  `--start-at-play`/`--start-at-play=` flag (and its value) out of a raw
  passthrough arg list, before anything else - `run`'s own analogue of
  `ParsePassthroughArgs`'s existing `--tags`/`--limit` extraction, except
  the result is never reassembled back into an ansible-playbook arg list
  (unlike tags/hosts) since it isn't a real ansible-playbook flag at all.
  `run`'s own handling resolves and validates the named play (exiting
  with a clear error, before anything is recorded to history, if it
  doesn't match) synchronously, before `StartFirstGeneration` spawns -
  the temp file's cleanup reuses the same `cleanup`/`defer` mechanism
  already in place for a role session's own stub playbook, tying its
  lifetime to the whole process rather than just this one generation
  (simpler, and harmless at this project's interactive, short-lived-
  process scale - unlike the rerun dialog's own per-generation-scoped
  cleanup, which matters more there since a single session can span many
  reruns).
- The temp file's name (`.tangsible-startplay-*.yml`, via
  `os.CreateTemp` in the playbook's own directory) is `.gitignore`d in
  case cleanup is ever skipped (a crash mid-run) - the same defensive
  posture already taken for `.tangsible/state.toml`.
