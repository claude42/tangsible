// Package ee builds the container command line that runs one
// ansible-playbook generation inside an execution environment image
// (design-docs/ExecutionEnvironments.md). Everything here is pure: it takes
// a fully resolved Inputs and returns the argv, the extra environment and
// any warnings, touching the filesystem only through Inputs.Exists, so the
// whole mount/path-rewriting policy is table-testable. Spawning, pulling
// and cleanup belong to internal/runner.
package ee

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Fixed container-side layout. /runner/project is where the project is
// mounted (what ansible-navigator users expect); the rest sits beside it.
const (
	ContainerRoot   = "/runner/project"
	ContainerPlugin = "/runner/plugin"
	ContainerEvents = "/runner/events"
	ContainerHome   = "/runner"
	ContainerSock   = "/runner/ssh-agent.sock"
	containerExt    = "/runner/ext"
)

// DefaultEngine is the only engine supported so far.
const DefaultEngine = "podman"

// Config is the user-facing [execution_environment] table.
type Config struct {
	Image       string
	Engine      string   // "" means DefaultEngine
	Network     string   // "" leaves the engine's default; "host" for real remote hosts
	ExtraMounts []string // host:container[:opts]
	// SELinux is already resolved (see ResolveSELinux): "disable", "z",
	// "Z" or "none"/"".
	SELinux string
	// ReadOnly mounts the project read-only.
	ReadOnly bool
}

// Inputs is everything Build needs; the caller resolves policy (is there an
// agent, should ~/.ssh be mounted) and passes plain values.
type Inputs struct {
	Config
	Cwd      string   // host working directory, absolute
	Playbook string   // host path, "" if there is none (e.g. --version)
	Args     []string // passthrough args, host paths as the user typed them
	// PluginDir holds the callback plugin; EventDir is a writable scratch
	// directory holding the event FIFO named EventFile.
	PluginDir string
	EventDir  string
	EventFile string
	// SSHAuthSock and SSHDir are mounted when non-empty; SSHDir read-only.
	SSHAuthSock string
	SSHDir      string
	HostEnv     []string // os.Environ()
	ExtraEnv    []string // NAME=value set in the container (colors, json indent)
	Name        string   // container name, for a fallback kill
	// Exists reports whether a host path exists; nil means os.Stat.
	Exists func(string) bool
}

// Mount is one bind mount.
type Mount struct {
	Host, Container string
	ReadOnly        bool
}

// Plan is the result of Build.
type Plan struct {
	Engine string
	// Argv is everything after the engine binary, starting with "run".
	Argv []string
	// Env is NAME=value pairs the caller must add to the engine process's
	// own environment: Argv names them with a bare `-e NAME`, so values
	// (which may be secrets) never appear on a command line.
	Env      []string
	Mounts   []Mount
	Workdir  string
	Warnings []string
	// Dropped lists host ANSIBLE_* variables deliberately not forwarded.
	Dropped []string
}

