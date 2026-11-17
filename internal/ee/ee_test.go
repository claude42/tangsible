package ee

import (
	"reflect"
	"strings"
	"testing"
)

func existsIn(paths ...string) func(string) bool {
	set := map[string]bool{}
	for _, p := range paths {
		set[p] = true
	}
	return func(p string) bool { return set[p] }
}

func base() Inputs {
	return Inputs{
		Config: Config{Image: "img:1"},
		Cwd:    "/home/u/proj",
		Exists: existsIn(),
	}
}

func build(t *testing.T, in Inputs) *Plan {
	t.Helper()
	p, err := Build(in)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return p
}

func hasPair(argv []string, flag, val string) bool {
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == flag && argv[i+1] == val {
			return true
		}
	}
	return false
}

func tail(p *Plan) []string {
	for i, a := range p.Argv {
		if a == "ansible-playbook" {
			return p.Argv[i:]
		}
	}
	return nil
}

func TestBuildMinimal(t *testing.T) {
	in := base()
	in.Playbook = "site.yml"
	in.Name = "tgs-1"
	p := build(t, in)
	want := []string{"run", "--rm", "-i", "--name", "tgs-1", "--userns=keep-id",
		"--passwd-entry=$USERNAME:*:$UID:$GID:tangsible:/runner/home:/bin/sh",
		"--tmpfs", "/runner/home:rw,mode=1777",
		"-w", "/home/u/proj", "-v", "/home/u/proj:/home/u/proj",
		"-e", "HOME=/runner/home", "img:1", "ansible-playbook", "site.yml"}
	if !reflect.DeepEqual(p.Argv, want) {
		t.Errorf("argv = %q\nwant   %q", p.Argv, want)
	}
}

