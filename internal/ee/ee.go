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
	"regexp"
	"slices"
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
	ContainerHome   = "/runner/home"
	ContainerSock   = "/runner/ssh-agent.sock"
	// ContainerSystemKnownHosts is where ssh looks for the system-wide list
	// of host keys, next to the user's own.
	ContainerSystemKnownHosts = "/etc/ssh/ssh_known_hosts"
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
	// SSHSystemKnownHosts is the host's /etc/ssh/ssh_known_hosts, mounted
	// read-only at the same place in the container: ssh reads it as well as
	// the user's file, so a host whose key is only recorded there verifies on
	// the host and must in the container.
	SSHSystemKnownHosts string
	HostEnv             []string // os.Environ()
	// HomeDir is the host user's home: a leading "~" in a path argument is
	// expanded to it (empty: not expanded, and warned about instead).
	HomeDir string
	// DNSSearch are search domains passed to the engine (--dns-search): see
	// DomainOnlySearch for the gap they close.
	DNSSearch []string
	ExtraEnv  []string // NAME=value set in the container (colors, json indent)
	// PassEnv and SetEnv are the user's pass_env / set_env: host variables to
	// pass (names, or prefixes ending in *) and fixed values to set.
	PassEnv []string
	SetEnv  map[string]string
	Name    string // container name, for a fallback kill
	// Exists reports whether a host path exists, IsDir whether it is a
	// directory; nil means os.Stat.
	Exists func(string) bool
	IsDir  func(string) bool
}

