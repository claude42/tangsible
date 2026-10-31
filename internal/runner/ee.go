package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"

	"code.aw.net/claude/tangsible/internal/config"
	"code.aw.net/claude/tangsible/internal/ee"
	"code.aw.net/claude/tangsible/internal/execerr"
)

// The live generations of "run"/"rerun"/"role" (and a rerun started from
// "revisit") run inside an execution environment when one is active
// (design-docs/ExecutionEnvironments.md). The setting is process-wide state
// like config.TangsibleStatePath: set once by session.Main before any
// generation spawns, read by SpawnGeneration. nil means a plain host run.
var eeActive atomic.Pointer[config.EESettings]

// SetExecutionEnvironment activates (or, with nil, deactivates) running
// generations inside a container.
func SetExecutionEnvironment(s *config.EESettings) { eeActive.Store(s) }

// ExecutionEnvironment returns the active setting, or nil for host runs.
func ExecutionEnvironment() *config.EESettings { return eeActive.Load() }

const selinuxEnforcePath = "/sys/fs/selinux/enforce"

// EnsureImage makes sure the active image is present locally, pulling it
// (with the engine's own progress output going to w) when it isn't. It runs
// before any TUI exists - a pull can take minutes and has nowhere to show
// itself afterwards. A no-op without an active execution environment.
func EnsureImage(w io.Writer) error {
	s := ExecutionEnvironment()
	if s == nil {
		return nil
	}
	exists := exec.Command(s.Engine, "image", "exists", s.Image)
	err := exists.Run()
	if err == nil {
		return nil
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		if msg := execerr.NotFoundMessage(s.Engine, err); msg != "" {
			return errors.New(msg)
		}
		return fmt.Errorf("couldn't run %s: %w", s.Engine, err)
	}
	fmt.Fprintf(w, "tangsible: pulling %s ...\n", s.Image)
	pull := exec.Command(s.Engine, "pull", s.Image)
	pull.Stdout, pull.Stderr = w, w
	if err := pull.Run(); err != nil {
		return fmt.Errorf("couldn't pull %s: %w", s.Image, err)
	}
	return nil
}

// eeRun is one container invocation's host-side plumbing: the scratch
// directory holding the event FIFO and the two ends we keep open on it.
type eeRun struct {
	cmd    *exec.Cmd
	events *os.File // read end, handed to ScanEvents
	holder *os.File // write end that keeps events from seeing EOF early
	dir    string
	name   string
}

// finish is called once the engine process is gone: closing our own write
// end lets events reach EOF as soon as the FIFO's buffered data is drained,
// and the scratch directory goes away.
func (r *eeRun) finish() {
	r.holder.Close()
	os.RemoveAll(r.dir)
}

// abort undoes a failed start.
func (r *eeRun) abort() {
	r.events.Close()
	r.finish()
}

// newEventFIFO creates the scratch directory with its "events" FIFO and
// opens both host-side ends. The reader is opened non-blocking (a blocking
// open waits for a writer); the holder then supplies the writer a FIFO
// needs so the reader doesn't see EOF before the container's plugin has
// opened it. Closing the holder is what finally lets the reader reach EOF.
func newEventFIFO() (dir string, reader, holder *os.File, err error) {
	dir, err = os.MkdirTemp("", "tangsible-ee-")
	if err != nil {
		return "", nil, nil, err
	}
	fifo := filepath.Join(dir, "events")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		os.RemoveAll(dir)
		return "", nil, nil, fmt.Errorf("couldn't create the event pipe: %w", err)
	}
	reader, err = os.OpenFile(fifo, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		os.RemoveAll(dir)
		return "", nil, nil, err
	}
	holder, err = os.OpenFile(fifo, os.O_WRONLY, 0)
	if err != nil {
		reader.Close()
		os.RemoveAll(dir)
		return "", nil, nil, err
	}
	return dir, reader, holder, nil
}

var containerSeq atomic.Int64

// activeContainer names the container of the most recently spawned
// generation, for KillActiveContainer's fallback.
var activeContainer atomic.Pointer[string]

// newEERun builds the engine command for one generation and its event
// FIFO. args are the full ansible-playbook arguments (including --diff).
func newEERun(s *config.EESettings, playbook string, args []string) (*eeRun, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	pluginDir, err := ResolveCallbackPluginDir()
	if err != nil {
		return nil, err
	}
	dir, reader, holder, err := newEventFIFO()
	if err != nil {
		return nil, err
	}
	run := &eeRun{events: reader, holder: holder, dir: dir,
		name: fmt.Sprintf("tangsible-%d-%d", os.Getpid(), containerSeq.Add(1))}

	in := ee.Inputs{
		Config: ee.Config{
			Image:       s.Image,
			Engine:      s.Engine,
			Network:     s.Network,
			ExtraMounts: s.ExtraMounts,
			SELinux:     ee.ResolveSELinux(s.SELinux, ee.SELinuxEnforcing(selinuxEnforcePath)),
			ReadOnly:    s.ReadOnly,
		},
		Cwd:       cwd,
		Playbook:  playbook,
		Args:      args,
		PluginDir: pluginDir,
		EventDir:  dir,
		EventFile: "events",
		HostEnv:   os.Environ(),
		ExtraEnv:  append(forceColorEnv(), "ANSIBLE_JSON_INDENT=0"),
		Name:      run.name,
	}
	if s.SSHAgent {
		if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
			if fi, err := os.Stat(sock); err == nil && fi.Mode()&os.ModeSocket != 0 {
				in.SSHAuthSock = sock
			}
		}
	}
	if s.SSHDir {
		if home, err := os.UserHomeDir(); err == nil {
			d := filepath.Join(home, ".ssh")
			if fi, err := os.Stat(d); err == nil && fi.IsDir() {
				in.SSHDir = d
			}
		}
	}
	plan, err := ee.Build(in)
	if err != nil {
		run.abort()
		return nil, err
	}
	for _, w := range plan.Warnings {
		config.QueueWarning("tangsible: execution environment: " + w)
	}
	run.cmd = exec.Command(plan.Engine, plan.Argv...)
	run.cmd.Env = append(os.Environ(), plan.Env...)
	name := run.name
	activeContainer.Store(&name)
	return run, nil
}

// KillActiveContainer force-stops the most recent generation's container,
// best effort: for when Ctrl-C's graceful path (SIGINT to the engine
// process) didn't end it and tangsible is about to exit, which would
// otherwise leave the container running. A no-op without one.
func KillActiveContainer() {
	s := ExecutionEnvironment()
	name := activeContainer.Load()
	if s == nil || name == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = exec.CommandContext(ctx, s.Engine, "kill", *name).Run()
}