func TestBuildErrors(t *testing.T) {
	cases := map[string]func(*Inputs){
		"engine":     func(in *Inputs) { in.Engine = "docker" },
		"no image":   func(in *Inputs) { in.Image = "" },
		"selinux":    func(in *Inputs) { in.SELinux = "maybe" },
		"relative":   func(in *Inputs) { in.Cwd = "proj" },
		"mount":      func(in *Inputs) { in.ExtraMounts = []string{"nocolon"} },
		"mount ro":   func(in *Inputs) { in.ExtraMounts = []string{"/a:/b:zz"} },
		"system dir": func(in *Inputs) { in.Cwd = "/usr/share/proj" },
		"no common dir": func(in *Inputs) {
			in.Cwd = "/home/u/proj"
			in.Playbook = "/srv/site.yml"
		},
	}
	for name, mod := range cases {
		in := base()
		mod(&in)
		if _, err := Build(in); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestMountRoot(t *testing.T) {
	cases := []struct {
		name, cwd, playbook, root string
	}{
		{"playbook in cwd", "/h/p", "site.yml", "/h/p"},
		{"playbook in subdir", "/h/p", "pb/site.yml", "/h/p"},
		{"playbook above cwd", "/h/p/sub", "../site.yml", "/h/p"},
		{"sibling dir", "/h/p/a", "../b/site.yml", "/h/p"},
		{"absolute playbook", "/h/p", "/h/p/x/site.yml", "/h/p"},
		{"playbook above, absolute", "/h/p/a/b", "/h/q/site.yml", "/h"},
	}
	for _, c := range cases {
		in := base()
		in.Cwd, in.Playbook = c.cwd, c.playbook
		p := build(t, in)
		// The root is mounted at its own path, and the working directory
		// and the playbook argument stay exactly as given.
		if want := (Mount{Host: c.root, Container: c.root}); !reflect.DeepEqual(p.Mounts[0], want) {
			t.Errorf("%s: root mount = %+v, want %+v", c.name, p.Mounts[0], want)
		}
		if p.Workdir != c.cwd {
			t.Errorf("%s: workdir = %s, want %s", c.name, p.Workdir, c.cwd)
		}
		if got := tail(p)[1]; got != c.playbook {
			t.Errorf("%s: playbook = %s, want %s", c.name, got, c.playbook)
		}
	}
}

func TestCommandAndScratch(t *testing.T) {
	in := base()
	in.Command = "ansible-doc"
	in.Args = []string{"ansible.builtin.copy"}
	p := build(t, in)
	if got := tail(p); got != nil {
		t.Errorf("found ansible-playbook in %q", p.Argv)
	}
	if n := len(p.Argv); p.Argv[n-2] != "ansible-doc" || p.Argv[n-1] != "ansible.builtin.copy" {
		t.Errorf("argv tail = %q", p.Argv[len(p.Argv)-3:])
	}

	// A stub playbook in a scratch dir needs no further mount; the dir is
	// mounted read-write at its own path.
	in = base()
	in.Scratch = []string{"/tmp/tangsible-resolve-1"}
	in.Playbook = "/tmp/tangsible-resolve-1/stub.yml"
	in.Args = []string{"--extra-vars", "@/tmp/tangsible-resolve-1/vars.json"}
	in.Exists = existsIn("/tmp/tangsible-resolve-1/stub.yml", "/tmp/tangsible-resolve-1/vars.json")
	p = build(t, in)
	if !hasPair(p.Argv, "-v", "/tmp/tangsible-resolve-1:/tmp/tangsible-resolve-1") {
		t.Errorf("scratch not mounted rw: %q", p.Argv)
	}
	mounts := 0
	for _, mt := range p.Mounts {
		if strings.HasPrefix(mt.Host, "/tmp/") {
			mounts++
		}
	}
	if mounts != 1 || len(p.Warnings) != 0 {
		t.Errorf("mounts = %v, warnings = %q", p.Mounts, p.Warnings)
	}
}

func TestNoPlaybook(t *testing.T) {
	in := base()
	in.Args = []string{"--version"}
	p := build(t, in)
	if got := tail(p); !reflect.DeepEqual(got, []string{"ansible-playbook", "--version"}) {
		t.Errorf("tail = %q", got)
	}
}

func TestReadOnlyProject(t *testing.T) {
	in := base()
	in.ReadOnly = true
	p := build(t, in)
	if !hasPair(p.Argv, "-v", "/home/u/proj:/home/u/proj:ro") {
		t.Errorf("argv = %q", p.Argv)
	}
}

func TestArgMounting(t *testing.T) {
	in := base()
	in.Playbook = "site.yml"
	in.Exists = existsIn(
		"/home/u/proj/inv/hosts", "/etc/ans/outside.ini", "/home/u/vault.txt",
		"/home/u/proj/vars.yml", "/home/u/proj/lib", "/home/u/key",
	)
	in.Args = []string{
		"-i", "inv/hosts,localhost,/etc/ans/outside.ini",
		"--inventory=localhost,",
		"-e", "@vars.yml", "-e", "k=v", "-e@vars.yml", "--extra-vars=@/home/u/vault.txt",
		"--vault-password-file", "/home/u/vault.txt",
		"--private-key=/home/u/key",
		"-M", "lib:/nonexistent",
		"--limit", "@vars.yml", "--limit", "web",
		"--vault-id", "dev@/home/u/vault.txt", "--vault-id", "@prompt",
		"-t", "tag", "-vvv", "--check",
	}
	p := build(t, in)
	// Paths are never rewritten...
	if got := tail(p)[2:]; !reflect.DeepEqual(got, in.Args) {
		t.Errorf("args changed:\n%q\nwant\n%q", got, in.Args)
	}
	// ...only the ones outside the project root are mounted, read-only, at
	// their own path, in order of first use and once each.
	var ext []Mount
	for _, mt := range p.Mounts[1:] {
		ext = append(ext, mt)
	}
	wantExt := []Mount{
		{Host: "/etc/ans/outside.ini", Container: "/etc/ans/outside.ini", ReadOnly: true},
		{Host: "/home/u/vault.txt", Container: "/home/u/vault.txt", ReadOnly: true},
		{Host: "/home/u/key", Container: "/home/u/key", ReadOnly: true},
	}
	if !reflect.DeepEqual(ext, wantExt) {
		t.Errorf("ext mounts = %v, want %v", ext, wantExt)
	}
	if len(p.Warnings) != 1 || !strings.Contains(p.Warnings[0], "/nonexistent") {
		// -M lib:/nonexistent: the missing entry is a plain list entry,
		// left alone silently; nothing else may warn.
		if len(p.Warnings) != 0 {
			t.Errorf("warnings = %q", p.Warnings)
		}
	}
}

func TestArgRewritingEdgeCases(t *testing.T) {
	in := base()
	in.Exists = existsIn()
	in.Args = []string{
		"--private-key", "/missing/key", // outside, absent: warning
		"--vault-password-file=~/pw", // tilde: unchanged + warning
		"/abs/looks/like/path",       // stray absolute arg: warning
		"--tags=/odd",                // unknown flag with path-ish value: warning
		"-i",                         // dangling flag: kept
	}
	p := build(t, in)
	if got := tail(p)[1:]; !reflect.DeepEqual(got, in.Args) {
		t.Errorf("args changed: %q", got)
	}
	if len(p.Warnings) != 4 {
		t.Errorf("warnings = %q, want 4", p.Warnings)
	}
}

func TestDoubleDashStopsRewriting(t *testing.T) {
	in := base()
	in.Exists = existsIn("/x/key")
	in.Args = []string{"--", "--private-key", "/x/key"}
	p := build(t, in)
	if got := tail(p)[1:]; !reflect.DeepEqual(got, in.Args) {
		t.Errorf("args = %q", got)
	}
}

func TestOutsideMountDeduplicated(t *testing.T) {
	in := base()
	in.Exists = existsIn("/s/pw")
	in.Args = []string{"--vault-password-file", "/s/pw", "--become-password-file", "/s/pw"}
	p := build(t, in)
	n := 0
	for _, mt := range p.Mounts {
		if mt.Host == "/s/pw" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("mounted %d times", n)
	}
}

func TestPluginEventsAndSSH(t *testing.T) {
	in := base()
	in.PluginDir = "/cache/plugin-abc"
	in.EventDir = "/tmp/tgs-ev"
	in.EventFile = "events"
	in.SSHDir = "/home/u/.ssh"
	in.SSHAuthSock = "/tmp/ssh-x/agent.1"
	p := build(t, in)
	for _, v := range []string{
		"/cache/plugin-abc:/runner/plugin:ro",
		"/tmp/tgs-ev:/runner/events",
		"/home/u/.ssh:/runner/home/.ssh:ro",
		"/tmp/ssh-x/agent.1:/runner/ssh-agent.sock",
	} {
		if !hasPair(p.Argv, "-v", v) {
			t.Errorf("missing mount %s in %q", v, p.Argv)
		}
	}
	for _, e := range []string{
		"ANSIBLE_CALLBACK_PLUGINS=/runner/plugin",
		"TANGSIBLE_EVENT_FILE=/runner/events/events",
		"SSH_AUTH_SOCK=/runner/ssh-agent.sock",
	} {
		if !hasPair(p.Argv, "-e", e) {
			t.Errorf("missing env %s in %q", e, p.Argv)
		}
	}
}

func TestNetworkAndSELinux(t *testing.T) {
	cases := []struct {
		selinux  string
		wantOpt  bool
		rootSpec string
	}{
		{"", false, "/home/u/proj:/home/u/proj"},
		{"none", false, "/home/u/proj:/home/u/proj"},
		{"disable", true, "/home/u/proj:/home/u/proj"},
		{"z", false, "/home/u/proj:/home/u/proj:z"},
		{"Z", false, "/home/u/proj:/home/u/proj:Z"},
	}
	for _, c := range cases {
		in := base()
		in.Network = "host"
		in.SELinux = c.selinux
		p := build(t, in)
		if !strings.Contains(strings.Join(p.Argv, " "), "--network=host") {
			t.Errorf("%q: no --network=host", c.selinux)
		}
		if got := hasPair(p.Argv, "--security-opt", "label=disable"); got != c.wantOpt {
			t.Errorf("%q: label=disable = %v", c.selinux, got)
		}
		if !hasPair(p.Argv, "-v", c.rootSpec) {
			t.Errorf("%q: argv %q lacks %s", c.selinux, p.Argv, c.rootSpec)
		}
	}
	in := base()
	in.SELinux = "Z"
	in.ReadOnly = true
	if !hasPair(build(t, in).Argv, "-v", "/home/u/proj:/home/u/proj:ro,Z") {
		t.Error("ro,Z combination missing")
	}
}

func TestExtraMounts(t *testing.T) {
	in := base()
	in.Exists = existsIn("/data", "/cfg")
	in.ExtraMounts = []string{"/data:/data", "/cfg:/etc/cfg:ro"}
	p := build(t, in)
	if !hasPair(p.Argv, "-v", "/data:/data") || !hasPair(p.Argv, "-v", "/cfg:/etc/cfg:ro") {
		t.Errorf("argv = %q", p.Argv)
	}
}

func extraMountCase(t *testing.T, specs ...string) (*Plan, error) {
	t.Helper()
	in := base() // cwd /home/u/proj
	in.HomeDir = "/home/u"
	in.HostEnv = []string{"HOME=/home/u", "DATA=/srv/data"}
	in.Exists = existsIn("/data", "/cfg", "/srv/data", "/home/u/.aws", "/home/u/proj/local", "/etc/ssl/certs")
	in.ExtraMounts = specs
	return Build(in)
}

func TestExtraMountOptionsAndForms(t *testing.T) {
	cases := []struct {
		spec string
		want string // the -v value
	}{
		{"/data:/data", "/data:/data"},
		{"/data:/data:ro", "/data:/data:ro"},
		{"/data:/data:rw", "/data:/data"},
		{"/data:/data:ro,z", "/data:/data:ro,z"},
		{"/data:/data:z,ro", "/data:/data:ro,z"},
		{"/data:/data:Z", "/data:/data:Z"},
		{"/data:/data:O", "/data:/data:O"},
		{"/data:/data:ro,ro", "/data:/data:ro"},
		{" /data : /data : ro ", "/data:/data:ro"},
		{"/data:/data/", "/data:/data"},
		{"~/.aws:/runner/home/.aws:ro", "/home/u/.aws:/runner/home/.aws:ro"},
		{"$DATA:/data2", "/srv/data:/data2"},
		{"${DATA}:$DATA", "/srv/data:/srv/data"},
		{"local:/work", "/home/u/proj/local:/work"}, // relative source: from the working directory, not a named volume
		{"/data:~/mnt", "/data:/home/u/mnt"},
	}
	for _, c := range cases {
		p, err := extraMountCase(t, c.spec)
		if err != nil {
			t.Errorf("%q: %v", c.spec, err)
			continue
		}
		if !hasPair(p.Argv, "-v", c.want) {
			t.Errorf("%q: want -v %s in %q", c.spec, c.want, p.Argv)
		}
	}
}

func TestExtraMountErrors(t *testing.T) {
	cases := []struct {
		spec, want string
	}{
		{"/data", "want SOURCE:DESTINATION"},
		{"/data:/a:ro:extra", "want SOURCE:DESTINATION"},
		{":/a", "no source"},
		{"/data:", "no destination"},
		{"/missing:/a", "source /missing does not exist"},
		{"relative-missing:/a", "does not exist"},
		{"/data:relative", "must be an absolute path"},
		{"/data:/a:bogus", `unrecognized option "bogus" (allowed: O, Z, ro, rw, z)`},
		{"/data:/a:ro,rw", "ro and rw contradict"},
		{"/data:/a:z,Z", "z and Z contradict"},
		{"~other/x:/a", "cannot expand"},
		{"$UNDEFINED/x:/a", "does not exist"}, // left as written, so it is not a path that exists
	}
	for _, c := range cases {
		_, err := extraMountCase(t, c.spec)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: err = %v, want it to contain %q", c.spec, err, c.want)
		} else if !strings.Contains(err.Error(), "extra_mounts entry") {
			t.Errorf("%q: the error should name the entry: %v", c.spec, err)
		}
	}
}

func TestExtraMountDestinations(t *testing.T) {
	// Two entries onto one destination: different sources are an error, the
	// same mount twice is just one.
	if _, err := extraMountCase(t, "/data:/x", "/cfg:/x"); err == nil || !strings.Contains(err.Error(), "already mounted at /x") {
		t.Errorf("same destination, different sources: %v", err)
	}
	p, err := extraMountCase(t, "/data:/x", "/data:/x")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, mt := range p.Mounts {
		if mt.Container == "/x" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("a repeated entry produced %d mounts", n)
	}

	// Onto a destination tangsible itself uses: refused with a clear message...
	in := base()
	in.Exists = existsIn("/data")
	in.PluginDir = "/cache/plugin"
	in.ExtraMounts = []string{"/data:/runner/plugin"}
	if _, err := Build(in); err == nil || !strings.Contains(err.Error(), "/runner/plugin, which tangsible already uses for /cache/plugin") {
		t.Errorf("collision with the plugin mount: %v", err)
	}
	in.ExtraMounts = []string{"/data:/home/u/proj"} // the project root's own path
	if _, err := Build(in); err == nil || !strings.Contains(err.Error(), "already uses") {
		t.Errorf("collision with the project mount: %v", err)
	}
	// ...but the very same mount as an automatic one (known_hosts, which is
	// automatic now and was once configured by hand) is quietly one mount.
	in = base()
	in.SSHKnownHosts = "/home/u/.ssh/known_hosts"
	in.Exists = existsIn("/home/u/.ssh/known_hosts")
	in.ExtraMounts = []string{"/home/u/.ssh/known_hosts:/runner/home/.ssh/known_hosts"}
	p = build(t, in)
	n = 0
	for _, mt := range p.Mounts {
		if mt.Container == "/runner/home/.ssh/known_hosts" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("duplicate of an automatic mount: %d mounts", n)
	}
}

// An entry's own z/Z beats the engine-wide relabel mode.
func TestExtraMountRelabelOptionVersusSELinuxMode(t *testing.T) {
	in := base()
	in.Exists = existsIn("/data", "/cfg")
	in.SELinux = "Z"
	in.ExtraMounts = []string{"/data:/d", "/cfg:/c:ro,z"}
	p := build(t, in)
	if !hasPair(p.Argv, "-v", "/data:/d:Z") {
		t.Errorf("a plain entry gets the mode: %q", p.Argv)
	}
	if !hasPair(p.Argv, "-v", "/cfg:/c:ro,z") {
		t.Errorf("an entry with its own z keeps it, without a second label: %q", p.Argv)
	}
}

func TestCheckExtraMounts(t *testing.T) {
	dir := t.TempDir()
	if err := CheckExtraMounts([]string{dir + ":/x:ro"}, "/", "/home/u", nil, nil); err != nil {
		t.Errorf("an existing source (real os.Stat): %v", err)
	}
	if err := CheckExtraMounts([]string{dir + "/nope:/x"}, "/", "/home/u", nil, nil); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("a missing source: %v", err)
	}
	if err := CheckExtraMounts(nil, "/", "", nil, nil); err != nil {
		t.Errorf("nothing configured: %v", err)
	}
}