// Build assembles the container command for in.
func Build(in Inputs) (*Plan, error) {
	engine := in.Engine
	if engine == "" {
		engine = DefaultEngine
	}
	if engine != DefaultEngine {
		return nil, fmt.Errorf("execution environment engine %q is not supported (only %s)", engine, DefaultEngine)
	}
	if in.Image == "" {
		return nil, fmt.Errorf("no execution environment image configured")
	}
	if !filepath.IsAbs(in.Cwd) {
		return nil, fmt.Errorf("working directory %q is not absolute", in.Cwd)
	}
	switch in.SELinux {
	case "", "none", "disable", "z", "Z":
	default:
		return nil, fmt.Errorf("unknown selinux setting %q (want disable, z, Z or none)", in.SELinux)
	}
	exists := in.Exists
	if exists == nil {
		exists = func(p string) bool { _, err := os.Stat(p); return err == nil }
	}

	root := filepath.Clean(in.Cwd)
	if in.Playbook != "" {
		pbDir := filepath.Dir(absPath(in.Cwd, in.Playbook))
		root = commonAncestor(root, pbDir)
	}
	if root == string(filepath.Separator) {
		return nil, fmt.Errorf("the playbook and the working directory share no directory below /; run from a common parent")
	}

	p := &Plan{Engine: engine}
	m := &mapper{cwd: in.Cwd, root: root, exists: exists, plan: p}
	p.Mounts = append(p.Mounts, Mount{Host: root, Container: ContainerRoot, ReadOnly: in.ReadOnly})
	p.Workdir = m.inside(filepath.Clean(in.Cwd))

	if in.PluginDir != "" {
		p.Mounts = append(p.Mounts, Mount{Host: in.PluginDir, Container: ContainerPlugin, ReadOnly: true})
	}
	if in.EventDir != "" {
		p.Mounts = append(p.Mounts, Mount{Host: in.EventDir, Container: ContainerEvents})
	}
	if in.SSHDir != "" {
		p.Mounts = append(p.Mounts, Mount{Host: in.SSHDir, Container: ContainerHome + "/.ssh", ReadOnly: true})
	}
	if in.SSHAuthSock != "" {
		p.Mounts = append(p.Mounts, Mount{Host: in.SSHAuthSock, Container: ContainerSock})
	}

	playbook := ""
	if in.Playbook != "" {
		playbook = m.translate(in.Playbook)
	}
	args := rewriteArgs(in.Args, m)

	// Extra mounts come last so user-supplied ones can't shadow ours by
	// ordering alone (a duplicate container path is rejected by the engine).
	extra, err := parseExtraMounts(in.ExtraMounts)
	if err != nil {
		return nil, err
	}
	p.Mounts = append(p.Mounts, extra...)

	fixed := []string{"HOME=" + ContainerHome}
	if in.PluginDir != "" {
		fixed = append(fixed, "ANSIBLE_CALLBACK_PLUGINS="+ContainerPlugin)
	}
	if in.EventDir != "" && in.EventFile != "" {
		fixed = append(fixed, "TANGSIBLE_EVENT_FILE="+ContainerEvents+"/"+in.EventFile)
	}
	if in.SSHAuthSock != "" {
		fixed = append(fixed, "SSH_AUTH_SOCK="+ContainerSock)
	}
	forwarded, dropped := forwardEnv(in.HostEnv, in.ExtraEnv, fixed)
	p.Env, p.Dropped = forwarded, dropped

	argv := []string{"run", "--rm", "-i"}
	if in.Name != "" {
		argv = append(argv, "--name", in.Name)
	}
	argv = append(argv, "--userns=keep-id")
	if in.Network != "" {
		argv = append(argv, "--network="+in.Network)
	}
	if in.SELinux == "disable" {
		argv = append(argv, "--security-opt", "label=disable")
	}
	argv = append(argv, "-w", p.Workdir)
	for _, mt := range p.Mounts {
		argv = append(argv, "-v", mt.spec(in.SELinux))
	}
	for _, kv := range fixed {
		argv = append(argv, "-e", kv)
	}
	for _, kv := range p.Env {
		argv = append(argv, "-e", kv[:strings.IndexByte(kv, '=')])
	}
	argv = append(argv, in.Image, "ansible-playbook")
	if playbook != "" {
		argv = append(argv, playbook)
	}
	p.Argv = append(argv, args...)
	return p, nil
}

func (mt Mount) spec(selinux string) string {
	var opts []string
	if mt.ReadOnly {
		opts = append(opts, "ro")
	}
	if selinux == "z" || selinux == "Z" {
		opts = append(opts, selinux)
	}
	s := mt.Host + ":" + mt.Container
	if len(opts) > 0 {
		s += ":" + strings.Join(opts, ",")
	}
	return s
}

