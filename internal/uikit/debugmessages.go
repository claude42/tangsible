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

package uikit

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"code.aw.net/claude/tangsible/internal/playbook"
)

// The debug module's output as a message to the person running the
// playbook (design-docs/DebugMessages.md): which results count, and how
// the hosts that report the same text are grouped. Pure; the rows that show
// it live in internal/session.

// Caps of the recap's Messages section, applied by DebugMessageLines and
// DebugGroupsShown (the data itself is never cut).
const (
	maxMessageLines = 5 // lines of one message
	maxMessageItems = 3 // messages (loop items) of one group
	maxMessageGroup = 3 // groups of one task
	maxGroupHosts   = 3 // hosts named in a group's label
)

// DebugMessagesOf returns the messages one host's result for a task holds,
// or nil when it holds none. A result counts when its module is debug, it
// was not skipped or failed (a debug task skipped by when:, or by
// verbosity:, reports skipped), no_log did not censor it, and the text is
// not empty - the text is the one the drill-down shows
// (PrimaryOutputField's msg, or debug's var: value). A loop's result holds
// one message per item (results[]); an item that was skipped or failed is
// not one, and the loop's own "All items completed" is never one. An item's
// label is put in front of its text, "label: text", because the items of a
// loop would otherwise be indistinguishable.
func DebugMessagesOf(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var decoded map[string]interface{}
	if json.Unmarshal(raw, &decoded) != nil || ModuleShortName(decoded) != "debug" {
		return nil
	}
	if _, ok := decoded["results"]; ok {
		items, _ := decoded["results"].([]interface{})
		var out []string
		for _, it := range items {
			m, ok := it.(map[string]interface{})
			if !ok {
				continue
			}
			text, ok := debugMessageText(m)
			if !ok {
				continue
			}
			if label, ok := m["_ansible_item_label"]; ok {
				text = fmt.Sprintf("%v: %s", label, text)
			}
			out = append(out, text)
		}
		return out
	}
	if text, ok := debugMessageText(decoded); ok {
		return []string{text}
	}
	return nil
}

// debugMessageText is the one result's (or loop item's) text, ok false when
// it is not a message: skipped, failed, censored by no_log, or empty.
func debugMessageText(m map[string]interface{}) (string, bool) {
	if flag(m, "skipped") || flag(m, "failed") {
		return "", false
	}
	if _, censored := m["censored"]; censored {
		return "", false
	}
	text := ""
	if msg, ok := m["msg"]; ok {
		text = DebugValueText(msg)
	}
	if text == "" {
		if v, ok := DebugVarValue(m); ok {
			text = v
		}
	}
	if text == "" {
		return "", false
	}
	return text, true
}

func flag(m map[string]interface{}, key string) bool {
	b, _ := m[key].(bool)
	return b
}

// DebugGroup is the hosts of one task that report exactly the same messages.
type DebugGroup struct {
	Hosts    []string
	Messages []string
}

// DebugTask is one task of the run with its debug messages, grouped.
type DebugTask struct {
	Play   string
	Task   *playbook.TaskNode
	Groups []DebugGroup
	// HostCount is every host that reported any result for the task, which
	// is what "all N hosts" means.
	HostCount int
}

// CollectDebugMessages walks the run in order (plays, then tasks - handlers
// where they ran) and returns the tasks with at least one message. Hosts
// that report the same messages form one group; hosts and groups are in
// alphabetical order of the (first) host name.
func CollectDebugMessages(state *playbook.PlaybookState) []DebugTask {
	var out []DebugTask
	for _, play := range state.Plays {
		for _, task := range play.Tasks {
			var groups []DebugGroup
			byKey := map[string]int{}
			// hosts report in whatever order they finish: by name, so the
			// groups come out the same way every time (the recap's own
			// host lines are alphabetical too)
			hosts := append([]string(nil), task.HostOrder...)
			sort.Strings(hosts)
			for _, host := range hosts {
				if o := task.Hosts[host]; o != playbook.OutcomeOK && o != playbook.OutcomeChanged {
					continue
				}
				msgs := DebugMessagesOf(task.Raw[host])
				if len(msgs) == 0 {
					continue
				}
				key := strings.Join(msgs, "\x00")
				if i, ok := byKey[key]; ok {
					groups[i].Hosts = append(groups[i].Hosts, host)
					continue
				}
				byKey[key] = len(groups)
				groups = append(groups, DebugGroup{Hosts: []string{host}, Messages: msgs})
			}
			if len(groups) > 0 {
				out = append(out, DebugTask{Play: play.Name, Task: task, Groups: groups, HostCount: len(task.Hosts)})
			}
		}
	}
	return out
}

// DebugHostsLabel names a group's hosts: the host itself when the task ran on
// one host, "all N hosts" when the group is every host that ran it, else the
// first few names and "+N hosts".
func DebugHostsLabel(g DebugGroup, hostCount int) string {
	switch {
	case hostCount <= 1 && len(g.Hosts) == 1:
		return g.Hosts[0]
	case len(g.Hosts) >= hostCount:
		return fmt.Sprintf("all %d hosts", len(g.Hosts))
	case len(g.Hosts) <= maxGroupHosts:
		return strings.Join(g.Hosts, ", ")
	}
	return fmt.Sprintf("%s, +%d hosts", strings.Join(g.Hosts[:maxGroupHosts], ", "), len(g.Hosts)-maxGroupHosts)
}

// DebugMessageLines turns a group's messages into the lines the recap shows:
// at most maxMessageItems messages, each at most maxMessageLines lines, with
// a "... (+N more ...)" line for what was left out.
func DebugMessageLines(messages []string) []string {
	var lines []string
	for i, m := range messages {
		if i == maxMessageItems {
			lines = append(lines, fmt.Sprintf("... (+%d more items)", len(messages)-maxMessageItems))
			break
		}
		ml := strings.Split(strings.TrimRight(m, "\n"), "\n")
		if len(ml) > maxMessageLines {
			lines = append(lines, ml[:maxMessageLines]...)
			lines = append(lines, fmt.Sprintf("... (+%d more lines)", len(ml)-maxMessageLines))
			continue
		}
		lines = append(lines, ml...)
	}
	return lines
}

// DebugGroupsShown is how many of a task's groups the recap lists, and how
// many it leaves out.
func DebugGroupsShown(n int) (shown, more int) {
	if n > maxMessageGroup {
		return maxMessageGroup, n - maxMessageGroup
	}
	return n, 0
}
