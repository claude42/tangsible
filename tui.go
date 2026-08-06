package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync/atomic"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

var reverseStyle = tcell.StyleDefault.Reverse(true)

// row is one flattened, currently-visible line in the list: a play, a task,
// or (if its task is expanded) a host. selected is nil for play/host rows;
// for task rows it toggles that task's expand state. id identifies the row
// across rebuilds (a *playNode, *taskNode, or hostRowID), used to restore
// the selection to the same logical row after the list is repopulated.
type row struct {
	text     string
	selected func()
	id       any
}

type hostRowID struct {
	task *taskNode
	host string
}

// flattenRows walks state's play/task/host tree into an ordered row list,
// respecting which tasks are currently expanded. Rebuilt fresh on every
// event - cheap at this project's target scale (~10 hosts, Purpose.md), and
// avoids needing to incrementally patch a tree structure by hand.
//
// width is the list's current available width (see rebuild), used to
// right-align each TASK row's counts segment (see taskLabel). showOutput is
// called when a host row is selected (Enter), to display that host's full
// result for that task.
func flattenRows(state *playbookState, expanded map[*taskNode]bool, width int, showOutput func(task *taskNode, host string)) []row {
	var rows []row
	for _, play := range state.Plays {
		rows = append(rows, row{text: fmt.Sprintf("PLAY: %s", play.Name), id: play})
		for _, task := range play.Tasks {
			t := task
			rows = append(rows, row{
				text:     taskLabel(t, state.AllHosts, width),
				id:       t,
				selected: func() { expanded[t] = !expanded[t] },
			})
			if expanded[t] {
				for _, host := range t.HostOrder {
					h := host
					rows = append(rows, row{
						text:     "    " + hostLabel(t, h),
						id:       hostRowID{t, h},
						selected: func() { showOutput(t, h) },
					})
				}
			}
		}
	}
	return rows
}

