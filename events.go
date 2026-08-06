package main

import "encoding/json"

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
}

type playRef struct {
	Name string `json:"name"`
}

type taskRef struct {
	Name string `json:"name"`
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
