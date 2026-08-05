package main

// rawEvent is the subset of the ansible.posix.jsonl event schema this app
// cares about. Fields we don't need are simply dropped by json.Unmarshal.
type rawEvent struct {
	Event string                `json:"_event"`
	Play  *playRef              `json:"play"`
	Task  *taskRef              `json:"task"`
	Hosts map[string]hostResult `json:"hosts"`
}

type playRef struct {
	Name string `json:"name"`
}

type taskRef struct {
	Name string `json:"name"`
}

type hostResult struct {
	Changed     bool   `json:"changed"`
	Skipped     bool   `json:"skipped"`
	Failed      bool   `json:"failed"`
	Unreachable bool   `json:"unreachable"`
	Msg         string `json:"msg"`
}