// Mount is one bind mount.
type Mount struct {
	Host, Container string
	ReadOnly        bool
	// Opts are further -v options a user asked for in extra_mounts (z, Z,
	// O); "ro" is ReadOnly.
	Opts []string
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
	isDir := in.IsDir
	if isDir == nil {
		isDir = func(p string) bool { fi, err := os.Stat(p); return err == nil && fi.IsDir() }
	}

	// The root is where cwd and the playbook meet - unless the playbook is
	// a stub inside a scratch directory, which is mounted by itself and
	// must not pull the project root up towards /.
	root := filepath.Clean(in.Cwd)
	hostEnv := envMap(in.HostEnv)
	in.Playbook = expandPath(in.Playbook, in.HomeDir, hostEnv)
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
	if w := broadRoot(root, in.HomeDir); w != "" {
		p.Warnings = append(p.Warnings, w)
	}
	m := &mapper{cwd: in.Cwd, root: root, exists: exists, isDir: isDir, plan: p, home: in.HomeDir, env: hostEnv}
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
	// A relabel mode would have :z/:Z rewrite the label of a system file
	// under /etc, which can upset the services that own it; the file is
	// public host keys only, so the cost of leaving it out is a host key
	// that doesn't verify, said so once.
	if in.SSHSystemKnownHosts != "" {
		if in.SELinux == "z" || in.SELinux == "Z" {
			p.Warnings = append(p.Warnings, `the system-wide `+ContainerSystemKnownHosts+` is not mounted: with selinux = "`+in.SELinux+`" it would be relabeled, and hosts recorded only there will fail host key verification (selinux = "disable" mounts it)`)
		} else {
			p.Mounts = append(p.Mounts, Mount{Host: in.SSHSystemKnownHosts, Container: ContainerSystemKnownHosts, ReadOnly: true})
		}
	}
	// A confined container cannot connect to the agent's socket whatever its
	// file label is (the socket belongs to another SELinux domain; checked
	// live: only label=disable works), so under the relabel modes mounting
	// it would be pointless - and :z/:Z would relabel the user's live agent
	// socket for nothing.
	sock := in.SSHAuthSock
	if sock != "" && (in.SELinux == "z" || in.SELinux == "Z") {
		p.Warnings = append(p.Warnings, `the SSH agent is not forwarded: with selinux = "`+in.SELinux+`" SELinux stops the container from reaching it (selinux = "disable" allows it)`)
		sock = ""
	}
	if sock != "" {
		p.Mounts = append(p.Mounts, Mount{Host: sock, Container: ContainerSock})
	}

	playbook := ""
	if in.Playbook != "" {
		playbook = m.translate(in.Playbook)
	}
	args := rewriteArgs(in.Args, m)

	// Extra mounts come last, and may not reuse a destination of ours: the
	// engine would refuse the run with a bare "duplicate mount destination".
	extra, err := parseExtraMounts(in.ExtraMounts, extraMountEnv{cwd: in.Cwd, home: in.HomeDir, env: hostEnv, exists: exists})
	if err != nil {
		return nil, err
	}
	for _, x := range extra {
		dup := false
		for _, have := range p.Mounts {
			if have.Container != x.Container {
				continue
			}
			if have.Host == x.Host {
				dup = true // the same mount twice (say known_hosts, which is also automatic): once is enough
				break
			}
			return nil, fmt.Errorf("extra_mounts: %s is mounted at %s, which tangsible already uses for %s", x.Host, x.Container, have.Host)
		}
		if !dup {
			p.Mounts = append(p.Mounts, x)
		}
	}

	fixed := []string{"HOME=" + ContainerHome}
	if in.PluginDir != "" {
		fixed = append(fixed, "ANSIBLE_CALLBACK_PLUGINS="+ContainerPlugin)
	}
	if in.EventDir != "" && in.EventFile != "" {
		fixed = append(fixed, "TANGSIBLE_EVENT_FILE="+ContainerEvents+"/"+in.EventFile)
	}
	if sock != "" {
		fixed = append(fixed, "SSH_AUTH_SOCK="+ContainerSock)
	}
	forwarded, dropped := forwardEnv(in.HostEnv, in.ExtraEnv, fixed, in.PassEnv, in.SetEnv)
	p.Env, p.Dropped = forwarded, dropped

	argv := []string{"run", "--rm", "-i"}
	if in.Name != "" {
		argv = append(argv, "--name", in.Name)
	}
	// The container user's home needs care. keep-id alone makes podman 5.4
	// put the working directory (the project!) in as the home - ansible
	// then drops ~/.ansible/tmp into the project - and podman 5.8 gives
	// HOME=/tmp; the image's own /runner is writable only through group
	// root, which keep-id gives the user on 5.4 but not on 5.8. So: a
	// world-writable tmpfs of our own as the home (the same on every
	// version and image), named in both the passwd entry and $HOME, and
	// where ~/.ssh entries are mounted.
	argv = append(argv, "--userns=keep-id",
		"--passwd-entry=$USERNAME:*:$UID:$GID:tangsible:"+ContainerHome+":/bin/sh",
		"--tmpfs", ContainerHome+":rw,mode=1777")
	if in.Network != "" {
		argv = append(argv, "--network="+in.Network)
	}
	for _, d := range in.DNSSearch {
		argv = append(argv, "--dns-search="+d)
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

// extraMountOptions are the -v options accepted in extra_mounts, the set
// both podman and docker document for a bind mount: read-only/read-write,
// SELinux relabel (shared z / private Z) and overlay.
var extraMountOptions = []string{"O", "Z", "ro", "rw", "z"}

// extraMountEnv is what resolving an extra_mounts entry needs from the host.
type extraMountEnv struct {
	cwd, home string
	env       map[string]string
	exists    func(string) bool
}

// parseExtraMount resolves one "SOURCE:DESTINATION[:OPTIONS]" entry: ~ and
// $VAR are expanded from the host (as for path options), a relative source is
// taken from the working directory - an engine would otherwise read it as a
// named volume - the destination must be absolute, the source must exist,
// OPTIONS is a comma-separated list of extraMountOptions, and contradictions
// (ro with rw, z with Z) are refused.
func parseExtraMount(spec string, e extraMountEnv) (Mount, error) {
	fail := func(format string, args ...any) (Mount, error) {
		return Mount{}, fmt.Errorf("extra_mounts entry %q: %s", spec, fmt.Sprintf(format, args...))
	}
	f := strings.Split(spec, ":")
	if len(f) < 2 || len(f) > 3 {
		return fail("want SOURCE:DESTINATION[:OPTIONS]")
	}
	src := expandPath(strings.TrimSpace(f[0]), e.home, e.env)
	dst := expandPath(strings.TrimSpace(f[1]), e.home, e.env)
	if src == "" {
		return fail("no source given")
	}
	if dst == "" {
		return fail("no destination given")
	}
	if strings.HasPrefix(src, "~") {
		return fail("cannot expand %q (only the current user's ~ is known)", src)
	}
	src = absPath(e.cwd, src)
	if !filepath.IsAbs(dst) {
		return fail("the destination %q must be an absolute path in the container", dst)
	}
	if !e.exists(src) {
		return fail("the source %s does not exist", src)
	}
	mt := Mount{Host: src, Container: filepath.Clean(dst)}
	if len(f) == 3 && strings.TrimSpace(f[2]) != "" {
		seen := map[string]bool{}
		for _, o := range strings.Split(f[2], ",") {
			o = strings.TrimSpace(o)
			if !slices.Contains(extraMountOptions, o) {
				return fail("unrecognized option %q (allowed: %s)", o, strings.Join(extraMountOptions, ", "))
			}
			if seen[o] {
				continue
			}
			seen[o] = true
			if o == "ro" {
				mt.ReadOnly = true
			} else if o != "rw" {
				mt.Opts = append(mt.Opts, o)
			}
		}
		if seen["ro"] && seen["rw"] {
			return fail("ro and rw contradict each other")
		}
		if seen["z"] && seen["Z"] {
			return fail("z and Z contradict each other")
		}
	}
	return mt, nil
}

func parseExtraMounts(specs []string, e extraMountEnv) ([]Mount, error) {
	var out []Mount
	byDest := map[string]string{}
	for _, spec := range specs {
		mt, err := parseExtraMount(spec, e)
		if err != nil {
			return nil, err
		}
		if prev, ok := byDest[mt.Container]; ok {
			if prev == mt.Host {
				continue // listed twice
			}
			return nil, fmt.Errorf("extra_mounts entry %q: %s is already mounted at %s from %s", spec, mt.Container, mt.Container, prev)
		}
		byDest[mt.Container] = mt.Host
		out = append(out, mt)
	}
	return out, nil
}

// CheckExtraMounts validates the extra_mounts of a configuration against the
// host, as Build will when it runs: for a startup check that reports a typo
// before an image is pulled or a TUI exists. exists nil means os.Stat.
func CheckExtraMounts(specs []string, cwd, home string, hostEnv []string, exists func(string) bool) error {
	if exists == nil {
		exists = func(p string) bool { _, err := os.Stat(p); return err == nil }
	}
	_, err := parseExtraMounts(specs, extraMountEnv{cwd: cwd, home: home, env: envMap(hostEnv), exists: exists})
	return err
}

func (mt Mount) spec(selinux string) string {
	opts := []string{}
	if mt.ReadOnly {
		opts = append(opts, "ro")
	}
	opts = append(opts, mt.Opts...)
	// The engine-wide relabel mode, unless the entry chose its own.
	if (selinux == "z" || selinux == "Z") && !slices.Contains(mt.Opts, "z") && !slices.Contains(mt.Opts, "Z") {
		opts = append(opts, selinux)
	}
	s := mt.Host + ":" + mt.Container
	if len(opts) > 0 {
		s += ":" + strings.Join(opts, ",")
	}
	return s
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

// domainRe is what a search domain may look like: a hostname's characters.
// resolv.conf is read from the host and the result ends up on a command
// line, so anything else (a flag-shaped value, whitespace) is ignored.
var domainRe = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9._-]*[A-Za-z0-9])?$`)

// DomainOnlySearch returns the search domain a host's resolv.conf names with
// a "domain" directive when podman would lose it. The engine builds the
// container's resolv.conf from the host's: it copies the nameserver and
// options lines and a "search" line, but not "domain" - so on such a host a
// short name (shunya) resolves outside the container and not inside it, with
// the default network or --network=host alike. A file with a "search" line
// is left to the engine, which copies it, and so is one without either
// directive; the result is nil in both cases. Of several "domain" lines the
// last one counts, as for glibc.
func DomainOnlySearch(resolvConf string) []string {
	domain := ""
	for _, line := range strings.Split(resolvConf, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || strings.HasPrefix(f[0], "#") || strings.HasPrefix(f[0], ";") {
			continue
		}
		switch f[0] {
		case "search":
			return nil
		case "domain":
			if len(f) >= 2 {
				domain = f[1]
			}
		}
	}
	if domain == "" || !domainRe.MatchString(domain) {
		return nil
	}
	return []string{domain}
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

// broadRoot describes why mounting root, read-write, hands the container far
// more than a project: it is the user's home directory (keys, tokens and
// shell history included) or a directory above it (a playbook in /home/a run
// from /home/b makes /home the root). "" for an ordinary project directory.
func broadRoot(root, home string) string {
	root = filepath.Clean(root)
	var what string
	switch {
	case home != "" && root == filepath.Clean(home):
		what = "your home directory"
	case home != "" && strings.HasPrefix(filepath.Clean(home), root+string(filepath.Separator)):
		what = root + ", which contains your home directory"
	case root == "/home" || root == "/root" || root == "/Users":
		what = root
	default:
		return ""
	}
	return "the project directory is " + what + ", and all of it is mounted into the container (including anything like ~/.ssh and tokens): run from the project's own directory instead" +
		" (or set read_only = true to at least stop writes)"
}

// mapper makes the paths an invocation names available in the container.
// Paths are never rewritten: one inside the project root or a scratch
// directory is already mounted, one outside gets its own read-only mount at
// its own path.
type mapper struct {
	cwd, root string
	scratch   []string
	exists    func(string) bool
	isDir     func(string) bool
	home      string
	env       map[string]string
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
	p = expandPath(p, m.home, m.env)
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
	p = expandPath(p, m.home, m.env)
	if p == "" || strings.HasPrefix(p, "~") || !m.exists(absPath(m.cwd, p)) {
		return p
	}
	return m.translate(p)
}

// inventoryCompanions are the directories Ansible loads inventory variables
// from, next to the inventory file.
var inventoryCompanions = []string{"group_vars", "host_vars"}

// translateInventory is translateExisting for one entry of -i: a path (or a
// host pattern, which is left alone). For an inventory *file* outside the
// mounted directories it also mounts the group_vars and host_vars
// directories beside it, read-only: Ansible reads them from the inventory
// file's own directory, and without them a container run silently sees
// undefined variables where a host run does not. Only those two directories,
// not the whole parent - it may be a home directory. An inventory directory
// is mounted whole by translateExisting already.
func (m *mapper) translateInventory(p string) string {
	out := m.translateExisting(p)
	if out == "" || strings.HasPrefix(out, "~") {
		return out
	}
	abs := absPath(m.cwd, out)
	if !m.exists(abs) || m.isDir(abs) {
		return out
	}
	for _, name := range inventoryCompanions {
		dir := filepath.Join(filepath.Dir(abs), name)
		if m.covered(dir) || m.mounted[dir] || !m.exists(dir) || !m.isDir(dir) {
			continue
		}
		if m.mounted == nil {
			m.mounted = map[string]bool{}
		}
		m.mounted[dir] = true
		m.plan.Mounts = append(m.plan.Mounts, Mount{Host: dir, Container: dir, ReadOnly: true})
	}
	return out
}

// envRef matches $NAME and ${NAME}.
var envRef = regexp.MustCompile(`\$(?:([A-Za-z_][A-Za-z0-9_]*)|\{([A-Za-z_][A-Za-z0-9_]*)\})`)

// envMap turns an os.Environ()-style list into a lookup table.
func envMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, kv := range env {
		if name, val, ok := strings.Cut(kv, "="); ok {
			m[name] = val
		}
	}
	return m
}

// expandPath expands what Ansible itself expands in a path option on the
// host - $NAME / ${NAME} from the host environment, then a leading "~" to
// home - before the path is handed to a container, where "~" would mean the
// container's own home and the variables the container's own environment.
// An undefined variable is left as written, as os.path.expandvars does;
// "~user" is not expanded; an empty home leaves "~" alone.
func expandPath(p, home string, env map[string]string) string {
	if strings.Contains(p, "$") {
		p = envRef.ReplaceAllStringFunc(p, func(ref string) string {
			sub := envRef.FindStringSubmatch(ref)
			name := sub[1]
			if name == "" {
				name = sub[2]
			}
			if v, ok := env[name]; ok {
				return v
			}
			return ref
		})
	}
	if home != "" && (p == "~" || strings.HasPrefix(p, "~/")) {
		p = home + p[1:]
	}
	return p
}