// NewLiveTUI builds an initially-empty list UI and wires it to state's
// hooks so it grows as events arrive. It does not block — the caller must
// call app.Run() and feed events through applyLive.
//
// proc is ansible-playbook's process, used so Ctrl-C/q can forward SIGINT
// to it while it's still running (tcell's raw mode disables the OS's own
// Ctrl-C-to-SIGINT delivery, so without this the child would stop
// receiving the interrupt it used to get for free — see Purpose.md's
// Ctrl-C decision). processDone/quitting are shared with the caller:
// this function only reads processDone and only writes quitting.
func NewLiveTUI(state *playbookState, playbookName string, proc *os.Process, processDone, quitting *atomic.Bool) (app *tview.Application, applyLive func(rawEvent)) {
	list := tview.NewList().ShowSecondaryText(false)
	list.SetWrapAround(false)

	expanded := map[*taskNode]bool{}
	var currentRows []row
	var currentID any
	var rebuilding bool
	following := true     // auto-follow the newest row until the user navigates away
	var jumpingToEnd bool  // true only while our own End/G handler drives SetCurrentItem
	var viewingOutput bool // true while the host-output page is frontmost; see
	// SetInputCapture below - suppresses the list's own End/'G' handling so
	// it doesn't swallow the output TextView's native End/'G' scrolling. A
	// plain locally-owned bool, not a pages.GetFrontPage() query, since this
	// function owns both places that ever switch pages.

	// Output drill-down page: a single, reused TextView (never recreated
	// per drill-down), updated via SetText each time a host row is
	// selected. styleTags stays off (tview's default) so raw command
	// output can go straight into SetText with no tview.Escape() and no
	// risk of it being misparsed as color tags.
	outputView := tview.NewTextView()
	outputView.SetDynamicColors(false) // explicit, though already the
	// default - self-documents that "no tag parsing" is deliberate here,
	// not an oversight, given raw command output goes straight into it.

	outputTopBar := tview.NewTextView()
	outputTopBar.SetTextStyle(reverseStyle)

	outputBottomBar := tview.NewTextView().SetText(" ↑/↓ or j/k/h/l scroll  g/home top  G/end bottom  esc/enter: back ")
	outputBottomBar.SetTextStyle(reverseStyle)

	outputFlex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(outputTopBar, 1, 0, false).
		AddItem(outputView, 0, 1, true).
		AddItem(outputBottomBar, 1, 0, false)

	pages := tview.NewPages()

	showOutput := func(task *taskNode, host string) {
		outputTopBar.SetText(fmt.Sprintf(" %s — %s ", host, task.Name))
		outputView.SetText(formatHostOutput(task, host))
		// SetText does not reset scroll position (lineOffset/trackEnd) -
		// without this, reopening a different host's output right after
		// scrolling through a previous one would open already scrolled to
		// the old position, potentially hiding the new content entirely.
		outputView.ScrollToBeginning()
		viewingOutput = true
		pages.SwitchToPage("output")
	}

	outputView.SetDoneFunc(func(tcell.Key) {
		// Fires on Escape, Enter, Tab, or Backtab (TextView's fixed set of
		// "done" keys). Tab/Backtab also backing out is a harmless side
		// effect, not a real concern.
		viewingOutput = false
		pages.SwitchToPage("main")
	})

	var rebuild func()
	rebuild = func() {
		rebuilding = true
		defer func() { rebuilding = false }()

		_, _, width, _ := list.GetInnerRect()
		// Belt-and-suspenders only: tview.Box's own zero-value width is 15
		// (never 0), and QueueUpdateDraw can't run this closure before
		// Run()'s first real-size draw pass anyway - taskLabel is also
		// panic-safe for any width - but clamp defensively in case that
		// ordering assumption ever changes.
		if width < 20 {
			width = 20
		}
		currentRows = flattenRows(state, expanded, width, showOutput)
		list.Clear()
		for _, r := range currentRows {
			r := r
			var selected func()
			if r.selected != nil {
				selected = func() {
					r.selected()
					rebuild()
				}
			}
			list.AddItem(r.text, "", 0, selected)
		}
		if list.GetItemCount() == 0 {
			return
		}
		if following {
			// Keep pinned to the newest row; currentID is intentionally
			// left stale here - it's never read while following, and gets
			// refreshed the instant a genuine navigation disengages it.
			list.SetCurrentItem(list.GetItemCount() - 1)
			return
		}
		for i, r := range currentRows {
			if r.id == currentID {
				list.SetCurrentItem(i)
				return
			}
		}
		list.SetCurrentItem(0)
	}

	list.SetChangedFunc(func(index int, _ string, _ string, _ rune) {
		if rebuilding {
			// List.Clear()+AddItem() fires spurious "changed" events while
			// rebuild() is repopulating (e.g. as soon as the first item
			// lands back in the now-empty list) - ignore those, rebuild()
			// restores the real selection itself once done.
			return
		}
		if index >= 0 && index < len(currentRows) {
			currentID = currentRows[index].id
		}
		if !jumpingToEnd {
			following = false
		}
	})

	state.OnPlayAdded = func(*playNode) { rebuild() }
	state.OnTaskAdded = func(*playNode, *taskNode) { rebuild() }
	state.OnHostRecorded = func(*taskNode, string) { rebuild() }

	topBar := tview.NewTextView().SetText(fmt.Sprintf(" %s ", playbookName))
	topBar.SetTextStyle(reverseStyle)

	bottomBar := tview.NewTextView().SetText(" ↑/↓ navigate  home/end/G: top/bottom  enter: expand/view output  q: quit ")
	bottomBar.SetTextStyle(reverseStyle)

	flex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(topBar, 1, 0, false).
		AddItem(list, 0, 1, true).
		AddItem(bottomBar, 1, 0, false)

	pages.AddPage("main", flex, true, true)
	pages.AddPage("output", outputFlex, true, false)

	app = tview.NewApplication().SetRoot(pages, true)

	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		isQuit := event.Key() == tcell.KeyCtrlC ||
			(event.Key() == tcell.KeyRune && event.Rune() == 'q')
		if isQuit {
			if processDone.Load() {
				quitting.Store(true) // before Stop() - see main.go's race note
				app.Stop()
			} else {
				_ = proc.Signal(os.Interrupt) // best-effort; child may race-exit
			}
			return nil
		}

		isJumpToEnd := event.Key() == tcell.KeyEnd ||
			(event.Key() == tcell.KeyRune && event.Rune() == 'G')
		if isJumpToEnd && !viewingOutput {
			following = true
			jumpingToEnd = true
			if list.GetItemCount() > 0 {
				list.SetCurrentItem(list.GetItemCount() - 1)
			}
			jumpingToEnd = false
			return nil
		}
		// While viewingOutput, End/'G' fall through unmodified so the output
		// TextView's own native End/'G' scroll-to-bottom handling gets them
		// instead - rebuild() keeps updating the hidden list regardless of
		// which page is frontmost, so nothing about the list's state is lost.

		return event
	})

	applyLive = func(ev rawEvent) {
		app.QueueUpdateDraw(func() {
			state.Apply(ev)
		})
	}

	return app, applyLive
}

const taskIndent = "  "

// colorTag returns the tview style-tag foreground color name for o, per
// TUI.md's OK/Changed/Skipped/Failed = green/yellow/cyan/red convention
// (using tcell's/W3C's "teal" as the closest named match for cyan).
func colorTag(o outcome) string {
	switch o {
	case outcomeOK:
		return "green"
	case outcomeChanged:
		return "yellow"
	case outcomeSkipped:
		return "teal"
	case outcomeFailed:
		return "red"
	case outcomeUnreachable:
		return "maroon" // deliberately muted vs "red" - see TUI.md; both are
		// base ANSI-16 names (index 9 vs 1), not RGB-approximated extended
		// W3C names, so they stay reliably distinct across terminal themes.
	default:
		return "-"
	}
}