func TestEnvForwarding(t *testing.T) {
	in := base()
	in.HostEnv = []string{
		"ANSIBLE_HOST_KEY_CHECKING=False",
		"ANSIBLE_CONFIG=/home/u/ansible.cfg",
		"ANSIBLE_ROLES_PATH=/home/u/roles",
		"ANSIBLE_CALLBACK_PLUGINS=/host/plugins",
		"ANSIBLE_INVENTORY=/home/u/inv",
		"ANSIBLE_INVENTORY_UNPARSED_WARNING=false",
		"ANSIBLE_FORCE_COLOR=0",
		"HTTPS_PROXY=http://proxy:3128",
		"NO_COLOR=1",
		"HOME=/home/u",
		"SECRET=nope",
	}
	in.ExtraEnv = []string{"ANSIBLE_FORCE_COLOR=1", "ANSIBLE_JSON_INDENT=0"}
	in.PluginDir = "/cache/p"
	p := build(t, in)
	wantEnv := []string{
		"ANSIBLE_FORCE_COLOR=1", "ANSIBLE_HOST_KEY_CHECKING=False",
		"ANSIBLE_INVENTORY_UNPARSED_WARNING=false", "ANSIBLE_JSON_INDENT=0",
		"HTTPS_PROXY=http://proxy:3128", "NO_COLOR=1",
	}
	if !reflect.DeepEqual(p.Env, wantEnv) {
		t.Errorf("env = %q\nwant  %q", p.Env, wantEnv)
	}
	wantDropped := []string{"ANSIBLE_CONFIG", "ANSIBLE_INVENTORY", "ANSIBLE_ROLES_PATH"}
	if !reflect.DeepEqual(p.Dropped, wantDropped) {
		t.Errorf("dropped = %q, want %q", p.Dropped, wantDropped)
	}
	// Values must never reach the command line: only bare names.
	joined := strings.Join(p.Argv, "\x00")
	if strings.Contains(joined, "proxy:3128") || strings.Contains(joined, "False") {
		t.Errorf("a forwarded value leaked into argv: %q", p.Argv)
	}
	if !hasPair(p.Argv, "-e", "HTTPS_PROXY") {
		t.Errorf("no bare -e HTTPS_PROXY in %q", p.Argv)
	}
	// The fixed plugin path wins over the host's dropped one.
	if !hasPair(p.Argv, "-e", "ANSIBLE_CALLBACK_PLUGINS=/runner/plugin") {
		t.Errorf("plugin env missing: %q", p.Argv)
	}
}

