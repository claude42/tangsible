// Copyright 2026 Klaus Wissmann
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package session

import (
	"fmt"
	"strings"

	"code.aw.net/claude/tangsible/internal/playbook"
	"code.aw.net/claude/tangsible/internal/uikit"
	"github.com/rivo/tview"
)

// The recap's Messages section (design-docs/DebugMessages.md): every debug
// message of the run, grouped by hosts, below the per-host summary lines.
// The data comes from uikit.CollectDebugMessages; this file is the rows.

// messagesHeadingRowID is the section's heading row; Enter collapses and
// expands the section (s.messagesCollapsed - the section starts open, it is
// what the user is meant to notice).
type messagesHeadingRowID struct{}

// messagesUnderlineRowID is the heading's "=" underline (like the
// "Summary" heading's, recapHeadingUnderlineRowText) and
// messagesSpacerRowID the blank row below it: plain text, the cursor skips
// them.
type messagesUnderlineRowID struct{}
type messagesSpacerRowID struct{}

// messagePlayRowID is the play name above its tasks: plain text, the cursor
// skips it.
type messagePlayRowID struct{ play string }

// messageGroupRowID is the first row of one group of hosts with the same
// messages (the task name and the hosts); Enter opens the task's drill-down
// for the group's first host. messageLineRowID are the message lines below it
// and the "+N more groups" row: plain text, skipped by the cursor.
type messageGroupRowID struct {
	task  *playbook.TaskNode
	group int
}
type messageLineRowID struct {
	task        *playbook.TaskNode
	group, line int
}

func messagesHeadingLine(n int) string { return fmt.Sprintf("Messages (%d)", n) }

// messagesUnderlineText underlines the heading with "=" of its own length,
// in the heading color, as the Summary heading's underline is.
func messagesUnderlineText(n int) string {
	return "[" + uikit.HeadingColor + "::b]" + strings.Repeat("=", len(messagesHeadingLine(n))) + "[-::-]"
}

func messagesHeadingText(n int, selected bool) string {
	line := messagesHeadingLine(n)
	if selected {
		return fmt.Sprintf("[%s:%s:b]%s[-:-:-]", uikit.SelectedRowTextColor, uikit.SelectedRowBackgroundColor, line)
	}
	return "[" + uikit.HeadingColor + "::b]" + line + "[-::-]"
}

// messageGroupText is a group's first row: for the first group of a task its
// name, for a later one just the hosts.
func messageGroupText(dt uikit.DebugTask, i int, selected bool) string {
	hosts := uikit.DebugHostsLabel(dt.Groups[i], dt.HostCount)
	if selected {
		name := "    "
		if i == 0 {
			name += dt.Task.Name + "  "
		} else {
			name += "  "
		}
		return fmt.Sprintf("[%s:%s:b]%s%s[-:-:-]", uikit.SelectedRowTextColor, uikit.SelectedRowBackgroundColor, tview.Escape(name), tview.Escape(hosts))
	}
	if i == 0 {
		return fmt.Sprintf("[%s]    %s[-]  [%s]%s[-]", uikit.StandardTextColor, tview.Escape(dt.Task.Name), uikit.AnnotationColor, tview.Escape(hosts))
	}
	return fmt.Sprintf("      [%s]%s[-]", uikit.AnnotationColor, tview.Escape(hosts))
}

func messageLineText(line string) string {
	return fmt.Sprintf("[%s]        %s[-]", uikit.StandardTextColor, tview.Escape(line))
}

// flattenMessageRows is the section's rows (none when the run produced no
// message). toggle flips s.messagesCollapsed; showOutput opens the
// drill-down.
func flattenMessageRows(tasks []uikit.DebugTask, collapsed bool, toggle func(), showOutput func(task *playbook.TaskNode, host string)) []uikit.Row {
	if len(tasks) == 0 {
		return nil
	}
	rows := []uikit.Row{
		{Text: "", ID: recapDividerAfterMessagesHeadingGap},
		{Text: messagesHeadingText(len(tasks), false), ID: messagesHeadingRowID{}, Selected: toggle},
		{Text: messagesUnderlineText(len(tasks)), ID: messagesUnderlineRowID{}},
	}
	if collapsed {
		return rows
	}
	rows = append(rows, uikit.Row{Text: "", ID: messagesSpacerRowID{}})
	lastPlay := ""
	for _, dt := range tasks {
		dt := dt
		if dt.Play != lastPlay {
			lastPlay = dt.Play
			rows = append(rows, uikit.Row{
				Text: fmt.Sprintf("  [%s]%s[-]", uikit.AnnotationColor, tview.Escape(dt.Play)),
				ID:   messagePlayRowID{dt.Play},
			})
		}
		shown, more := uikit.DebugGroupsShown(len(dt.Groups))
		for gi := 0; gi < shown; gi++ {
			gi := gi
			rows = append(rows, uikit.Row{
				Text:     messageGroupText(dt, gi, false),
				ID:       messageGroupRowID{dt.Task, gi},
				Selected: func() { showOutput(dt.Task, dt.Groups[gi].Hosts[0]) },
			})
			for li, line := range uikit.DebugMessageLines(dt.Groups[gi].Messages) {
				rows = append(rows, uikit.Row{Text: messageLineText(line), ID: messageLineRowID{dt.Task, gi, li}})
			}
		}
		if more > 0 {
			rows = append(rows, uikit.Row{
				Text: fmt.Sprintf("[%s]      ... (+%d more groups)[-]", uikit.AnnotationColor, more),
				ID:   messageLineRowID{dt.Task, shown, 0},
			})
		}
	}
	return rows
}

// recapDividerAfterMessagesHeadingGap is the blank row between the recap's
// host lines and the Messages heading.
type messagesGapRowID struct{}

var recapDividerAfterMessagesHeadingGap = messagesGapRowID{}

// messagesRowSelectedText is the text of the row under the cursor with its
// selected styling, ok false for the ids that are not this section's
// selectable rows (rebuild keeps its own text then).
func (s *liveSession) messagesRowSelectedText(id any) (string, bool) {
	switch id := id.(type) {
	case messagesHeadingRowID:
		return messagesHeadingText(len(s.messageTasks), true), true
	case messageGroupRowID:
		for _, dt := range s.messageTasks {
			if dt.Task == id.task && id.group < len(dt.Groups) {
				return messageGroupText(dt, id.group, true), true
			}
		}
	}
	return "", false
}
