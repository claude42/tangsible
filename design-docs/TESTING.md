# Testing strategy

Tangsible has no tests yet. This document is a phased plan for introducing
them - written so someone without much testing background can pick up any
phase and know exactly what to write and why, without having to invent an
approach first. It is a roadmap to work through incrementally, not
something meant to be done in one sitting.

## Philosophy

* **Test pure functions before UI code.** A function that takes plain data
  in and returns plain data out (a `*taskNode`, a `string`, a `bool`) needs
  no setup, no mocking, no framework - just call it and compare the
  result. Widget code needs a screen, an event loop, and timing, which
  makes it slow to test and brittle. Tangsible's own architecture already
  separates these cleanly - `aggregate.go`/`events.go` have zero UI
  imports, and a large chunk of `tui.go` takes `*taskNode`/`*playbookState`/
  strings and returns strings/bools with no widget construction at all.
  That separation is what makes this incremental plan possible.
* **Weight priority by this codebase's own bug history, not file size.**
  `aggregate.go` is only ~250 lines, but it has already produced two real,
  silent-corruption bugs during development (see Phase 1) - that's why it
  comes first here even though it isn't the biggest file.
* **Stdlib `testing` only** - table-driven tests, `t.Run` subtests. No
  testify, no gomock, no test-only dependency. Nothing in this plan needs
  assertion-library sugar or mocking machinery, and the project's own docs
  consistently favor "dead simple, more than fast enough at this project's
  scale" over generalized machinery - test code should follow the same
  rule as everything else here.

## Phase 1 - `aggregate.go`: the event-driven state machine

**Why first:** this is the state machine everything else displays, and
it's the easiest file in the project to test - `&playbookState{}` is a
valid starting point (its slices/maps are created lazily inside `Apply`),
and `rawEvent` is a plain struct literal, so no JSON parsing or fixtures
are needed to drive it. It's also already produced two real bugs that were
both *silent* (wrong data shown, nothing crashed) - exactly the kind of
regression a quick `go run .` glance is likely to miss:

* the handler-task-start bug, where `v2_playbook_on_handler_task_start`
  wasn't handled like `v2_playbook_on_task_start`, so a handler's results
  silently bled onto whatever task had genuinely started last;
* the timestamp bug, where a malformed `_timestamp` field used to make the
  whole event fail `json.Unmarshal` and get silently dropped, task and all.

Target: `(*playbookState).Apply`, `.recordHost`, `.noteHost`,
`(*taskNode).record`, `.counts`.

Scenarios:

* A play-start, then a task-start, then one host reporting OK (`Changed:
  false`) produces exactly one play, one task, one host recorded as
  `outcomeOK`.
* The same, but the host's raw result has `Changed: true` - outcome must
  be `outcomeChanged`, not `outcomeOK`.
* **Regression test for the handler bug:** a task-start for "task A"
  followed by a handler-task-start for "handler B", each with its own
  `v2_runner_on_ok` for the *same* host name. Assert A and B end up as two
  separate `taskNode`s, each with its own `Hosts`/`Raw` entry - B's result
  must never land on A's.
* **Regression test for the timestamp bug:** an event with
  `TimestampText: "not-a-timestamp"` still unmarshals and still gets
  processed by `Apply` normally; calling `.Timestamp()` on it directly
  returns the zero `time.Time`, not a panic or error.
* A `v2_runner_on_unreachable` event sets `HadUnreachable = true` and
  records `outcomeUnreachable`; a run with no such event leaves it false.
* Two hosts report on the same task (one OK, one Failed) - `HostOrder`
  preserves recording order, `Hosts` has both, `counts()` returns the
  right tallies.
* Recording "web2" then "web1" leaves `AllHosts` sorted (`["web1",
  "web2"]`) regardless of insertion order; recording a host again doesn't
  duplicate it.
* `recordHost` called before any task has started (`currentTask == nil`)
  is a no-op - no panic, no phantom task.
* `CurrentTask()` is nil before the first task starts, and keeps pointing
  at the most recent task even after all its hosts have reported (the
  documented "stays active until the next task starts" approximation).

Effort: a focused afternoon covers all of the above as one table-driven
`TestApply` (or a few grouped tests). Highest-value afternoon available in
this project's test suite.

## Phase 2 - `events.go`: JSON decoding helpers

**Why second:** it's Phase 1's direct dependency (`Apply`'s
`v2_runner_on_ok` branch calls `decodeHostResult`), and just as
dependency-free.

Target: `decodeHostResult`, `(rawEvent).Timestamp`.

Scenarios:

* Valid JSON with `"changed": true` decodes to a `hostResult` with that
  field set and the rest false.