func parseExtraMounts(specs []string) ([]Mount, error) {
	var out []Mount
	for _, spec := range specs {
		f := strings.Split(spec, ":")
		if len(f) < 2 || len(f) > 3 || !strings.HasPrefix(f[0], "/") || !strings.HasPrefix(f[1], "/") {
			return nil, fmt.Errorf("extra mount %q: want /host/path:/container/path[:ro]", spec)
		}
		mt := Mount{Host: f[0], Container: f[1]}
		if len(f) == 3 {
			switch f[2] {
			case "ro":
				mt.ReadOnly = true
			case "rw", "":
			default:
				return nil, fmt.Errorf("extra mount %q: unsupported option %q (want ro or rw)", spec, f[2])
			}
		}
		out = append(out, mt)
	}
	return out, nil
}

// SELinuxEnforcing reports whether the host's SELinux is in enforcing mode,
// reading enforcePath (normally /sys/fs/selinux/enforce).
func SELinuxEnforcing(enforcePath string) bool {
	b, err := os.ReadFile(enforcePath)
	return err == nil && strings.TrimSpace(string(b)) == "1"
}

// ResolveSELinux turns the configured setting into a concrete one: an
// explicit value stands, "" (auto) disables label separation only when
// SELinux is enforcing, because the alternative (:Z) permanently relabels
// the host's project directory.
func ResolveSELinux(setting string, enforcing bool) string {
	if setting != "" {
		return setting
	}
	if enforcing {
		return "disable"
	}
	return "none"
}

func absPath(cwd, p string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(cwd, p)
}

func commonAncestor(a, b string) string {
	as := strings.Split(filepath.Clean(a), string(filepath.Separator))
	bs := strings.Split(filepath.Clean(b), string(filepath.Separator))
	var out []string
	for i := 0; i < len(as) && i < len(bs) && as[i] == bs[i]; i++ {
		out = append(out, as[i])
	}
	if len(out) <= 1 {
		return string(filepath.Separator)
	}
	return strings.Join(out, string(filepath.Separator))
}

// mapper turns host paths into container paths: inside the mount root by
// relative position, outside it through a dedicated read-only mount.
type mapper struct {
	cwd, root string
	exists    func(string) bool
	plan      *Plan
	ext       map[string]string
}

func (m *mapper) within(abs string) (string, bool) {
	rel, err := filepath.Rel(m.root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}

func (m *mapper) inside(abs string) string {
	rel, _ := m.within(abs)
	if rel == "." {
		return ContainerRoot
	}
	return path.Join(ContainerRoot, filepath.ToSlash(rel))
}

// translate maps a host path the user typed. A path outside the root that
// doesn't exist is left alone (and warned about): mounting it would make
// the engine fail before ansible could report the real problem.
func (m *mapper) translate(p string) string {
	if strings.HasPrefix(p, "~") {
		m.plan.Warnings = append(m.plan.Warnings, fmt.Sprintf("%s starts with ~ and can't be mapped; passed through unchanged", p))
		return p
	}
	abs := absPath(m.cwd, p)
	if _, ok := m.within(abs); ok {
		return m.inside(abs)
	}
	if !m.exists(abs) {
		m.plan.Warnings = append(m.plan.Warnings, fmt.Sprintf("%s does not exist; passed through unchanged", p))
		return p
	}
	if c, ok := m.ext[abs]; ok {
		return c
	}
	if m.ext == nil {
		m.ext = map[string]string{}
	}
	c := fmt.Sprintf("%s/%d/%s", containerExt, len(m.ext)+1, filepath.Base(abs))
	m.ext[abs] = c
	m.plan.Mounts = append(m.plan.Mounts, Mount{Host: abs, Container: c, ReadOnly: true})
	return c
}

// translateExisting maps p only if it is an existing host path (a list
// entry that may instead be a hostname or pattern).
func (m *mapper) translateExisting(p string) string {
	if p == "" || strings.HasPrefix(p, "~") || !m.exists(absPath(m.cwd, p)) {
		return p
	}
	return m.translate(p)
}
