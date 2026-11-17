package runner

import (
	"bufio"
	"code.aw.net/claude/tangsible/internal/config"
	"os"
	"path/filepath"
	"reflect"
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

func TestHostDNSSearch(t *testing.T) {
	old := resolvConfPath
	defer func() { resolvConfPath = old }()

	resolvConfPath = filepath.Join(t.TempDir(), "resolv.conf")
	if got := hostDNSSearch(); got != nil {
		t.Errorf("missing file: %q, want nil", got)
	}
	if err := os.WriteFile(resolvConfPath, []byte("domain aw.net\nnameserver 127.0.0.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := hostDNSSearch(); !reflect.DeepEqual(got, []string{"aw.net"}) {
		t.Errorf("domain file: %q, want [aw.net]", got)
	}
	if err := os.WriteFile(resolvConfPath, []byte("search aw.net\nnameserver 127.0.0.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := hostDNSSearch(); got != nil {
		t.Errorf("search file: %q, want nil (the engine copies a search line)", got)
	}
}

func TestBaseEEInputsSystemKnownHosts(t *testing.T) {
	old := systemKnownHostsPath
	defer func() { systemKnownHostsPath = old }()
	dir := t.TempDir()
	systemKnownHostsPath = filepath.Join(dir, "ssh_known_hosts")

	on := &config.EESettings{Image: "img", Engine: "podman", SSHKnownHosts: true}
	off := &config.EESettings{Image: "img", Engine: "podman", SSHKnownHosts: false}

	in, err := baseEEInputs(on)
	if err != nil || in.SSHSystemKnownHosts != "" {
		t.Errorf("file missing: %q, %v", in.SSHSystemKnownHosts, err)
	}
	if err := os.WriteFile(systemKnownHostsPath, []byte("h ssh-ed25519 AAAA\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if in, _ = baseEEInputs(on); in.SSHSystemKnownHosts != systemKnownHostsPath {
		t.Errorf("file present, ssh_known_hosts on: %q", in.SSHSystemKnownHosts)
	}
	if in, _ = baseEEInputs(off); in.SSHSystemKnownHosts != "" {
		t.Errorf("ssh_known_hosts off must not mount it: %q", in.SSHSystemKnownHosts)
	}
	// A directory of that name is not the file ssh reads.
	systemKnownHostsPath = dir
	if in, _ = baseEEInputs(on); in.SSHSystemKnownHosts != "" {
		t.Errorf("a directory was taken for the file: %q", in.SSHSystemKnownHosts)
	}
}