* Malformed/non-object JSON decodes to a zero-value `hostResult` - no
  panic, no error surfaced (this "swallow and return zero value" contract
  is exactly what Phase 1's timestamp regression test relies on one level
  up).
* A well-formed RFC3339 timestamp (with fractional seconds) round-trips
  through `Timestamp()` correctly.
* An empty or garbage `TimestampText` yields the zero `time.Time`.

Effort: 30-45 minutes.

## Phase 3 - `tui.go`'s pure logic functions

**Why third:** most of the codebase's *subtle* domain logic lives here -
filter semantics, failure disambiguation, output formatting - and none of
it needs a single tview widget constructed. Do it in three sub-passes.

### 3a. Filtering and visibility (do this first)

Target: `taskVisible(t *taskNode, q filterQuery, sourceIndex
taskSourceIndex, isActive bool) bool`, `taskHasAnyOutcome`,
`taskMatchesSearch`, `taskOutputText`, `visibleTasks`,
`visibleTasksForHost`, `allTasks`, `tasksForHost`, `taskSet`. Build
`*taskNode`s directly (`&taskNode{Hosts: map[string]outcome{"web1":
outcomeFailed, "web2": outcomeOK}}`) - no event stream needed.

Scenarios:

* A task with one Failed host and one OK host is visible under
  `filterQuery{mode: filterFailed}` - host-level "any host" semantics, not
  "all hosts."
* A task with an Unreachable host but no Failed host is *also* visible
  under `filterFailed` - Unreachable counts as failure everywhere in this
  app; easy to regress if the outcome switch ever gets "cleaned up."
* A task with only an OK host is not visible under `filterFailed`, but is
  always visible under `filterAll`.
* A task with a Changed host is visible under `filterChanged`; a task with
  only Failed/Unreachable hosts (no Changed) is *also* visible under
  `filterChanged`, per its own "failed || has Changed" logic.
* `isActive == true` forces visibility regardless of filter, even for a
  task with zero recorded hosts (the in-progress-task case).
* `taskMatchesSearch` with an empty term matches everything; a non-empty
  term matches on task name, on `sourceIndex` text, and on a host's output
  text - three separate scenarios, since they're three separate checks -
  all case-insensitively.

### 3b. Failure/status disambiguation

Target: `statusRowText`, `genuineFailure`, `lastFailedTaskAndHost`. This is
the exit-code-4 overload logic (ansible-core reuses exit code 4 for both
`HOST_UNREACHABLE` and `PARSER_ERROR`) - a real footgun worth pinning down
in isolation from any real subprocess.

Scenarios:

* `code=0` -> `genuineFailure` false, message says "completed
  successfully."
* `code=4, hadUnreachable=true` -> `genuineFailure` false (the benign
  case), message mentions unreachable hosts, not failure.
* `code=4, hadUnreachable=false` -> `genuineFailure` TRUE (the
  parser-error case sharing the same exit code) - the scenario most likely
  to get silently broken if the exit-code handling is ever "simplified,"
  so give it its own explicit assertion distinguishing it from the
  previous case.
* `code=99` (the documented user-interrupted code) -> `genuineFailure`
  false, its own distinct message.
* Some other nonzero code -> `genuineFailure` true, message includes the
  code number.
* `lastFailedTaskAndHost` on a state with two failed tasks returns the
  *most recent* one, not the first (it searches backward through plays and
  tasks - build the `playbookState` with struct literals, no `Apply`
  needed); within the winning task, it returns the first Failed/Unreachable
  host in `HostOrder`, skipping any earlier OK hosts; a state with no
  failure anywhere returns `(nil, "")`.

### 3c. Formatting/cosmetic helpers (lower priority, do whenever)

Target: `colorTag`, `outputSummary`, `skipDetail`, `colorizeYAML`,
`topBarText`, `minutesSeconds`, `spinnerAt`, `hostTransition`,
`filterDialogText`. Lower risk - a wrong color or a slightly-off elapsed
time is visibly obvious the moment the app runs, unlike a silently
corrupted task result. A few worth calling out:

* `outputSummary`: single-line output -> `" (that line)"`; multi-line ->
  `" (N lines of output)"`; no output field at all -> `""`.
* `minutesSeconds`: `90 * time.Second` -> `(1, 30)`; `0` -> `(0, 0)`.
* `colorTag`: each of the five outcomes maps to its documented color name.

Effort: 3a is a full afternoon - the most real logic packed into small
functions in this codebase. 3b is 1-2 hours. 3c is ad hoc, an hour here and
there.

