package main

import (
	"encoding/json"
	"time"
)

// rawEvent is the subset of the ansible.posix.jsonl event schema this app
// cares about. Fields we don't need are simply dropped by json.Unmarshal.
// Hosts keeps each host's full original bytes (rather than decoding
// straight into hostResult) so the complete result - not just the fields
// below - can be recorded and shown later; see aggregate.go's taskNode.Raw.
type rawEvent struct {
	Event string                     `json:"_event"`
	Play  *playRef                   `json:"play"`
	Task  *taskRef                   `json:"task"`
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
	// decodeHostResult.
	TimestampText string `json:"_timestamp"`
}

// Timestamp parses TimestampText as RFC3339, returning the zero time.Time
// if it's missing or malformed. Callers must treat a zero result as
// "unknown," never as the epoch.
func (ev rawEvent) Timestamp() time.Time {
	t, _ := time.Parse(time.RFC3339, ev.TimestampText)
	return t
}

type playRef struct {
	Name string `json:"name"`
	// Path is the play's own source location, "<absolute file>:<line>",
	// the same shape as taskRef.Path - used by tui.go's output drill-down
	// view to look up the parent play's own raw source text via
	// source.go's taskSourceIndex, the same way a task's own Path already
	// does.
	Path string `json:"path"`
}

type taskRef struct {
	Name string `json:"name"`
	// Path is the task's own source location, "<absolute file>:<line>",
	// exactly as Ansible reports it on every event - used by tui.go's
	// output drill-down view to look up the task's raw source text via
	// source.go's taskSourceIndex.
	Path string `json:"path"`
}

// hostResult is the handful of classification fields Apply needs to decide
// a host's outcome for one task. It's deliberately not the full result
// shape - different modules return wildly different fields (command:
// stdout/stderr/cmd/rc; most modules: msg on failure; others: arbitrary
// module-specific fields) and hand-modeling all of them isn't worth it. The
// full original bytes are kept separately (taskNode.Raw) for on-demand
// display.
type hostResult struct {
	Changed     bool   `json:"changed"`
	Skipped     bool   `json:"skipped"`
	Failed      bool   `json:"failed"`
	Unreachable bool   `json:"unreachable"`
	Msg         string `json:"msg"`
}

// decodeHostResult extracts hostResult's classification fields from one
// host's raw JSON payload. A decode error is treated as "no flags set"
// rather than surfaced - a malformed/unexpected shape for these fields
// shouldn't prevent the host's full raw payload from still being recorded.
func decodeHostResult(raw json.RawMessage) hostResult {
	var r hostResult
	_ = json.Unmarshal(raw, &r)
	return r
}
