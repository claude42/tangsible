package runner

import (
	"bufio"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The event FIFO must neither report EOF before the (container-side)
// writer shows up, nor hang after it is gone.
func TestEventFIFOLifecycle(t *testing.T) {
	dir, reader, holder, err := newEventFIFO()
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	run := &eeRun{events: reader, holder: holder, dir: dir}

	lines := make(chan string, 8)
	done := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(reader)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("reader hit EOF before any writer existed")
	case <-time.After(100 * time.Millisecond):
	}

	// The "container": opens the FIFO by path, writes, exits.
	w, err := os.OpenFile(filepath.Join(dir, "events"), os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	w.WriteString("one\ntwo\n")
	w.Close()

	select {
	case got := <-lines:
		if got != "one" {
			t.Errorf("first line = %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no line delivered")
	}
	select {
	case <-done:
		t.Fatal("EOF while the holder is still open")
	case <-time.After(100 * time.Millisecond):
	}

	run.finish()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("no EOF after finish")
	}
	if got := <-lines; got != "two" {
		t.Errorf("buffered line lost: %q", got)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("scratch dir not removed: %v", err)
	}
}
