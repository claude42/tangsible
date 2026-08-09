package main

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestProcHandle(t *testing.T) {
	var h procHandle
	if got := h.Load(); got != nil {
		t.Errorf("Load() on a fresh procHandle = %v, want nil", got)
	}

	cmd := exec.Command("sh", "-c", "sleep 0")
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start test process: %v", err)
	}
	defer cmd.Wait()

	h.Store(cmd.Process)
	if got := h.Load(); got != cmd.Process {
		t.Errorf("Load() = %v, want %v", got, cmd.Process)
	}
}

func TestExitCodeOf(t *testing.T) {
	t.Run("nil error means a clean exit", func(t *testing.T) {
		if got := exitCodeOf(nil); got != 0 {
			t.Errorf("exitCodeOf(nil) = %d, want 0", got)
		}
	})

	t.Run("a real ExitError yields the process's actual exit code", func(t *testing.T) {
		err := exec.Command("sh", "-c", "exit 7").Run()
		if got := exitCodeOf(err); got != 7 {
			t.Errorf("exitCodeOf(%v) = %d, want 7", err, got)
		}
	})

	t.Run("a non-ExitError is not a real exit code", func(t *testing.T) {
		if got := exitCodeOf(errors.New("boom")); got != -1 {
			t.Errorf("exitCodeOf(non-ExitError) = %d, want -1", got)
		}
	})
}

func TestStreamStderr(t *testing.T) {
	t.Run("splits into lines", func(t *testing.T) {
		got := streamStderr(strings.NewReader("line one\nline two\n"))
		want := []string{"line one", "line two"}
		if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
			t.Errorf("streamStderr() = %v, want %v", got, want)
		}
	})

	t.Run("empty reader yields no lines", func(t *testing.T) {
		got := streamStderr(strings.NewReader(""))
		if len(got) != 0 {
			t.Errorf("streamStderr(empty) = %v, want no lines", got)
		}
	})
}

func TestScanEvents(t *testing.T) {
	t.Run("valid JSON line then a garbage line", func(t *testing.T) {
		input := `{"_event":"v2_playbook_on_play_start","play":{"name":"my play"}}` + "\n" +
			"not valid json at all\n"
		ch := scanEvents(strings.NewReader(input))

		first := <-ch
		if !first.isEvent || first.ev.Event != "v2_playbook_on_play_start" {
			t.Errorf("first item = %+v, want a decoded v2_playbook_on_play_start event", first)
		}

		second := <-ch
		if second.isEvent {
			t.Errorf("second item = %+v, want isEvent=false for a non-JSON line", second)
		}
		if !strings.HasPrefix(second.diag, "(not JSON) ") {
			t.Errorf("second item's diag = %q, want it prefixed \"(not JSON) \"", second.diag)
		}

		if _, ok := <-ch; ok {
			t.Error("expected the channel to be closed after both lines were consumed")
		}
	})

	t.Run("v2_playbook_on_stats is both an event and a diagnostic", func(t *testing.T) {
		// The two aren't mutually exclusive - documented explicitly on
		// streamItem - worth pinning down so a future change doesn't
		// accidentally make them exclusive.
		input := `{"_event":"v2_playbook_on_stats","stats":{"web1":{"ok":1}}}` + "\n"
		ch := scanEvents(strings.NewReader(input))

		item := <-ch
		if !item.isEvent {
			t.Error("expected isEvent=true for v2_playbook_on_stats")
		}
		if item.diag == "" {
			t.Error("expected a non-empty diag for v2_playbook_on_stats")
		}
	})

	t.Run("empty reader closes the channel immediately", func(t *testing.T) {
		ch := scanEvents(strings.NewReader(""))
		if _, ok := <-ch; ok {
			t.Error("expected the channel to close with no items for an empty reader")
		}
	})
}
