# Check mode chrome

## Problem

`ansible-playbook --check` (a dry run) already works with Tangsible with
no code changes at all - it's a plain passthrough flag, and the jsonl
event stream has the same shape either way. But a `--check` session looks
identical to a real one: nothing in the chrome tells the user "this run
didn't actually change anything." Confirmed live (`testdata/outcomes.yml
--check`) that the risk is real in both directions - modules with proper
check-mode support (`file`, `copy`, `template`, `package`, `service`,
etc.) report an accurate predicted OK/Changed/Failed, but modules with no
check-mode support at all (`command`/`shell`/`script`) get silently
skipped by Ansible core itself instead, so a task that would fail or
change for real can render as plain Skipped - not just "the same tree,
nothing on disk changed."

## Solution (implemented)

Detect `--check`/`-C` in the session's own passthrough args
(`config.HasCheckFlag`, `internal/config/rerunargs.go` - exact-token
match only, same "documented heuristic" gap `ParsePassthroughArgs`
already accepts for `-tfoo`) and switch the session's chrome - top bar,
bottom bar, output drill-down bars, two-pane divider - from `BarStyle`'s
navy to `uikit.CheckBarStyle` (white on olive, `internal/uikit/tui_style.go`)
for as long as the session's current generation is a `--check` one. Olive
is the next still-unused base-16 chrome slot after purple (revisit) and
fuchsia (`tangsible diff`) - see design-docs/Colors.md's own "Chrome:
olive" section for the full color rationale.

`checkMode` is computed once (`internal/session/tui.go`) and never
re-derived: the re-run dialog has no field to toggle `--check`, and a
rerun always carries the original invocation's passthrough args forward
unedited, so whatever a session starts with is what every later
generation in it runs with too - live, rerun, and a revisited historical
run alike (revisit.go threads the replayed run's own recorded args
through the same way).

Also appends a plain-text `[CHECK MODE - dry run]` note to the main tree's
bottom bar (`currentMainBottomBarText`) regardless of color support -
chrome color alone doesn't reach a `NO_COLOR`/monochrome terminal, the
same concern design-docs/Morehosts.md already raised for the collapsed
host-row summary.

Deliberately out of scope: no attempt to detect or flag the "task would
have run for real but got silently skipped because its module has no
check-mode support" case from the Problem section above - that's real
`ansible-playbook --check` behavior, not something Tangsible should paper
over, and the run still shows up as a real Skipped row either way.
