package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
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

// EEHistoryValue is what to record in invocation history for the current
// generation's execution environment (config.EEHistoryValue).
func EEHistoryValue() string {
	return config.EEHistoryValue(config.MergedSettingsConfig(), ExecutionEnvironment())
}

const selinuxEnforcePath = "/sys/fs/selinux/enforce"

// systemKnownHostsPath is ssh's system-wide host key list (a variable so a test
// can point it elsewhere).
var systemKnownHostsPath = "/etc/ssh/ssh_known_hosts"

// resolvConfPath is the host's resolver configuration (a variable only so a
// test can point it elsewhere).
var resolvConfPath = "/etc/resolv.conf"

// hostDNSSearch is the host's "domain" search domain that the engine would
// drop from the container's resolv.conf (ee.DomainOnlySearch); nil when the
// file can't be read or has nothing to add.
func hostDNSSearch() []string {
	b, err := os.ReadFile(resolvConfPath)
	if err != nil {
		return nil
	}
	return ee.DomainOnlySearch(string(b))
}

// ValidateExecutionEnvironment checks the active setting against the host
// before anything is pulled or started: the extra_mounts entries (a source
// that doesn't exist, an unknown option, a clashing destination) and the
// pass_env/set_env entries (bad or reserved names). Those are the rules Build
// applies at every spawn, reported here once, early, as a usage error instead
// of a failed first run. The warnings (a passed variable that isn't set) are
// for the caller to show. A no-op without an active
// execution environment.
func ValidateExecutionEnvironment() (warnings []string, err error) {
	s := ExecutionEnvironment()
	if s == nil {
		return nil, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	home, _ := os.UserHomeDir()
	if err := ee.CheckExtraMounts(s.ExtraMounts, cwd, home, os.Environ(), nil); err != nil {
		return nil, err
	}
	return ee.CheckEnv(s.PassEnv, s.SetEnv, os.Environ())
}

// refreshed records the images this process has already tried to refresh: a
// refreshing pull policy (always, tag) acts once per session, not before every
// re-run.
var (
	refreshedMu sync.Mutex
	refreshed   = map[string]bool{}
)

func markRefreshed(image string) {
	refreshedMu.Lock()
	refreshed[image] = true
	refreshedMu.Unlock()
}

func wasRefreshed(image string) bool {
	refreshedMu.Lock()
	defer refreshedMu.Unlock()
	return refreshed[image]
}

// imagePresent asks the engine whether the image is in local storage. podman's
// "image exists" answers by exit code: 0 present, 1 absent, anything else an
// error (storage trouble, say) - which must not be mistaken for "absent",
// because pulling would only bury the real problem under a second one.
func imagePresent(engine, image string) (bool, error) {
	cmd := exec.Command(engine, "image", "exists", image)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		if msg := execerr.NotFoundMessage(engine, err); msg != "" {
			return false, errors.New(msg)
		}
		return false, fmt.Errorf("couldn't run %s: %w", engine, err)
	}
	if exit.ExitCode() == 1 {
		return false, nil
	}
	detail := strings.TrimSpace(stderr.String())
	if detail == "" {
		detail = exit.Error()
	}
	return false, fmt.Errorf("couldn't check whether the image %s is available: %s", image, detail)
}

// pullArguments expands $VAR (and ~, also after "--flag=") in the configured
// pull arguments from the host, so e.g. "--authfile=~/auth.json" works.
func pullArguments(args []string) []string {
	home, _ := os.UserHomeDir()
	env := os.Environ()
	out := make([]string, 0, len(args))
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			if k, v, ok := strings.Cut(a, "="); ok {
				out = append(out, k+"="+ee.ExpandPath(v, home, env))
				continue
			}
		}
		out = append(out, ee.ExpandPath(a, home, env))
	}
	return out
}

