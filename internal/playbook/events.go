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

package playbook

import (
	"encoding/json"
	"time"
)

// RawEvent is the subset of the ansible.posix.jsonl event schema this app
// cares about. Fields we don't need are simply dropped by json.Unmarshal.
// Hosts keeps each host's full original bytes (rather than decoding
// straight into HostResult) so the complete result - not just the fields
// below - can be recorded and shown later; see aggregate.go's TaskNode.Raw.
type RawEvent struct {
	Event string                     `json:"_event"`
	Play  *PlayRef                   `json:"play"`
	Task  *TaskRef                   `json:"task"`
	Hosts map[string]json.RawMessage `json:"hosts"`

	// TimestampText is the event's own "_timestamp" (empirically RFC3339
	// with fractional seconds, e.g. "2026-08-06T12:41:02.439015Z").
	// Deliberately a plain string, not time.Time: a time.Time field's own
	// UnmarshalJSON failing on a malformed value would make this whole
	// struct's json.Unmarshal call return an error, which main.go's
	// streamEvents treats as "not JSON" and drops the entire event - task
	// and hosts included - over nothing but a bad timestamp. A string
	// field can't fail that way; Timestamp() below does the fallible
	// parsing on demand and swallows any error, same convention as
	// DecodeHostResult.
	TimestampText string `json:"_timestamp"`
}

// Timestamp parses TimestampText as RFC3339, returning the zero time.Time
// if it's missing or malformed. Callers must treat a zero result as
// "unknown," never as the epoch.
func (ev RawEvent) Timestamp() time.Time {
	t, _ := time.Parse(time.RFC3339, ev.TimestampText)
	return t
}

type PlayRef struct {
	Name string `json:"name"`
}

type TaskRef struct {
	Name string `json:"name"`
	// Path is the task's own source location, "<absolute file>:<line>",
	// exactly as Ansible reports it on every event - used by tui.go's
	// output drill-down view to look up the task's raw source text via
	// source.go's taskSourceIndex.
	Path string `json:"path"`
}

// HostResult is the handful of classification fields Apply needs to decide
// a host's outcome for one task. It's deliberately not the full result
// shape - different modules return wildly different fields (command:
// stdout/stderr/cmd/rc; most modules: msg on failure; others: arbitrary
// module-specific fields) and hand-modeling all of them isn't worth it. The
// full original bytes are kept separately (TaskNode.Raw) for on-demand
// display.
type HostResult struct {
	Changed     bool   `json:"changed"`
	Skipped     bool   `json:"skipped"`
	Failed      bool   `json:"failed"`
	Unreachable bool   `json:"unreachable"`
	Msg         string `json:"msg"`
}

// DecodeHostResult extracts HostResult's classification fields from one
// host's raw JSON payload. A decode error is treated as "no flags set"
// rather than surfaced - a malformed/unexpected shape for these fields
// shouldn't prevent the host's full raw payload from still being recorded.
func DecodeHostResult(raw json.RawMessage) HostResult {
	var r HostResult
	_ = json.Unmarshal(raw, &r)
	return r
}

// hasNonEmptyWarnings reports whether raw carries a non-empty "warnings"
// field - the same "presence and non-empty" rule uikit's own HasWarnings
// (tui.go, the drill-down/tree-marker package) already follows, and
// deliberately kept in sync with it: both must agree on what "this result
// has a warning" means. Not a call to that function directly - this
// package can't import uikit (uikit already imports this one) - but the
// full text-joining JoinedStringList does for multi-shape "warnings"
// values isn't needed here either, only a yes/no answer, so decoding into
// a single json.RawMessage field (cheap - no map[string]interface{} built
// for the rest of the payload) and checking its own shape directly is a
// smaller, self-contained equivalent, not a full port.
func hasNonEmptyWarnings(raw json.RawMessage) bool {
	var decoded struct {
		Warnings json.RawMessage `json:"warnings"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil || len(decoded.Warnings) == 0 {
		return false
	}
	var single string
	if json.Unmarshal(decoded.Warnings, &single) == nil {
		return single != ""
	}
	// A list, per JoinedStringList's own handling - decoded element-by-
	// element into json.RawMessage first (never []string directly), so
	// one non-string entry doesn't fail the whole array the way it would
	// decoding straight into []string; JoinedStringList skips a non-string
	// entry individually instead, and this mirrors that exactly rather
	// than drifting to a stricter, easier-to-write shortcut.
	var list []json.RawMessage
	if json.Unmarshal(decoded.Warnings, &list) == nil {
		for _, elem := range list {
			var s string
			if json.Unmarshal(elem, &s) == nil && s != "" {
				return true
			}
		}
	}
	return false
}

// hasNonEmptyStderr reports whether raw carries a non-empty "stderr"
// field - the same field tui_drilldown.go's own Errors section reads
// (BuildOutputTabs: `stderr, _ := decoded["stderr"].(string)`). Unlike
// warnings, ansible's own "stderr" field is always a plain string, never
// a list - command/shell-family modules are the only ones that ever set
// it at all, so there's no multi-shape case to handle the way
// hasNonEmptyWarnings has to.
func hasNonEmptyStderr(raw json.RawMessage) bool {
	var decoded struct {
		Stderr string `json:"stderr"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return false
	}
	return decoded.Stderr != ""
}
