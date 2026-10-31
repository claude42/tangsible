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
		"--passwd-entry=$USERNAME:*:$UID:$GID:tangsible:/runner:/bin/sh",
		"-w", "/home/u/proj", "-v", "/home/u/proj:/home/u/proj",
		"img:1", "ansible-playbook", "site.yml"}
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
		if want := (Mount{Host: c.root, Container: c.root}); p.Mounts[0] != want {
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
		{"/etc/ans/outside.ini", "/etc/ans/outside.ini", true},
		{"/home/u/vault.txt", "/home/u/vault.txt", true},
		{"/home/u/key", "/home/u/key", true},
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
		"/home/u/.ssh:/runner/.ssh:ro",
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
	in.ExtraMounts = []string{"/data:/data", "/cfg:/etc/cfg:ro"}
	p := build(t, in)
	if !hasPair(p.Argv, "-v", "/data:/data") || !hasPair(p.Argv, "-v", "/cfg:/etc/cfg:ro") {
		t.Errorf("argv = %q", p.Argv)
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