## Phase 4 - filesystem/env-driven helpers: `resolve.go` and `source.go`

**Why fourth:** the first real I/O tests, but stdlib gives everything
needed (`t.Setenv`, `t.TempDir()`, `t.Chdir`) with no extra dependency.
Config precedence and path resolution are exactly the kind of logic that's
easy to get subtly wrong and tedious to verify by hand every time.

`resolve.go` target: `splitPlaybookArgs`, `configHome`,
`readDefaultPlaybook`, `resolvePlaybook`.

Scenarios:

* `splitPlaybookArgs([]string{"site.yml", "-v"})` returns `("site.yml",
  []string{"-v"}, true)`; `splitPlaybookArgs([]string{"-v"})` returns
  `("", []string{"-v"}, false)` (a flag-shaped first arg means no
  positional playbook); `splitPlaybookArgs(nil)` returns `("", nil,
  false)`.
* `configHome` with `XDG_CONFIG_HOME` set (via `t.Setenv`) returns that
  value; unset, falls back to `$HOME/.config`.
* `readDefaultPlaybook` against a nonexistent path returns `""` silently;
  against a file with valid TOML (`[general]\ndefault_playbook =
  "foo.yml"`) returns `"foo.yml"`; against malformed TOML returns `""`.
* `resolvePlaybook`'s precedence: `TANGSIBLE_PLAYBOOK` (via `t.Setenv`)
  wins even when a `.tangsible` file also exists in the cwd. This is the
  trickiest test in this phase because of the implicit cwd dependency -
  use `t.Chdir(t.TempDir())` (available since Go 1.24, which this project
  already requires) rather than skipping the scenario.

`source.go` target: `buildTaskSourceIndex`. Reuse the six existing fixture
playbooks already under `testdata/` instead of inventing new YAML by hand.

Scenarios:

* `buildTaskSourceIndex("testdata/outcomes.yml")` produces an index
  containing an entry for each task name defined there ("ok task",
  "changed task", "failed task", ...).
* A playbook path that doesn't exist returns an empty index, not a panic -
  matches the "swallow errors" convention used throughout this codebase.

Effort: an afternoon, split unevenly - the cwd-dependent `resolvePlaybook`
test takes more care than it looks like it should.

## Phase 5 - `render.go`

**Why fifth:** simple and low-risk, but a good warm-up for asserting on
multi-line string output before Phase 6.

Scenario: build a small `playbookState` by hand (one play, one task, two
hosts in different outcomes), call `Render(&buf, state)`, and check the
output either with an exact string comparison (fine at this size) or a
handful of `strings.Contains` checks.

Effort: 20-30 minutes.

## Phase 6 - `main.go`'s isolated I/O functions

**Why sixth:** `main()` itself wraps a real subprocess and isn't
practically testable without injecting a fake `exec.Cmd`, which is a
larger refactor out of scope here. But three pieces are already isolated
behind `io.Reader`/`error` and are worth testing precisely because they
encode the exit-code-4 ambiguity again, from the subprocess side, plus a
streaming-decode edge case.

Target: `exitCodeOf`, `streamStderr`, `scanEvents`.

Scenarios:

* `exitCodeOf(nil)` -> `0`.
* `exitCodeOf` given a real `*exec.ExitError` (easiest source: run
  `exec.Command("false").Run()` in the test and use its returned error) ->
  the process's actual exit code.
* `exitCodeOf` given some other error (e.g. `errors.New("boom")`) -> `-1`,
  the sentinel for "not a real exit code."
* `streamStderr(strings.NewReader("line one\nline two\n"))` returns
  `["line one", "line two"]`; an empty reader returns an empty/nil slice.
* `scanEvents` fed one valid JSON event line plus one garbage line: the
  first item off the channel has `isEvent == true` with the expected
  `ev.Event`; the second has `isEvent == false` with a `diag` prefixed
  `"(not JSON) "`.
* `scanEvents` fed a `v2_playbook_on_stats` line: the resulting item has
  `isEvent == true` *and* a non-empty `diag` - the doc comment explicitly
  says these aren't mutually exclusive, worth pinning down so a future
  change doesn't accidentally make them exclusive.
* `scanEvents` on an empty reader: the channel closes immediately with no
  items.

Effort: an afternoon - the functions are short, but getting a real
`*exec.ExitError` and reasoning about the channel-based API takes a little
extra care.

## Phase 7 (optional, later) - `treelist.go`'s state machine only

