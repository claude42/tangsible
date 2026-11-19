package eepush

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseArgs(t *testing.T) {
	for _, c := range []struct {
		args []string
		want Options
		bad  bool
	}{
		{nil, Options{File: "execution-environment.yml"}, false},
		{[]string{"-f", "ee.yml"}, Options{File: "ee.yml"}, false},
		{[]string{"--file=ee.yml", "--yes"}, Options{File: "ee.yml", Yes: true}, false},
		{[]string{"-y"}, Options{File: "execution-environment.yml", Yes: true}, false},
		{[]string{"-f"}, Options{}, true},
		{[]string{"-f", "-y"}, Options{}, true},
		{[]string{"--file="}, Options{}, true},
		{[]string{"-t", "x"}, Options{}, true},
		{[]string{"x:1"}, Options{}, true},
	} {
		got, err := ParseArgs(c.args)
		if (err != nil) != c.bad || (!c.bad && got != c.want) {
			t.Errorf("ParseArgs(%v) = %+v, %v; want %+v (bad=%v)", c.args, got, err, c.want, c.bad)
		}
	}
}

func TestRegistry(t *testing.T) {
	for name, want := range map[string]string{
		"registry.example.com/team/x:1":     "registry.example.com",
		"registry.example.com/x@sha256:abc": "registry.example.com",
		"localhost:5000/x:1":                "localhost:5000",
		"localhost/x:1":                     "localhost",
		"x:1":                               "",
		"team/x:1":                          "",
		"localhost:5000":                    "",
		"x@sha256:abc":                      "",
		"ghcr.io/o/x":                       "ghcr.io",
		"10.0.0.5:5000/x":                   "10.0.0.5:5000",
	} {
		if got := Registry(name); got != want {
			t.Errorf("Registry(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestLocal(t *testing.T) {
	for name, want := range map[string]bool{
		"std-ee:latest":                 true,
		"team/x:1":                      true,
		"localhost/x:1":                 false,
		"localhost:5000/x:1":            false,
		"registry.example.com/team/x:1": false,
	} {
		if got := Local(name); got != want {
			t.Errorf("Local(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestProblem(t *testing.T) {
	for name, bad := range map[string]bool{
		"registry.example.com/team/x:1": false,
		"localhost:5000/x:1":            false,
		"x:1":                           false,
		"team/x:1":                      false,
		"localhost/x:1":                 true,
	} {
		if got := Problem(name) != ""; got != bad {
			t.Errorf("Problem(%q) = %q, want a problem: %v", name, Problem(name), bad)
		}
	}
}

func TestSummary(t *testing.T) {
	one := Summary([]Image{{"r.io/t/x:1", "aaaaaaaaaaaa", 612 << 20}}, nil)
	if want := "Push 1 name to r.io?\n  r.io/t/x:1  image aaaaaaaaaaaa, 612 MB\n"; one != want {
		t.Errorf("one name:\n%q\nwant\n%q", one, want)
	}
	same := Summary([]Image{
		{"r.io/t/x:1", "aaaaaaaaaaaa", 612 << 20},
		{"r.io/t/x:latest", "aaaaaaaaaaaa", 612 << 20},
	}, nil)
	if want := "Push 2 names to r.io?\n  r.io/t/x:1       image aaaaaaaaaaaa, 612 MB\n  r.io/t/x:latest  (same image)\n"; same != want {
		t.Errorf("same image:\n%q\nwant\n%q", same, want)
	}
	two := Summary([]Image{
		{"r.io/t/x:1", "aaaaaaaaaaaa", 1 << 30},
		{"q.io/t/x:1", "aaaaaaaaaaaa", 1 << 30},
		{"r.io/t/x:2", "bbbbbbbbbbbb", 1 << 30},
	}, nil)
	want := "Push 3 names to 2 registries?\n" +
		"  r.io\n    r.io/t/x:1  image aaaaaaaaaaaa, 1.0 GB\n    r.io/t/x:2  image bbbbbbbbbbbb, 1.0 GB\n" +
		"  q.io\n    q.io/t/x:1  (same image)\n"
	if two != want {
		t.Errorf("two registries:\n%q\nwant\n%q", two, want)
	}
}

func TestSummarySkipped(t *testing.T) {
	got := Summary([]Image{{"r.io/t/x:1", "aaaaaaaaaaaa", 612 << 20}}, []string{"x:1", "x:latest"})
	want := "Push 1 name to r.io?\n  r.io/t/x:1  image aaaaaaaaaaaa, 612 MB\nNot pushed (no registry, local only): x:1, x:latest\n"
	if got != want {
		t.Errorf("got\n%q\nwant\n%q", got, want)
	}
}

func TestAccepts(t *testing.T) {
	for a, want := range map[string]bool{
		"y": true, "Y\n": true, "yes": true, " YES \n": true,
		"": false, "\n": false, "n": false, "no": false, "yep": false, "ye": false,
	} {
		if got := Accepts(a); got != want {
			t.Errorf("Accepts(%q) = %v, want %v", a, got, want)
		}
	}
}

func TestAuthFailure(t *testing.T) {
	// The line podman prints for registry:2 with htpasswd, no login or wrong
	// credentials.
	real := "Error: trying to reuse blob sha256:75f1 at destination: checking whether a blob sha256:75f1 exists in localhost:5055/test/img: authentication required"
	if !AuthFailure(real) {
		t.Error("the real message is not recognised")
	}
	for _, other := range []string{"", "Error: localhost:5099/x/y:1: image not known", "dial tcp: connection refused"} {
		if AuthFailure(other) {
			t.Errorf("%q taken for an authentication failure", other)
		}
	}
}

// fakePodman puts a podman script first on PATH: it knows the images in
// $FAKE_IMAGES, records every call in $FAKE_LOG, and fails a push of
// $FAKE_FAIL with the authentication message.
func fakePodman(t *testing.T, images ...string) (log string) {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
echo "$@" >> "$FAKE_LOG"
case "$1" in
image)
	for last; do :; done
	case " $FAKE_IMAGES " in *" $last "*) echo "0123456789abcdef0123 1048576"; exit 0;; esac
	exit 1;;
push)
	shift; shift; f=$1; shift
	if [ "$1" = "$FAKE_FAIL" ]; then echo "Error: $1: authentication required" >&2; exit 125; fi
	printf sha256:feed > "$f"; echo "Copying blob" >&2; exit 0;;
esac
exit 1
`
	if err := os.WriteFile(filepath.Join(dir, "podman"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	log = filepath.Join(dir, "log")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_LOG", log)
	t.Setenv("FAKE_IMAGES", strings.Join(images, " "))
	t.Setenv("FAKE_FAIL", "")
	return log
}

func definition(t *testing.T, tags ...string) string {
	t.Helper()
	text := "version: 3\n"
	if len(tags) > 0 {
		text += "options:\n  tags:\n"
		for _, tag := range tags {
			text += "    - " + tag + "\n"
		}
	}
	path := filepath.Join(t.TempDir(), "ee.yml")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func pushes(t *testing.T, log string) []string {
	t.Helper()
	b, _ := os.ReadFile(log)
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "push ") {
			out = append(out, l[strings.LastIndexByte(l, ' ')+1:])
		}
	}
	return out
}

func TestPush(t *testing.T) {
	const a, b = "r.io/t/x:1", "r.io/t/x:latest"
	t.Run("yes pushes every name in order", func(t *testing.T) {
		log := fakePodman(t, a, b)
		var out, errb strings.Builder
		code := push(Options{File: definition(t, a, b), Yes: true}, strings.NewReader(""), false, &out, &errb)
		if code != 0 {
			t.Fatalf("exit %d: %s", code, errb.String())
		}
		if got := pushes(t, log); strings.Join(got, " ") != a+" "+b {
			t.Errorf("pushed %v", got)
		}
		if want := "Pushed " + a + " (sha256:feed)\nPushed " + b + " (sha256:feed)\n"; out.String() != want {
			t.Errorf("stdout %q, want %q", out.String(), want)
		}
		if !strings.Contains(errb.String(), "Push 2 names to r.io?") || strings.Contains(errb.String(), "Proceed?") {
			t.Errorf("summary without a question expected:\n%s", errb.String())
		}
	})
	t.Run("asks, and a y pushes", func(t *testing.T) {
		log := fakePodman(t, a)
		var out, errb strings.Builder
		code := push(Options{File: definition(t, a)}, strings.NewReader("y\n"), true, &out, &errb)
		if code != 0 || len(pushes(t, log)) != 1 || !strings.Contains(errb.String(), "Proceed? [y/N]") {
			t.Errorf("exit %d, pushes %v, stderr:\n%s", code, pushes(t, log), errb.String())
		}
	})
	for _, answer := range []string{"\n", "n\n", ""} {
		t.Run("declined: "+strings.TrimSpace(answer), func(t *testing.T) {
			log := fakePodman(t, a)
			var out, errb strings.Builder
			code := push(Options{File: definition(t, a)}, strings.NewReader(answer), true, &out, &errb)
			if code != 1 || len(pushes(t, log)) != 0 || !strings.Contains(errb.String(), "not pushed") {
				t.Errorf("exit %d, pushes %v, stderr:\n%s", code, pushes(t, log), errb.String())
			}
		})
	}
	t.Run("no terminal and no --yes", func(t *testing.T) {
		log := fakePodman(t, a)
		var out, errb strings.Builder
		code := push(Options{File: definition(t, a)}, strings.NewReader("y\n"), false, &out, &errb)
		if code != 1 || len(pushes(t, log)) != 0 || !strings.Contains(errb.String(), "--yes") {
			t.Errorf("exit %d, pushes %v, stderr:\n%s", code, pushes(t, log), errb.String())
		}
	})
	t.Run("local-only names are left out and said so", func(t *testing.T) {
		log := fakePodman(t, a, "std:1")
		var out, errb strings.Builder
		code := push(Options{File: definition(t, "std:1", a), Yes: true}, strings.NewReader(""), false, &out, &errb)
		if code != 0 {
			t.Fatalf("exit %d: %s", code, errb.String())
		}
		if got := pushes(t, log); strings.Join(got, " ") != a {
			t.Errorf("pushed %v", got)
		}
		if !strings.Contains(errb.String(), "Not pushed (no registry, local only): std:1") {
			t.Errorf("the skipped name is not reported:\n%s", errb.String())
		}
	})
	t.Run("only local names: nothing to push", func(t *testing.T) {
		log := fakePodman(t, "std:1")
		var out, errb strings.Builder
		code := push(Options{File: definition(t, "std:1", "std:latest"), Yes: true}, strings.NewReader(""), false, &out, &errb)
		if code != 1 || len(pushes(t, log)) != 0 || !strings.Contains(errb.String(), "nothing to push") {
			t.Errorf("exit %d, pushes %v, stderr:\n%s", code, pushes(t, log), errb.String())
		}
	})
	t.Run("all problems listed, nothing pushed", func(t *testing.T) {
		log := fakePodman(t, a)
		var out, errb strings.Builder
		code := push(Options{File: definition(t, a, "localhost/x:1", "r.io/t/missing:1"), Yes: true}, strings.NewReader(""), false, &out, &errb)
		if code != 1 || len(pushes(t, log)) != 0 {
			t.Errorf("exit %d, pushes %v", code, pushes(t, log))
		}
		for _, want := range []string{"nothing was pushed", "localhost/x:1 is in local storage", "r.io/t/missing:1: no such image"} {
			if !strings.Contains(errb.String(), want) {
				t.Errorf("stderr lacks %q:\n%s", want, errb.String())
			}
		}
	})
	t.Run("no tags", func(t *testing.T) {
		fakePodman(t)
		var out, errb strings.Builder
		code := push(Options{File: definition(t), Yes: true}, strings.NewReader(""), false, &out, &errb)
		if code != 1 || !strings.Contains(errb.String(), "names no image") {
			t.Errorf("exit %d, stderr:\n%s", code, errb.String())
		}
	})
	t.Run("missing definition", func(t *testing.T) {
		fakePodman(t)
		var out, errb strings.Builder
		code := push(Options{File: filepath.Join(t.TempDir(), "nope.yml"), Yes: true}, strings.NewReader(""), false, &out, &errb)
		if code != 1 || !strings.Contains(errb.String(), "ee init") {
			t.Errorf("exit %d, stderr:\n%s", code, errb.String())
		}
	})
	t.Run("a failure stops, names what was pushed and hints at the login", func(t *testing.T) {
		log := fakePodman(t, a, b, "q.io/t/x:1")
		t.Setenv("FAKE_FAIL", b)
		var out, errb strings.Builder
		code := push(Options{File: definition(t, a, b, "q.io/t/x:1"), Yes: true}, strings.NewReader(""), false, &out, &errb)
		if code != 1 {
			t.Errorf("exit %d", code)
		}
		if got := pushes(t, log); strings.Join(got, " ") != a+" "+b {
			t.Errorf("pushed %v; the name after the failure must not be tried", got)
		}
		for _, want := range []string{"pushing " + b + " failed", "not logged in? run: podman login r.io", "already pushed: " + a} {
			if !strings.Contains(errb.String(), want) {
				t.Errorf("stderr lacks %q:\n%s", want, errb.String())
			}
		}
	})
}