func TestResolveSELinux(t *testing.T) {
	cases := []struct {
		setting   string
		enforcing bool
		want      string
	}{
		{"", true, "disable"}, {"", false, "none"},
		{"Z", false, "Z"}, {"none", true, "none"},
	}
	for _, c := range cases {
		if got := ResolveSELinux(c.setting, c.enforcing); got != c.want {
			t.Errorf("ResolveSELinux(%q,%v) = %q, want %q", c.setting, c.enforcing, got, c.want)
		}
	}
}

func TestShortImageName(t *testing.T) {
	for in, want := range map[string]string{
		"ghcr.io/ansible-community/community-ee-base:latest":  "community-ee-base:latest",
		"quay.io/ansible/ee-minimal":                          "ee-minimal",
		"localhost:5000/team/ee:2":                            "ee:2",
		"ee:1":                                                "ee:1",
		"reg.io/a/ee@sha256:0123456789abcdef0123456789abcdef": "ee@sha256:0123456789ab",
	} {
		if got := ShortImageName(in); got != want {
			t.Errorf("ShortImageName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSSHKnownHostsMount(t *testing.T) {
	in := base()
	in.SSHKnownHosts = "/home/u/.ssh/known_hosts"
	p := build(t, in)
	if !hasPair(p.Argv, "-v", "/home/u/.ssh/known_hosts:/runner/home/.ssh/known_hosts:ro") {
		t.Errorf("known_hosts not mounted read-only: %q", p.Argv)
	}
	// The whole directory already carries it: no nested second mount.
	in.SSHDir = "/home/u/.ssh"
	p = build(t, in)
	for _, mt := range p.Mounts {
		if mt.Container == "/runner/home/.ssh/known_hosts" {
			t.Errorf("known_hosts mounted on top of the ssh dir: %v", p.Mounts)
		}
	}
}

// Under the relabel modes the agent socket is neither mounted nor relabeled:
// a confined container can't reach it anyway.
func TestSSHAgentSkippedUnderRelabelModes(t *testing.T) {
	for _, mode := range []string{"z", "Z"} {
		in := base()
		in.SELinux = mode
		in.SSHAuthSock = "/tmp/ssh-x/agent.1"
		p := build(t, in)
		if strings.Contains(strings.Join(p.Argv, " "), "agent.1") || strings.Contains(strings.Join(p.Argv, " "), "SSH_AUTH_SOCK") {
			t.Errorf("%s: agent socket still passed: %q", mode, p.Argv)
		}
		if len(p.Warnings) != 1 || !strings.Contains(p.Warnings[0], "SSH agent is not forwarded") {
			t.Errorf("%s: warnings = %q", mode, p.Warnings)
		}
	}
	for _, mode := range []string{"", "none", "disable"} {
		in := base()
		in.SELinux = mode
		in.SSHAuthSock = "/tmp/ssh-x/agent.1"
		p := build(t, in)
		if !hasPair(p.Argv, "-v", "/tmp/ssh-x/agent.1:/runner/ssh-agent.sock") || len(p.Warnings) != 0 {
			t.Errorf("%q: agent should be forwarded silently: %q %q", mode, p.Argv, p.Warnings)
		}
	}
}

func TestDomainOnlySearch(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"domain only (the dropped case)", "domain aw.net\nnameserver 127.0.0.1\n", []string{"aw.net"}},
		{"search is left to the engine", "search aw.net corp.example\nnameserver 10.0.0.1\n", nil},
		{"search wins when both exist", "domain aw.net\nsearch other.example\n", nil},
		{"search wins even when it comes first", "search other.example\ndomain aw.net\n", nil},
		{"neither directive", "nameserver 127.0.0.53\noptions edns0\n", nil},
		{"the last domain counts", "domain first.example\ndomain aw.net\n", []string{"aw.net"}},
		{"comments and blank lines", "# domain nope.example\n; domain nope2.example\n\n  domain  aw.net  \n", []string{"aw.net"}},
		{"tabs", "domain\taw.net\n", []string{"aw.net"}},
		{"domain without a value", "domain\nnameserver 1.1.1.1\n", nil},
		{"a flag-shaped value is ignored", "domain --privileged\n", nil},
		{"a value with a space-separated extra is cut to the first word", "domain aw.net extra\n", []string{"aw.net"}},
		{"empty file", "", nil},
	}
	for _, c := range cases {
		if got := DomainOnlySearch(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: DomainOnlySearch(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestDNSSearchFlags(t *testing.T) {
	in := base()
	if joined := strings.Join(build(t, in).Argv, " "); strings.Contains(joined, "--dns-search") {
		t.Errorf("no search domain given, yet: %q", joined)
	}
	in.DNSSearch = []string{"aw.net", "corp.example"}
	argv := build(t, in).Argv
	for _, want := range []string{"--dns-search=aw.net", "--dns-search=corp.example"} {
		found := false
		for _, a := range argv {
			if a == want {
				found = true
			}
		}
		if !found {
			t.Errorf("missing %s in %q", want, argv)
		}
	}
	// With the host network as well: the engine drops "domain" there too.
	in.Network = "host"
	if !strings.Contains(strings.Join(build(t, in).Argv, " "), "--network=host --dns-search=aw.net") {
		t.Errorf("host network: %q", build(t, in).Argv)
	}
}

func TestSystemKnownHostsMount(t *testing.T) {
	in := base()
	if joined := strings.Join(build(t, in).Argv, " "); strings.Contains(joined, "ssh_known_hosts") {
		t.Errorf("nothing given, yet: %q", joined)
	}
	in.SSHSystemKnownHosts = "/etc/ssh/ssh_known_hosts"
	if !hasPair(build(t, in).Argv, "-v", "/etc/ssh/ssh_known_hosts:/etc/ssh/ssh_known_hosts:ro") {
		t.Errorf("not mounted read-only at its own path: %q", build(t, in).Argv)
	}
	// It is a separate file from the user's: mounted whether or not the whole
	// ssh dir is, and alongside the user's known_hosts.
	in.SSHDir = "/home/u/.ssh"
	in.SSHKnownHosts = "/home/u/.ssh/known_hosts"
	if !hasPair(build(t, in).Argv, "-v", "/etc/ssh/ssh_known_hosts:/etc/ssh/ssh_known_hosts:ro") {
		t.Errorf("with ssh_dir: %q", build(t, in).Argv)
	}
}

// Under :z/:Z the engine would relabel a file in /etc, so it is left out
// (with a warning) instead.
func TestSystemKnownHostsSkippedUnderRelabelModes(t *testing.T) {
	for _, mode := range []string{"z", "Z"} {
		in := base()
		in.SELinux = mode
		in.SSHSystemKnownHosts = "/etc/ssh/ssh_known_hosts"
		p := build(t, in)
		if strings.Contains(strings.Join(p.Argv, " "), "/etc/ssh/ssh_known_hosts") {
			t.Errorf("%s: still mounted: %q", mode, p.Argv)
		}
		if len(p.Warnings) != 1 || !strings.Contains(p.Warnings[0], "system-wide /etc/ssh/ssh_known_hosts is not mounted") {
			t.Errorf("%s: warnings = %q", mode, p.Warnings)
		}
	}
	for _, mode := range []string{"", "none", "disable"} {
		in := base()
		in.SELinux = mode
		in.SSHSystemKnownHosts = "/etc/ssh/ssh_known_hosts"
		p := build(t, in)
		if !hasPair(p.Argv, "-v", "/etc/ssh/ssh_known_hosts:/etc/ssh/ssh_known_hosts:ro") || len(p.Warnings) != 0 {
			t.Errorf("%q: should be mounted silently: %q %q", mode, p.Argv, p.Warnings)
		}
	}
}

func dirsIn(paths ...string) func(string) bool { return existsIn(paths...) }

func mountHosts(p *Plan) []string {
	var hosts []string
	for _, mt := range p.Mounts[1:] { // [0] is the project root
		hosts = append(hosts, mt.Host)
	}
	return hosts
}

// Ansible reads group_vars/host_vars from the inventory file's directory, so
// an inventory file outside the project brings those two directories along -
// and nothing else from its directory.
func TestInventoryFileMountsItsGroupAndHostVars(t *testing.T) {
	in := base()
	in.Exists = existsIn("/srv/inv/hosts.ini", "/srv/inv/group_vars", "/srv/inv/host_vars", "/srv/inv/secrets")
	in.IsDir = dirsIn("/srv/inv/group_vars", "/srv/inv/host_vars", "/srv/inv/secrets")
	in.Args = []string{"-i", "/srv/inv/hosts.ini"}
	p := build(t, in)
	want := []string{"/srv/inv/hosts.ini", "/srv/inv/group_vars", "/srv/inv/host_vars"}
	if got := mountHosts(p); !reflect.DeepEqual(got, want) {
		t.Errorf("mounts = %q, want %q", got, want)
	}
	for _, mt := range p.Mounts[1:] {
		if !mt.ReadOnly || mt.Host != mt.Container {
			t.Errorf("%+v: want read-only at its own path", mt)
		}
	}
	for _, flag := range []string{"--inventory", "--inventory-file"} {
		in.Args = []string{flag, "/srv/inv/hosts.ini"}
		if got := mountHosts(build(t, in)); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: mounts = %q", flag, got)
		}
	}
	in.Args = []string{"--inventory=/srv/inv/hosts.ini"}
	if got := mountHosts(build(t, in)); !reflect.DeepEqual(got, want) {
		t.Errorf("--inventory=: mounts = %q", got)
	}
}

func TestInventoryCompanionsOnlyWhenTheyExist(t *testing.T) {
	in := base()
	in.Exists = existsIn("/srv/inv/hosts.ini", "/srv/inv/host_vars")
	in.IsDir = dirsIn("/srv/inv/host_vars")
	in.Args = []string{"-i", "/srv/inv/hosts.ini"}
	if got, want := mountHosts(build(t, in)), []string{"/srv/inv/hosts.ini", "/srv/inv/host_vars"}; !reflect.DeepEqual(got, want) {
		t.Errorf("mounts = %q, want %q", got, want)
	}
	// A file named group_vars is not the directory Ansible reads.
	in.Exists = existsIn("/srv/inv/hosts.ini", "/srv/inv/group_vars")
	in.IsDir = dirsIn()
	if got, want := mountHosts(build(t, in)), []string{"/srv/inv/hosts.ini"}; !reflect.DeepEqual(got, want) {
		t.Errorf("group_vars as a file: mounts = %q, want %q", got, want)
	}
}

// An inventory directory is mounted whole (companions included); one inside
// the project is already covered; a host list is not a path.
func TestInventoryCompanionsNotAddedWhereTheyAreAlreadyThere(t *testing.T) {
	in := base()
	in.Exists = existsIn("/srv/inv", "/srv/inv/group_vars", "/home/u/proj/inv/hosts", "/home/u/proj/inv/group_vars")
	in.IsDir = dirsIn("/srv/inv", "/srv/inv/group_vars", "/home/u/proj/inv/group_vars")
	in.Args = []string{"-i", "/srv/inv"}
	if got, want := mountHosts(build(t, in)), []string{"/srv/inv"}; !reflect.DeepEqual(got, want) {
		t.Errorf("inventory directory: mounts = %q, want %q", got, want)
	}
	in.Args = []string{"-i", "inv/hosts"}
	if got := mountHosts(build(t, in)); len(got) != 0 {
		t.Errorf("inventory inside the project: extra mounts %q", got)
	}
	in.Args = []string{"-i", "web1,web2,"}
	if got := mountHosts(build(t, in)); len(got) != 0 {
		t.Errorf("host list: extra mounts %q", got)
	}
}

func TestInventoryCompanionsListAndDedupe(t *testing.T) {
	in := base()
	in.Exists = existsIn("/srv/inv/a.ini", "/srv/inv/b.ini", "/srv/inv/group_vars", "/other/c.ini", "/other/host_vars")
	in.IsDir = dirsIn("/srv/inv/group_vars", "/other/host_vars")
	// One value with several entries, and a second -i: two files in the same
	// directory share their companions.
	in.Args = []string{"-i", "/srv/inv/a.ini,localhost,/other/c.ini", "-i", "/srv/inv/b.ini"}
	want := []string{"/srv/inv/a.ini", "/srv/inv/group_vars", "/other/c.ini", "/other/host_vars", "/srv/inv/b.ini"}
	if got := mountHosts(build(t, in)); !reflect.DeepEqual(got, want) {
		t.Errorf("mounts = %q, want %q", got, want)
	}
}

func TestInventoryTildeIsNotGuessed(t *testing.T) {
	in := base()
	in.Args = []string{"-i", "~/inv/hosts.ini"}
	p := build(t, in)
	if got := mountHosts(p); len(got) != 0 {
		t.Errorf("a ~ path produced mounts: %q", got)
	}
}

func TestExpandPath(t *testing.T) {
	env := map[string]string{"HOME": "/home/u", "DIR": "/srv/x", "REL": "rel"}
	cases := []struct {
		in, home, want string
	}{
		{"~/k", "/home/u", "/home/u/k"},
		{"~", "/home/u", "/home/u"},
		{"~/k", "", "~/k"}, // no home known: left for the caller to warn about
		{"~other/k", "/home/u", "~other/k"},
		{"a~/b", "/home/u", "a~/b"},
		{"$HOME/k", "", "/home/u/k"},
		{"${DIR}/k", "", "/srv/x/k"},
		{"$DIR/$REL", "", "/srv/x/rel"},
		{"$UNDEFINED/k", "", "$UNDEFINED/k"},
		{"${UNDEFINED}/k", "", "${UNDEFINED}/k"},
		{"/plain/path", "/home/u", "/plain/path"},
		{"", "/home/u", ""},
		{"$", "", "$"},
	}
	for _, c := range cases {
		if got := expandPath(c.in, c.home, env); got != c.want {
			t.Errorf("expandPath(%q, %q) = %q, want %q", c.in, c.home, got, c.want)
		}
	}
	// Variables first, then the tilde, as Ansible does: a variable whose value
	// starts with ~ is expanded too.
	if got := expandPath("$T/x", "/home/u", map[string]string{"T": "~/t"}); got != "/home/u/t/x" {
		t.Errorf("variable holding a ~ path: %q", got)
	}
}

// A ~ in a path option means the host user's home; inside the container it
// would be the container's, so it is expanded first and the result mounted.
func TestTildeAndVariablesInPathOptions(t *testing.T) {
	in := base()
	in.HomeDir = "/home/u"
	in.HostEnv = []string{"DATA=/srv/data", "HOME=/home/u"}
	in.Playbook = "site.yml"
	in.Exists = existsIn(
		"/home/u/pw", "/home/u/k", "/home/u/vars.yml", "/home/u/lib", "/home/u/lib2",
		"/home/u/vault-pw", "/home/u/inv/hosts.ini", "/home/u/inv/group_vars", "/srv/data/extra.yml",
	)
	in.IsDir = dirsIn("/home/u/inv/group_vars")
	in.Args = []string{
		"--vault-password-file=~/pw",
		"--private-key", "~/k",
		"-e", "@~/vars.yml",
		"-e@$DATA/extra.yml",
		"-M", "~/lib:${HOME}/lib2:/nonexistent",
		"--vault-id", "dev@~/vault-pw",
		"-i", "~/inv/hosts.ini",
		"-e", "k=~/not-a-file", // a plain variable assignment, not a path
		"-t", "~tag",
	}
	p := build(t, in)
	want := []string{
		"--vault-password-file=/home/u/pw",
		"--private-key", "/home/u/k",
		"-e", "@/home/u/vars.yml",
		"-e@/srv/data/extra.yml",
		"-M", "/home/u/lib:/home/u/lib2:/nonexistent",
		"--vault-id", "dev@/home/u/vault-pw",
		"-i", "/home/u/inv/hosts.ini",
		"-e", "k=~/not-a-file",
		"-t", "~tag",
	}
	if got := tail(p)[2:]; !reflect.DeepEqual(got, want) {
		t.Errorf("args =\n%q\nwant\n%q", got, want)
	}
	have := map[string]bool{}
	for _, mt := range p.Mounts {
		have[mt.Host] = mt.ReadOnly
	}
	for _, path := range []string{"/home/u/pw", "/home/u/k", "/home/u/vars.yml", "/srv/data/extra.yml", "/home/u/lib", "/home/u/lib2", "/home/u/vault-pw", "/home/u/inv/hosts.ini", "/home/u/inv/group_vars"} {
		if ro, ok := have[path]; !ok || !ro {
			t.Errorf("%s: not mounted read-only (mounts: %v)", path, p.Mounts)
		}
	}
	if len(p.Warnings) != 0 {
		t.Errorf("warnings = %q", p.Warnings)
	}
}

func TestTildeUserAndMissingHomeStayWarned(t *testing.T) {
	// ~other has no home we know of.
	in := base()
	in.HomeDir = "/home/u"
	in.Args = []string{"--private-key", "~other/k"}
	p := build(t, in)
	if got := tail(p)[1:]; !reflect.DeepEqual(got, []string{"--private-key", "~other/k"}) || len(p.Warnings) != 1 {
		t.Errorf("~user: args %q, warnings %q", got, p.Warnings)
	}
	// No home given at all (cannot happen from the runner, but the builder must not guess).
	in = base()
	in.Args = []string{"--private-key", "~/k"}
	p = build(t, in)
	if got := tail(p)[1:]; !reflect.DeepEqual(got, []string{"--private-key", "~/k"}) || len(p.Warnings) != 1 {
		t.Errorf("no home: args %q, warnings %q", got, p.Warnings)
	}
}

func TestTildeInPlaybookArgument(t *testing.T) {
	in := base() // cwd /home/u/proj
	in.HomeDir = "/home/u"
	in.Playbook = "~/proj/site.yml"
	p := build(t, in)
	if got := tail(p)[1]; got != "/home/u/proj/site.yml" {
		t.Errorf("playbook argument = %q", got)
	}
	if p.Mounts[0].Host != "/home/u/proj" {
		t.Errorf("project root = %q", p.Mounts[0].Host)
	}
}

// A path that doesn't exist after expansion is reported with the expanded
// (host) path, which is what the user can act on.
func TestExpandedPathThatDoesNotExistWarns(t *testing.T) {
	in := base()
	in.HomeDir = "/home/u"
	in.Args = []string{"--vault-password-file", "~/missing"}
	p := build(t, in)
	if got := tail(p)[1:]; !reflect.DeepEqual(got, []string{"--vault-password-file", "/home/u/missing"}) {
		t.Errorf("args = %q", got)
	}
	if len(p.Warnings) != 1 || !strings.Contains(p.Warnings[0], "/home/u/missing does not exist") {
		t.Errorf("warnings = %q", p.Warnings)
	}
}
