package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRender(t *testing.T) {
	state := &playbookState{Plays: []*playNode{
		{Name: "webservers", Tasks: []*taskNode{
			{
				Name:      "install nginx",
				HostOrder: []string{"web2", "web1"}, // deliberately not alphabetical
				Hosts:     map[string]outcome{"web1": outcomeOK, "web2": outcomeFailed},
			},
		}},
	}}

	var buf bytes.Buffer
	Render(&buf, state)
	out := buf.String()

	for _, want := range []string{
		"PLAY: webservers",
		"TASK: install nginx",
		"OK: 1, Chgd: 0, Skip: 0, Fail: 1, Unrch: 0",
		"web2: Failed",
		"web1: OK",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not contain %q\nfull output:\n%s", want, out)
		}
	}

	// Host lines must follow HostOrder, not map iteration order - web2
	// comes first in HostOrder here, despite being alphabetically after
	// web1.
	if strings.Index(out, "web2: Failed") > strings.Index(out, "web1: OK") {
		t.Errorf("expected web2's line before web1's line (HostOrder), got:\n%s", out)
	}

	// The PLAY line must precede its own TASK line, which must precede its
	// own host lines - the basic tree-walk order.
	playIdx := strings.Index(out, "PLAY: webservers")
	taskIdx := strings.Index(out, "TASK: install nginx")
	hostIdx := strings.Index(out, "web2: Failed")
	if !(playIdx < taskIdx && taskIdx < hostIdx) {
		t.Errorf("expected PLAY before TASK before host lines, got:\n%s", out)
	}
}

func TestRender_NoPlays(t *testing.T) {
	var buf bytes.Buffer
	Render(&buf, &playbookState{})
	if got := buf.String(); got != "" {
		t.Errorf("Render on an empty state = %q, want empty output", got)
	}
}
