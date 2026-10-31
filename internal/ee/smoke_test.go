package ee

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestPodmanSmoke runs a real playbook through a Build()-produced command
// and reads the event stream back over the FIFO. Opt in with
// TANGSIBLE_EE_IMAGE=<image> (needs rootless podman and the image).
func TestPodmanSmoke(t *testing.T) {
	image := os.Getenv("TANGSIBLE_EE_IMAGE")
	if image == "" {
		t.Skip("TANGSIBLE_EE_IMAGE not set")
	}
	if _, err := exec.LookPath("podman"); err != nil {
		t.Skip("podman not installed")
	}
	repo, _ := filepath.Abs("../..")
	events := t.TempDir()
	fifo := filepath.Join(events, "events")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	// Held read-write so the plugin's open-for-write never blocks or sees EOF.
	rw, err := os.OpenFile(fifo, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()

	p, err := Build(Inputs{
		Config:    Config{Image: image, ReadOnly: true},
		Cwd:       repo,
		Playbook:  "testdata/outcomes.yml",
		Args:      []string{"-i", "localhost,"},
		PluginDir: filepath.Join(repo, "callback"),
		EventDir:  events,
		EventFile: "events",
		HostEnv:   os.Environ(),
		Name:      "tgs-smoke-" + filepath.Base(events),
	})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(p.Engine, p.Argv...)
	cmd.Env = append(os.Environ(), p.Env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("podman run: %v\n%s", err, out)
	}

	rw.SetReadDeadline(time.Now().Add(2 * time.Second))
	var kinds []string
	sc := bufio.NewScanner(rw)
	for sc.Scan() {
		kinds = append(kinds, sc.Text())
	}
	if len(kinds) == 0 || !strings.Contains(kinds[0], "v2_playbook_on_play_start") ||
		!strings.Contains(kinds[len(kinds)-1], "v2_playbook_on_stats") {
		t.Fatalf("unexpected event stream (%d lines): %.200v", len(kinds), kinds)
	}
}
