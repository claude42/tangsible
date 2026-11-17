package runner

import (
	"bufio"
	"bytes"
	"code.aw.net/claude/tangsible/internal/config"
	"code.aw.net/claude/tangsible/internal/ee"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

// fakeEngine writes a stand-in for podman that logs every call and answers
// "image exists" and "pull" by environment variables.
func fakeEngine(t *testing.T) (engine, logPath string) {
	t.Helper()
	dir := t.TempDir()
	logPath = filepath.Join(dir, "calls.log")
	engine = filepath.Join(dir, "fakepodman")
	script := "#!/bin/sh\n" +
		"echo \"$@\" >> \"$FAKE_LOG\"\n" +
		"if [ \"$1 $2\" = \"image exists\" ]; then\n" +
		"  [ -n \"$FAKE_EXISTS_ERR\" ] && echo \"$FAKE_EXISTS_ERR\" >&2\n" +
		"  exit ${FAKE_EXISTS:-1}\n" +
		"fi\n" +
		"if [ \"$1\" = pull ]; then echo progress-from-the-engine; exit ${FAKE_PULL:-0}; fi\n" +
		"exit 0\n"
	if err := os.WriteFile(engine, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_LOG", logPath)
	return engine, logPath
}

func resetEnsureImageState(t *testing.T, s *config.EESettings) {
	t.Helper()
	refreshedMu.Lock()
	refreshed = map[string]bool{}
	refreshedMu.Unlock()
	SetExecutionEnvironment(s)
	t.Cleanup(func() { SetExecutionEnvironment(nil) })
}

func calls(t *testing.T, logPath string) []string {
	t.Helper()
	b, _ := os.ReadFile(logPath)
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

func pulls(t *testing.T, logPath string) []string {
	var out []string
	for _, c := range calls(t, logPath) {
		if strings.HasPrefix(c, "pull") {
			out = append(out, c)
		}
	}
	return out
}

func TestEnsureImagePolicies(t *testing.T) {
	const img = "ghcr.io/x/ee:latest"
	cases := []struct {
		name, policy, image string
		exists              string // FAKE_EXISTS: 0 present, 1 absent
		pullExit            string
		wantPulls           int
		wantErr             string
		wantOut             string
	}{
		{"missing and absent: pull", "missing", img, "1", "0", 1, "", "pulling ghcr.io/x/ee:latest (not available locally)"},
		{"missing and present: nothing", "missing", img, "0", "0", 0, "", ""},
		{"always and present: refresh", "always", img, "0", "0", 1, "", "pull_policy = always"},
		{"always, refresh fails, local copy used", "always", img, "0", "125", 1, "", "using the local copy"},
		{"missing, absent, pull fails: error", "missing", img, "1", "125", 1, "couldn't pull ghcr.io/x/ee:latest", ""},
		{"never and absent: error, no pull", "never", img, "1", "0", 0, `pull_policy is "never"`, ""},
		{"never and present: nothing", "never", img, "0", "0", 0, "", ""},
		{"tag latest present: refresh", "tag", img, "0", "0", 1, "", `tag "latest"`},
		{"tag 1.0 present: nothing", "tag", "ghcr.io/x/ee:1.0", "0", "0", 0, "", ""},
		{"local-only image absent: error, no pull", "missing", "localhost/my-ee:1.0", "1", "0", 0, "images under localhost/ exist only locally", ""},
		{"local-only image present, always: nothing", "always", "localhost/my-ee:1.0", "0", "0", 0, "", ""},
	}
	for _, c := range cases {
		engine, log := fakeEngine(t)
		t.Setenv("FAKE_EXISTS", c.exists)
		t.Setenv("FAKE_PULL", c.pullExit)
		resetEnsureImageState(t, &config.EESettings{Image: c.image, Engine: engine, PullPolicy: c.policy})
		var out bytes.Buffer
		err := EnsureImage(&out)
		if c.wantErr == "" && err != nil {
			t.Errorf("%s: unexpected error %v", c.name, err)
		}
		if c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)) {
			t.Errorf("%s: err = %v, want it to contain %q", c.name, err, c.wantErr)
		}
		if got := len(pulls(t, log)); got != c.wantPulls {
			t.Errorf("%s: %d pull(s), want %d (calls: %q)", c.name, got, c.wantPulls, calls(t, log))
		}
		if !strings.Contains(out.String(), c.wantOut) {
			t.Errorf("%s: output %q lacks %q", c.name, out.String(), c.wantOut)
		}
	}
}

// A refreshing policy acts once per session: a re-run must not pull again.
func TestEnsureImageRefreshesOncePerSession(t *testing.T) {
	for _, policy := range []string{"always", "tag"} {
		engine, log := fakeEngine(t)
		t.Setenv("FAKE_EXISTS", "0")
		resetEnsureImageState(t, &config.EESettings{Image: "ghcr.io/x/ee:latest", Engine: engine, PullPolicy: policy})
		for i := 0; i < 3; i++ {
			if err := EnsureImage(io.Discard); err != nil {
				t.Fatal(err)
			}
		}
		if got := len(pulls(t, log)); got != 1 {
			t.Errorf("%s: %d pulls over three runs, want 1", policy, got)
		}
		// Another image is its own session-once.
		SetExecutionEnvironment(&config.EESettings{Image: "ghcr.io/x/other:latest", Engine: engine, PullPolicy: policy})
		if err := EnsureImage(io.Discard); err != nil {
			t.Fatal(err)
		}
		if got := len(pulls(t, log)); got != 2 {
			t.Errorf("%s: a second image: %d pulls in all, want 2", policy, got)
		}
	}
}

// An engine error is not "absent": pulling would only hide it.
func TestEnsureImageEngineErrorIsNotAbsent(t *testing.T) {
	engine, log := fakeEngine(t)
	t.Setenv("FAKE_EXISTS", "125")
	t.Setenv("FAKE_EXISTS_ERR", "Error: database is locked")
	resetEnsureImageState(t, &config.EESettings{Image: "ghcr.io/x/ee:1", Engine: engine, PullPolicy: "missing"})
	err := EnsureImage(io.Discard)
	if err == nil || !strings.Contains(err.Error(), "couldn't check whether the image ghcr.io/x/ee:1 is available: Error: database is locked") {
		t.Errorf("err = %v", err)
	}
	if len(pulls(t, log)) != 0 {
		t.Errorf("it pulled after an engine error: %q", calls(t, log))
	}
}

func TestEnsureImageEngineMissing(t *testing.T) {
	resetEnsureImageState(t, &config.EESettings{Image: "x", Engine: "tangsible-no-such-engine", PullPolicy: "missing"})
	if err := EnsureImage(io.Discard); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("err = %v", err)
	}
}

func TestEnsureImagePullArguments(t *testing.T) {
	engine, log := fakeEngine(t)
	t.Setenv("FAKE_EXISTS", "1")
	t.Setenv("AUTHDIR", "/srv/auth")
	resetEnsureImageState(t, &config.EESettings{
		Image: "ghcr.io/x/ee:1", Engine: engine, PullPolicy: "missing",
		PullArguments: []string{"--tls-verify=false", "--authfile=$AUTHDIR/auth.json", "--creds", "$AUTHDIR/x"},
	})
	if err := EnsureImage(io.Discard); err != nil {
		t.Fatal(err)
	}
	want := "pull --tls-verify=false --authfile=/srv/auth/auth.json --creds /srv/auth/x ghcr.io/x/ee:1"
	if got := pulls(t, log); len(got) != 1 || got[0] != want {
		t.Errorf("pull call = %q, want %q", got, want)
	}
}

// config deliberately doesn't import ee, so keep its copy of the list honest.
func TestPullPolicyListsAgree(t *testing.T) {
	if !reflect.DeepEqual(config.EEPullPolicies, ee.PullPolicies) {
		t.Errorf("config.EEPullPolicies = %q, ee.PullPolicies = %q", config.EEPullPolicies, ee.PullPolicies)
	}
}