const (
	taskPrefix = "TASK: "

	// minTaskTitleName is TUI.md's "minimum 15 characters" floor, applied to
	// task.Name's own text (the part after the "TASK: " label - what a user
	// would actually call "the title"), not the rendered "TASK: "+name
	// string as a whole.
	minTaskTitleName = 15

	// titleHostGapFloor is the minimum acceptable breathing room between the
	// title and the first host name, per TUI.md - used only to decide
	// *whether* shrinking is needed and *how far* to shrink the title. The
	// gap actually rendered (see taskLabel) is whatever's left over once
	// title and hosts are sized, which is normally >= this - true
	// right-alignment, matching TUI.md's own sketch, not a fixed 3 spaces.
	titleHostGapFloor = 3

	// minRenderedGap is the hard floor on the space actually rendered
	// between title and hosts. Only reached once the title (floored at 15
	// chars) and every hostname (floored at 1 char each) still don't leave
	// titleHostGapFloor of room - a pathologically narrow, accepted overflow
	// case (same style as aggregate.go's noteHost comment and this file's
	// width-staleness note below), not engineered away further.
	minRenderedGap = 1

	grayTag = "gray" // placeholder color for a host AllHosts knows about
	// run-wide but that hasn't reported for *this* task yet.
)

// taskLabel builds one TASK row's full text, including its leading indent.
// Per TUI.md's "New ideas for the task lines", every host in allHosts (the
// run-wide, alphabetically-sorted set of hosts seen so far - see
// playbookState.AllHosts) is shown right-aligned after the task title, each
// colored by its outcome for this specific task, or gray if this task
// hasn't recorded a result for it yet. If allHosts is empty (nothing has
// been discovered run-wide yet - always true right up until the first
// result of the run lands, for whichever task that turns out to be), the
// row is just the title, with no trailing gap or content.
//
// Fitting/shrinking happens entirely in plain, untagged rune space (raw task
// name, raw host names) and only wraps the final, already-correctly-sized
// pieces in color tags and tview.Escape() once, at the end - avoids repeated
// tview.TaggedStringWidth calls inside what can otherwise be a
// multi-iteration shrink loop, and mirrors the truncate-raw-then-escape
// discipline the old counts-based taskLabel already used.
//
// Per TUI.md: if title+gap+hosts doesn't fit, first the title's own name
// text is shortened (down to a 15-rune floor, or its own natural length if
// that's already shorter); if that alone isn't enough, hostnames are
// gradually shortened next - always the currently-longest one, one
// character at a time, down to a 1-character floor each. Truncation
// collisions between hostnames are an accepted, known tradeoff (TUI.md) -
// not solved here. Hostname truncation has no ellipsis marker (per TUI.md's
// own example); title truncation keeps the old "…" convention, since
// TUI.md says the title's rendering is otherwise unchanged ("as before").
//
// avail reflects the terminal size as of the last rebuild (see
// flattenRows) - a bare resize with no new incoming event won't re-flow
// existing rows until the next event triggers one. Accepted limitation,
// not a bug - unchanged from the old taskLabel.
func taskLabel(task *taskNode, allHosts []string, avail int) string {
	availContent := avail - len(taskIndent)
	if availContent < 0 {
		availContent = 0
	}

	nameRunes := []rune(task.Name)
	nameWidth := len(nameRunes)

	// Per-host raw display text, shrunk (if at all) as independent copies -
	// never mutates allHosts or its strings.
	hostRunes := make([][]rune, len(allHosts))
	for i, h := range allHosts {
		hostRunes[i] = []rune(h)
	}
	hostsWidth := func() int {
		w := 0
		for i, hr := range hostRunes {
			w += len(hr)
			if i > 0 {
				w++ // fixed 1-space separator between adjacent host names -
				// not itself a shrink target per TUI.md's algorithm, which
				// only calls out the title and the hostnames themselves.
			}
		}
		return w
	}

	haveHosts := len(allHosts) > 0

	// fits reports whether the current nameWidth, plus a hypothetical
	// titleHostGapFloor-sized gap, plus the current host list, would fit -
	// i.e. "is there at least the minimum acceptable breathing room". Drives
	// the shrink decisions below; the gap actually rendered (see padding,
	// at the end) is computed separately, as whatever's really left over.
	fits := func() bool {
		need := len(taskPrefix) + nameWidth
		if haveHosts {
			need += titleHostGapFloor + hostsWidth()
		}
		return need <= availContent
	}

	truncatedName := false

	if !fits() {
		// Step 1 (TUI.md): shorten the title's own name text, down to a
		// 15-rune floor (or its own natural length, if that's already
		// under 15 - the floor only ever shrinks, never pads).
		floor := nameWidth
		if floor > minTaskTitleName {
			floor = minTaskTitleName
		}
		need := len(taskPrefix)
		if haveHosts {
			need += titleHostGapFloor + hostsWidth()
		}
		target := availContent - need
		if target < floor {
			target = floor
		}
		if target < nameWidth {
			nameWidth = target
			truncatedName = true
		}
	}

	// Step 2 (TUI.md): if the title, even floored, still doesn't leave
	// titleHostGapFloor of room before the full host list, gradually shrink
	// hostnames - always the currently-longest, one character at a time,
	// down to a 1-character floor each.
	for !fits() {
		longest := -1
		for i, hr := range hostRunes {
			if len(hr) > 1 && (longest == -1 || len(hr) > len(hostRunes[longest])) {
				longest = i
			}
		}
		if longest == -1 {
			// Every hostname is already at its 1-character floor and it
			// still doesn't fit even alongside a title floored at 15
			// characters. Accept the overflow (see minRenderedGap).
			break
		}
		hostRunes[longest] = hostRunes[longest][:len(hostRunes[longest])-1]
	}

	var rawTitle string
	if truncatedName && nameWidth >= 1 {
		rawTitle = taskPrefix + string(nameRunes[:nameWidth-1]) + "…"
	} else {
		rawTitle = taskPrefix + string(nameRunes[:nameWidth])
	}
	// Escape only now that the raw text is already correctly sized, so
	// slicing above can never cut into an escape sequence Escape() would
	// otherwise have produced.
	title := tview.Escape(rawTitle)

	if !haveHosts {
		return taskIndent + title
	}

	// The actual rendered gap is whatever's really left over once title and
	// (possibly-shrunk) hosts are sized - right-aligning the host list to
	// the row's far edge, same "variable padding, floored low" shape as the
	// old counts-based taskLabel's own padding math.
	padding := availContent - tview.TaggedStringWidth(title) - hostsWidth()
	if padding < minRenderedGap {
		padding = minRenderedGap
	}

	hostSegments := make([]string, len(allHosts))
	for i, h := range allHosts {
		o, done := task.Hosts[h]
		tag := grayTag
		if done {
			tag = colorTag(o)
		}
		hostSegments[i] = fmt.Sprintf("[%s]%s[-]", tag, tview.Escape(string(hostRunes[i])))
	}

	return taskIndent + title + strings.Repeat(" ", padding) + strings.Join(hostSegments, " ")
}