// EnsureImage applies the pull policy to the active image before anything
// runs (ee.DecidePull): it is pulled when the policy says so, with the
// engine's own progress going to w. It runs before any TUI exists - a pull can
// take minutes and has nowhere to show itself afterwards - and again, cheaply,
// before a re-run (the dialog can name another image). A refresh of an image
// that is already here, which fails (offline), is reported and the local copy
// used. A no-op without an active execution environment.
func EnsureImage(w io.Writer) error {
	s := ExecutionEnvironment()
	if s == nil {
		return nil
	}
	present, err := imagePresent(s.Engine, s.Image)
	if err != nil {
		return err
	}
	d := ee.DecidePull(s.PullPolicy, s.Image, present, wasRefreshed(s.Image))
	if d.Err != nil {
		return d.Err
	}
	if !d.Pull {
		return nil
	}
	fmt.Fprintf(w, "tangsible: pulling %s (%s) ...\n", s.Image, d.Reason)
	args := append([]string{"pull"}, pullArguments(s.PullArguments)...)
	pull := exec.Command(s.Engine, append(args, s.Image)...)
	pull.Stdout, pull.Stderr = w, w
	perr := pull.Run()
	markRefreshed(s.Image)
	switch {
	case perr == nil:
		return nil
	case d.Optional:
		fmt.Fprintf(w, "tangsible: couldn't refresh %s (%v); using the local copy\n", s.Image, perr)
		return nil
	}
	return fmt.Errorf("couldn't pull %s: %w", s.Image, perr)
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

	in, err := baseEEInputs(s)
	if err != nil {
		run.abort()
		return nil, err
	}
	in.Playbook = playbook
	in.Args = args
	in.PluginDir = pluginDir
	in.EventDir = dir
	in.EventFile = "events"
	in.ExtraEnv = append(forceColorEnv(), "ANSIBLE_JSON_INDENT=0")
	in.Name = run.name
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

// baseEEInputs fills in what every container invocation shares: the
// working directory, the engine settings, the SSH agent socket and
// (opt-in) ~/.ssh, and the host environment.
func baseEEInputs(s *config.EESettings) (ee.Inputs, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return ee.Inputs{}, err
	}
	enforcing := ee.SELinuxEnforcing(selinuxEnforcePath)
	if enforcing && s.SELinux == "none" {
		config.QueueWarning(`tangsible: execution environment: selinux = "none" while SELinux is enforcing: the container will most likely be unable to read the project ("the playbook could not be found"); use "auto" or "disable"`)
	}
	in := ee.Inputs{
		Config: ee.Config{
			Image:       s.Image,
			Engine:      s.Engine,
			Network:     s.Network,
			ExtraMounts: s.ExtraMounts,
			SELinux:     ee.ResolveSELinux(s.SELinux, enforcing),
			ReadOnly:    s.ReadOnly,
		},
		Cwd:       cwd,
		HostEnv:   os.Environ(),
		DNSSearch: hostDNSSearch(),
		PassEnv:   s.PassEnv,
		SetEnv:    s.SetEnv,
	}
	if home, err := os.UserHomeDir(); err == nil {
		in.HomeDir = home
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
	if s.SSHKnownHosts {
		if fi, err := os.Stat(systemKnownHostsPath); err == nil && fi.Mode().IsRegular() {
			in.SSHSystemKnownHosts = systemKnownHostsPath
		}
	}
	if s.SSHKnownHosts && in.SSHDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			f := filepath.Join(home, ".ssh", "known_hosts")
			if fi, err := os.Stat(f); err == nil && fi.Mode().IsRegular() {
				in.SSHKnownHosts = f
			}
		}
	}
	return in, nil
}

// ToolOptions describe one short-lived ansible tool run for NewToolCommand.
type ToolOptions struct {
	// Playbook, if set, is the first argument (ansible-playbook's
	// playbook); it is separate from args so a container run can make it
	// reachable.
	Playbook string
	// Scratch are host directories the tool reads from and writes to
	// (stub playbooks, outputs); in a container they are mounted
	// read-write at the same path.
	Scratch []string
	// Plugin loads the callback plugin, whose events then arrive on the
	// tool's stdout (there is no event fd/FIFO for a one-shot run).
	Plugin bool
	// Env is extra NAME=value for the tool.
	Env []string
}

// NewToolCommand builds the command for a one-shot run of an ansible tool
// (ansible-playbook for a probe or stub, ansible-doc, python3 with ansible
// importable) - on the host, or in the active execution environment so the
// answer describes the same ansible, collections and Python the real run
// used. The caller sets Stdin/Stdout/Stderr and runs it.
func NewToolCommand(binary string, args []string, o ToolOptions) (*exec.Cmd, error) {
	s := ExecutionEnvironment()
	if s == nil {
		argv := args
		if o.Playbook != "" {
			argv = append([]string{o.Playbook}, args...)
		}
		cmd := exec.Command(binary, argv...)
		cmd.Env = os.Environ()
		if o.Plugin {
			pluginEnv, err := CallbackPluginEnv()
			if err != nil {
				return nil, err
			}
			cmd.Env = append(cmd.Env, pluginEnv...)
		}
		cmd.Env = append(cmd.Env, o.Env...)
		return cmd, nil
	}
	in, err := baseEEInputs(s)
	if err != nil {
		return nil, err
	}
	in.Command = binary
	in.Playbook = o.Playbook
	in.Args = args
	in.Scratch = o.Scratch
	in.ExtraEnv = o.Env
	if o.Plugin {
		if in.PluginDir, err = ResolveCallbackPluginDir(); err != nil {
			return nil, err
		}
		in.ExtraEnv = append([]string{"ANSIBLE_JSON_INDENT=0"}, o.Env...)
	}
	plan, err := ee.Build(in)
	if err != nil {
		return nil, err
	}
	for _, w := range plan.Warnings {
		config.QueueWarning("tangsible: execution environment: " + w)
	}
	cmd := exec.Command(plan.Engine, plan.Argv...)
	cmd.Env = append(os.Environ(), plan.Env...)
	return cmd, nil
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
