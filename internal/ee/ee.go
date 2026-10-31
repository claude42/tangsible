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
	"path/filepath"
	"strings"
)

// Fixed container-side layout for what tangsible itself adds. User files
// are never moved: the project and every other path an argument names are
// mounted at their own host path, so the paths ansible reports (task.path
// in events, saved run logs, playbook_dir, stub playbooks' absolute
// references) mean the same thing on both sides - see "Identity mounts" in
// design-docs/ExecutionEnvironments.md.
const (
	ContainerPlugin = "/runner/plugin"
	ContainerEvents = "/runner/events"
	ContainerHome   = "/runner"
	ContainerSock   = "/runner/ssh-agent.sock"
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
	Cwd string // host working directory, absolute
	// Command is the program to run in the container; "" means
	// ansible-playbook. Playbook is the host path handed to it first, ""
	// if there is none (e.g. --version, or ansible-doc).
	Command  string
	Playbook string
	Args     []string // passthrough args, host paths as the user typed them
	// Scratch are directories (stub playbooks, outputs) mounted read-write
	// at their own path; paths inside them need no further mapping.
	Scratch []string
	// PluginDir holds the callback plugin; EventDir is a writable scratch
	// directory holding the event FIFO named EventFile.
	PluginDir string
	EventDir  string
	EventFile string
	// SSHAuthSock, SSHDir and SSHKnownHosts are mounted when non-empty
	// (the last two read-only). SSHKnownHosts is a single file, only
	// the public host keys, and is left out when SSHDir covers it.
	SSHAuthSock   string
	SSHDir        string
	SSHKnownHosts string
	HostEnv       []string // os.Environ()
	ExtraEnv      []string // NAME=value set in the container (colors, json indent)
	Name          string   // container name, for a fallback kill
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

	// The root is where cwd and the playbook meet - unless the playbook is
	// a stub inside a scratch directory, which is mounted by itself and
	// must not pull the project root up towards /.
	root := filepath.Clean(in.Cwd)
	if in.Playbook != "" {
		pb := absPath(in.Cwd, in.Playbook)
		inScratch := false
		for _, d := range in.Scratch {
			if d = filepath.Clean(d); pb == d || strings.HasPrefix(pb, d+string(filepath.Separator)) {
				inScratch = true
			}
		}
		if !inScratch {
			root = commonAncestor(root, filepath.Dir(pb))
		}
	}
	if root == string(filepath.Separator) {
		return nil, fmt.Errorf("execution environment: %s and the working directory %s share no directory below /; run from a common parent", in.Playbook, in.Cwd)
	}
	if d := systemDir(root); d != "" {
		return nil, fmt.Errorf("execution environment: the project directory %s lies inside %s, which the image needs for itself; run from elsewhere", root, d)
	}

	p := &Plan{Engine: engine}
	m := &mapper{cwd: in.Cwd, root: root, exists: exists, plan: p}
	p.Mounts = append(p.Mounts, Mount{Host: root, Container: root, ReadOnly: in.ReadOnly})
	p.Workdir = filepath.Clean(in.Cwd)
	for _, d := range in.Scratch {
		d = filepath.Clean(d)
		m.scratch = append(m.scratch, d)
		p.Mounts = append(p.Mounts, Mount{Host: d, Container: d})
	}

	if in.PluginDir != "" {
		p.Mounts = append(p.Mounts, Mount{Host: in.PluginDir, Container: ContainerPlugin, ReadOnly: true})
	}
	if in.EventDir != "" {
		p.Mounts = append(p.Mounts, Mount{Host: in.EventDir, Container: ContainerEvents})
	}
	if in.SSHDir != "" {
		p.Mounts = append(p.Mounts, Mount{Host: in.SSHDir, Container: ContainerHome + "/.ssh", ReadOnly: true})
	}
	if in.SSHKnownHosts != "" && in.SSHDir == "" {
		p.Mounts = append(p.Mounts, Mount{Host: in.SSHKnownHosts, Container: ContainerHome + "/.ssh/known_hosts", ReadOnly: true})
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

	var fixed []string
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
	// keep-id alone makes podman put the working directory (the project!)
	// in as the user's home, overriding any -e HOME: ansible would then
	// drop ~/.ansible/tmp into the project and ssh would look for
	// ~/.ssh there instead of at the mount. --passwd-entry fixes the home.
	argv = append(argv, "--userns=keep-id",
		"--passwd-entry=$USERNAME:*:$UID:$GID:tangsible:"+ContainerHome+":/bin/sh")
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
	command := in.Command
	if command == "" {
		command = "ansible-playbook"
	}
	argv = append(argv, in.Image, command)
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

// ShortImageName shortens an image reference for display: the registry and
// path go ("ghcr.io/ansible-community/community-ee-base:latest" becomes
// "community-ee-base:latest"), and a digest is cut to its first 12
// characters.
func ShortImageName(image string) string {
	if i := strings.LastIndex(image, "/"); i >= 0 {
		image = image[i+1:]
	}
	if i := strings.Index(image, "@"); i >= 0 {
		digest := image[i+1:]
		if j := strings.Index(digest, ":"); j >= 0 && len(digest) > j+13 {
			digest = digest[:j+13]
		}
		image = image[:i+1] + digest
	}
	return image
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

// systemDir returns the system directory path lies in or is, "" if none:
// mounting a project over one of these would replace what the image runs
// from.
func systemDir(path string) string {
	for _, d := range []string{"/usr", "/etc", "/bin", "/sbin", "/lib", "/lib64", "/proc", "/sys", "/dev", "/run", "/runner"} {
		if path == d || strings.HasPrefix(path, d+"/") {
			return d
		}
	}
	return ""
}

// mapper makes the paths an invocation names available in the container.
// Paths are never rewritten: one inside the project root or a scratch
// directory is already mounted, one outside gets its own read-only mount at
// its own path.
type mapper struct {
	cwd, root string
	scratch   []string
	exists    func(string) bool
	plan      *Plan
	mounted   map[string]bool
}

func (m *mapper) covered(abs string) bool {
	for _, d := range append([]string{m.root}, m.scratch...) {
		if abs == d || strings.HasPrefix(abs, d+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// translate ensures the host path p, as the user typed it, is reachable
// and returns it unchanged. A path outside the root that doesn't exist is
// left alone (and warned about): mounting it would make the engine fail
// before ansible could report the real problem.
func (m *mapper) translate(p string) string {
	if strings.HasPrefix(p, "~") {
		m.plan.Warnings = append(m.plan.Warnings, fmt.Sprintf("%s starts with ~ and can't be mapped; passed through unchanged", p))
		return p
	}
	abs := absPath(m.cwd, p)
	if m.covered(abs) || m.mounted[abs] {
		return p
	}
	if !m.exists(abs) {
		m.plan.Warnings = append(m.plan.Warnings, fmt.Sprintf("%s does not exist; passed through unchanged", p))
		return p
	}
	if m.mounted == nil {
		m.mounted = map[string]bool{}
	}
	m.mounted[abs] = true
	m.plan.Mounts = append(m.plan.Mounts, Mount{Host: abs, Container: abs, ReadOnly: true})
	return p
}

// translateExisting is translate for a list entry that may instead be a
// hostname or pattern: only an existing path is touched.
func (m *mapper) translateExisting(p string) string {
	if p == "" || strings.HasPrefix(p, "~") || !m.exists(absPath(m.cwd, p)) {
		return p
	}
	return m.translate(p)
}