// hostLabel builds one host row's text, colored uniformly by its single
// outcome. No width-based truncation/alignment applies here - that rule is
// TASK-row-specific per TUI.md.
func hostLabel(task *taskNode, host string) string {
	o := task.Hosts[host]
	return fmt.Sprintf("[%s]%s: %s[-]", colorTag(o), tview.Escape(host), o)
}

// formatHostOutput renders task.Raw[host] for the output drill-down view.
// It decodes into a generic map (not a fixed struct) since different
// Ansible modules return wildly different result shapes; msg/stdout/stderr
// are pulled out as labeled, human-readable sections (real newlines, no
// escaping needed - the output TextView keeps style tags off) since those
// are by far the most commonly wanted fields for the common
// command/shell/script case, followed unconditionally by the complete
// result as pretty-printed JSON, which is what makes this work for any
// module type without having to special-case each one.
func formatHostOutput(task *taskNode, host string) string {
	raw := task.Raw[host]
	if len(raw) == 0 {
		// Shouldn't happen in normal operation - every host recorded via
		// recordHost always has some raw payload - but a live jsonl stream
		// from an external process isn't something to trust blindly, so
		// degrade gracefully rather than showing a blank screen.
		return fmt.Sprintf("(no output recorded for %s)", host)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		// Not a JSON object - shouldn't happen for any real module
		// result, but show the raw bytes rather than nothing.
		return string(raw)
	}

	var b strings.Builder
	writeSection := func(label, key string) {
		s, ok := decoded[key].(string)
		if !ok || s == "" {
			return
		}
		fmt.Fprintf(&b, "%s:\n%s\n\n", label, s)
	}
	writeSection("MSG", "msg")
	writeSection("STDOUT", "stdout")
	writeSection("STDERR", "stderr")

	pretty, err := json.MarshalIndent(decoded, "", "  ")
	if err != nil {
		fmt.Fprintf(&b, "FULL RESULT: (failed to format: %v)\n%s", err, string(raw))
	} else {
		fmt.Fprintf(&b, "FULL RESULT:\n%s", pretty)
	}

	return b.String()
}