`treeList`'s `Draw`/`InputHandler`/`MouseHandler` need a `tcell.Screen` -
see the hard-to-test tier below - but the selection/scroll bookkeeping
underneath them doesn't: `SetCurrentItem`, `ensureVisible`,
`GetOffset`/`SetOffset`, `AddItem`/`Clear`, `GetCurrentItem`/
`GetItemCount` are all plain Go methods you can call directly with no
screen involved at all. Worth a pass once Phases 1-6 feel comfortable:
add items, move the current index around, and assert the offset/visible-
window math in `ensureVisible` behaves correctly at the top, bottom, and
middle of a long list. Real, testable logic hiding inside an otherwise
hard-to-test file - don't let the file's overall classification make you
skip the one part that isn't actually hard. Effort: an afternoon.

## The hard-to-test tier

Not truly *untestable* - see the tmux note below, which corrects an
earlier version of this section - but still not worth forcing into the
default `go test ./...` suite:

* **`tui.go`'s `NewLiveTUI`** - the function that constructs every actual
  widget and wires up key bindings. It's composition, not logic; the logic
  it calls out to is already covered by Phase 3. Unit-testing the wiring
  itself would mean simulating a whole `tview.Application` for very little
  payoff, for most of it.
* **`treelist.go`'s `Draw`/`InputHandler`/`MouseHandler`** - same category
  (see Phase 7 above for the part of this file that *is* worth testing).
* **`main.go`'s `main()` itself** - would need a real refactor (e.g. an
  interface wrapping `exec.Cmd`) to test meaningfully. A legitimate future
  improvement, but a design change, not a testing task.
* **`tcell.SimulationScreen`** - confirmed present in this project's
  already-vendored tcell (v2.8.1), and it would let you script real
  key/mouse input against a full `tview.Application` and assert on
  rendered screen contents *in-process*, no subprocess involved. Genuinely
  useful, but still a stretch goal: nothing in the codebase wires
  `app.SetScreen()` today, and writing useful tests with it has its own
  learning curve (screen geometry, draw timing, event injection order).
* **tmux, on the other hand, is already proven, not a stretch goal** -
  discovered doing exactly this: driving the real compiled binary inside a
  real pty (`tmux new-session -d`, `tmux send-keys`, `tmux capture-pane -p`)
  is what actually caught a real bug during Rerun's development (the
  `SetDisabled` focus-skip quirk documented in `tui.go`/`CLAUDE.md`) - no
  pure-function unit test could have seen it, since it lived entirely in
  the interaction between `tview.Form`'s internal focus state and real
  keypresses. `e2e_rerun_test.go` (build tag `e2e`, so it's excluded from
  plain `go test ./...` - run explicitly with `go test -tags e2e ./...`)
  has a small number of smoke tests built this way, targeting exactly that
  class of regression - real bugs only visible through the actual
  keystroke → focus → render pipeline. Deliberately kept separate and
  small, not a wholesale replacement for manual verification or for the
  fast unit-test suite above: it needs `tmux` and `ansible-playbook`
  installed (a real dependency-surface increase over "stdlib only"),
  spawns real processes and a real terminal (slow next to an in-process
  function call), and needs careful poll-until-condition waits rather than
  fixed sleeps to avoid flaking - closer to a small test harness than a
  table-driven test. Reach for it for the same kind of thing that
  justified it in the first place: an interactive flow where the risk is
  specifically in the wiring/rendering/focus behavior itself, not in logic
  a unit test could isolate more cheaply.
* **Manual testing (`go run .`, or tmux for anything scripted/repeated)
  stays right for the visual/interactive layer generally.** This plan
  isn't meant to replace it for everything - only to back up the logic
  underneath it, plus now a small, deliberately-scoped set of the
  highest-risk interactive seams.

## Coverage: a flashlight, not a gate

Once Phases 1-3 exist, run `go test -cover ./...` occasionally (or `go
test -coverprofile=cover.out ./... && go tool cover -html=cover.out` for a
visual per-line view) to see which functions still have zero coverage -
especially useful inside `tui.go`, where it's easy to lose track of which
of its ~30 candidate functions you've gotten to. Don't chase a percentage
or gate anything on it: a codebase this size with a genuinely-hard-to-test
UI layer will never sensibly hit "100%," and treating coverage as a target
rather than a map tends to produce low-value tests written just to move
the number.

## CI

Deliberately not addressed here. Worth revisiting once Phases 1-3 exist -
at that point, confirm what's actually enabled on this project's specific
Codeberg instance (Woodpecker CI vs. Forgejo Actions vs. neither) before
designing anything, since GitHub Actions doesn't apply here.

## Where to start

Phase 1 this week. It directly targets the two bug classes this project
has already been bitten by, needs no new tooling, and teaches the
table-driven/`t.Run` pattern every later phase reuses.
