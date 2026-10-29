package ansibleconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeAnsibleConfig puts an ansible-config on PATH that appends a line to a
// counter file per invocation and prints a one-entry dump.
func fakeAnsibleConfig(t *testing.T) (calls func() int) {
	t.Helper()
	dir := t.TempDir()
	counter := filepath.Join(dir, "calls")
	script := "#!/bin/sh\necho x >> " + counter + "\necho '[{\"name\":\"CALLBACKS_ENABLED\",\"value\":[\"timer\"]}]'\n"
	if err := os.WriteFile(filepath.Join(dir, "ansible-config"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	mu.Lock()
	cur = nil
	mu.Unlock()
	return func() int {
		b, _ := os.ReadFile(counter)
		return strings.Count(string(b), "x")
	}
}

func TestPrefetchIsSharedAndFreshnessRespected(t *testing.T) {
	calls := fakeAnsibleConfig(t)

	Prefetch()
	Prefetch()
	entries := Get(time.Minute)
	if v, ok := Find(entries, "CALLBACKS_ENABLED"); !ok || string(v) != `["timer"]` {
		t.Fatalf("unexpected entries: %+v", entries)
	}
	Get(time.Minute)
	if n := calls(); n != 1 {
		t.Fatalf("prefetch + two fresh-enough Gets ran ansible-config %d times, want 1", n)
	}

	time.Sleep(5 * time.Millisecond)
	Get(time.Millisecond) // older than maxAge: redone
	if n := calls(); n != 2 {
		t.Fatalf("stale dump should be redone: %d runs, want 2", n)
	}
}

func TestGetWithoutAnsibleConfig(t *testing.T) {
	mu.Lock()
	cur = nil
	mu.Unlock()
	t.Setenv("PATH", t.TempDir())
	if got := Get(time.Minute); got != nil {
		t.Fatalf("got %+v, want nil", got)
	}
}
