package runner

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"code.aw.net/claude/tangsible/internal/config"
)

// syncBuffer is a bytes.Buffer safe for the watcher's goroutine and the test.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func fastWatch(t *testing.T) {
	t.Helper()
	oldFirst, oldEvery := startNotice, startNoticeEvery
	startNotice, startNoticeEvery = 40*time.Millisecond, 40*time.Millisecond
	t.Cleanup(func() { startNotice, startNoticeEvery = oldFirst, oldEvery })
}

func TestQuoteArgAndCommandLine(t *testing.T) {
	for in, want := range map[string]string{
		"podman":                       "podman",
		"--name":                       "--name",
		"--network=host":               "--network=host",
		"-v":                           "-v",
		"/home/u/proj:/home/u/proj:ro": "/home/u/proj:/home/u/proj:ro",
		"--passwd-entry=$USERNAME:*:$UID:$GID:tangsible:/runner/home:/bin/sh": `'--passwd-entry=$USERNAME:*:$UID:$GID:tangsible:/runner/home:/bin/sh'`,
		"has space": `'has space'`,
		"it's":      `'it'\''s'`,
		"":          `''`,
		"a;b":       `'a;b'`,
	} {
		if got := quoteArg(in); got != want {
			t.Errorf("quoteArg(%q) = %s, want %s", in, got, want)
		}
	}
	cmd := exec.Command("podman", "run", "--rm", "-v", "/a b:/a b", "img", "ansible-playbook", "x.yml")
	if got, want := CommandLine(cmd), "podman run --rm -v '/a b:/a b' img ansible-playbook x.yml"; got != want {
		t.Errorf("CommandLine = %q, want %q", got, want)
	}
}

func TestWatchStartQuietWhenTheStartIsQuick(t *testing.T) {
	fastWatch(t)
	var out syncBuffer
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() { watchStart(&out, "podman run x", "podman try", NewOutputLog(), 0, done); close(finished) }()
	close(done) // the first event arrived at once
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("the watcher did not stop")
	}
	time.Sleep(100 * time.Millisecond)
	if out.String() != "" {
		t.Errorf("a quick start printed %q", out.String())
	}
}

func TestWatchStartReportsASlowStart(t *testing.T) {
	fastWatch(t)
	var out syncBuffer
	log := NewOutputLog()
	fmt.Fprintln(log.Stderr(), "tangsible: running: podman run x") // before the snapshot index: not "said by the engine"
	from := log.Len()
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		watchStart(&out, "podman run --rm x", "podman run --rm --userns=keep-id img true", log, from, done)
		close(finished)
	}()

	time.Sleep(90 * time.Millisecond) // past the first notice
	fmt.Fprintln(log.Stderr(), "Copying blob 1")
	fmt.Fprintln(log.Stderr(), "Copying blob 2")
	fmt.Fprintln(log.Stderr(), "Copying blob 3")
	fmt.Fprintln(log.Stderr(), "Copying blob 4")
	time.Sleep(90 * time.Millisecond) // past a repeat
	close(done)
	<-finished
	got := out.String()
	for _, want := range []string{
		"waiting for ansible-playbook to start in the container",
		"first start of a newly pulled or built image can take a while",
		"tangsible: command: podman run --rm x",
		"to try the image on its own (timing the first start): podman run --rm --userns=keep-id img true",
		"still waiting for ansible-playbook in the container",
		"the engine said: Copying blob 4",
		"the engine said: Copying blob 3",
		"the engine said: Copying blob 2", // the last three only
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Copying blob 1") || strings.Contains(got, "tangsible: running:") {
		t.Errorf("only the engine's last three lines belong in it:\n%s", got)
	}
	if strings.Count(got, "tangsible: command:") != 1 {
		t.Errorf("the command should be printed once:\n%s", got)
	}
	n := len(got)
	time.Sleep(120 * time.Millisecond)
	if len(out.String()) != n {
		t.Errorf("the watcher kept printing after done was closed")
	}
}

func TestWatchStartWithoutACommandOrALog(t *testing.T) {
	fastWatch(t)
	var out syncBuffer
	done := make(chan struct{})
	go watchStart(&out, "", "podman try", nil, 0, done) // the re-run form: the command is already in the log
	time.Sleep(90 * time.Millisecond)
	close(done)
	if got := out.String(); !strings.Contains(got, "waiting for ansible-playbook") || strings.Contains(got, "command:") || !strings.Contains(got, "podman try") {
		t.Errorf("output = %q", got)
	}
}

// A spawn in an execution environment puts the exact engine command first in
// the raw-output view. A stand-in engine that exits at once is enough.
func TestSpawnGenerationLogsTheEngineCommand(t *testing.T) {
	dir := t.TempDir()
	// The builder accepts only the engine named "podman": a stand-in of that
	// name first on PATH.
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(bin, "podman"), "#!/bin/sh\nexit 0\n", 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	engine := "podman"
	t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "cache"))
	oldState := config.TangsibleStatePath
	config.TangsibleStatePath = filepath.Join(dir, "state", "state.toml")
	t.Cleanup(func() { config.TangsibleStatePath = oldState })
	SetExecutionEnvironment(&config.EESettings{Image: "img:1", Engine: engine, PullPolicy: "missing", PassEnv: []string{"TANGSIBLE_SECRET_PROBE"}})
	t.Cleanup(func() { SetExecutionEnvironment(nil) })
	t.Setenv("TANGSIBLE_SECRET_PROBE", "must-not-appear-in-the-log")

	outLog := NewOutputLog()
	var procH ProcHandle
	cmd, stdoutCh, stderrLines, _, err := SpawnGeneration("site.yml", []string{"-i", "localhost,"}, &procH, outLog, nil)
	if err != nil {
		t.Fatal(err)
	}
	for range stdoutCh {
	}
	<-stderrLines
	cmd.Wait()

	lines := outLog.LinesFrom(0)
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "tangsible: running: "+engine+" run --rm -i --name tangsible-") {
		t.Fatalf("first line of the output log = %q", lines)
	}
	for _, want := range []string{"img:1 ansible-playbook", "site.yml", "-i localhost,", "--userns=keep-id", `'--passwd-entry=$USERNAME:`} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("the logged command lacks %q: %s", want, lines[0])
		}
	}
	if strings.Contains(lines[0], "must-not-appear-in-the-log") {
		t.Errorf("an environment value leaked into the logged command: %s", lines[0])
	}
	if !strings.Contains(lines[0], "-e TANGSIBLE_SECRET_PROBE") && strings.Contains(lines[0], "TANGSIBLE_SECRET_PROBE=") {
		t.Errorf("a passed variable appears with a value: %s", lines[0])
	}
}

func writeFile(path, content string, mode os.FileMode) error {
	return os.WriteFile(path, []byte(content), mode)
}

var _ = io.Discard

func TestTryLine(t *testing.T) {
	if got, want := tryLine("podman", "localhost/my-ee:1.0"), "podman run --rm --userns=keep-id localhost/my-ee:1.0 true"; got != want {
		t.Errorf("tryLine = %q, want %q", got, want)
	}
	if got := tryLine("podman", "odd name;rm"); !strings.Contains(got, "'odd name;rm'") {
		t.Errorf("an image name is quoted for the shell: %q", got)
	}
}
