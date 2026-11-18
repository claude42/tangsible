package eeverify

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"code.aw.net/claude/tangsible/internal/eeinit"
)

func TestParseArgs(t *testing.T) {
	for _, c := range []struct {
		args []string
		want Options
		bad  string
	}{
		{nil, Options{File: DefaultFile}, ""},
		{[]string{"site.yml"}, Options{File: DefaultFile, Playbook: "site.yml"}, ""},
		{[]string{"-f", "my.yml", "site.yml", "-i", "hosts"}, Options{File: "my.yml", Playbook: "site.yml", Args: []string{"-i", "hosts"}}, ""},
		{[]string{"--file=my.yml", "--keep"}, Options{File: "my.yml", Keep: true}, ""},
		{[]string{"--no-cache"}, Options{File: DefaultFile, NoCache: true}, ""},
		{[]string{"site.yml", "--show-output", "-v"}, Options{File: DefaultFile, Playbook: "site.yml", ShowOutput: true, Args: []string{"-v"}}, ""},
		{[]string{"site.yml", "--keep", "-e", "x=1", "--limit", "a"}, Options{File: DefaultFile, Playbook: "site.yml", Keep: true, Args: []string{"-e", "x=1", "--limit", "a"}}, ""},
		{[]string{"-i", "hosts"}, Options{File: DefaultFile, Args: []string{"-i", "hosts"}}, ""},
		{[]string{"-f"}, Options{}, "-f needs a file"},
		{[]string{"-f", "--keep"}, Options{}, "-f needs a file"},
		{[]string{"--file="}, Options{}, "--file needs a file"},
		{[]string{"site.yml", "--ask-vault-pass"}, Options{}, "not supported"},
		{[]string{"-K"}, Options{}, "not supported"},
	} {
		got, err := ParseArgs(c.args)
		if c.bad != "" {
			if err == nil || !strings.Contains(err.Error(), c.bad) {
				t.Errorf("ParseArgs(%v) error = %v, want one containing %q", c.args, err, c.bad)
			}
			continue
		}
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("ParseArgs(%v) = %+v, %v; want %+v", c.args, got, err, c.want)
		}
	}
}

func TestCheckScript(t *testing.T) {
	names := Names{
		Modules:  []string{"copy", "community.docker.docker_container", "evil'; rm -rf /; '"},
		Lookups:  []string{"env"},
		Binaries: []string{"jq", "/usr/bin/ip", "bad name", "$(x)"},
	}
	s := CheckScript(names)
	for _, want := range []string{
		"echo @@modules\nansible-doc -t module -j 'copy' 'community.docker.docker_container' 2>/dev/null\n",
		"echo @@lookups\nansible-doc -t lookup -j 'env' 2>/dev/null\n",
		"echo @@binaries\nfor n in 'jq' '/usr/bin/ip'; do",
		"command -v ansible-doc",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
	for _, bad := range []string{"rm -rf", "bad name", "$(x)"} {
		if strings.Contains(s, bad) {
			t.Errorf("unsafe name %q got into the script:\n%s", bad, s)
		}
	}
	// many names are asked in chunks
	var many []string
	for i := 0; i < 130; i++ {
		many = append(many, "m"+strings.Repeat("x", i%3)+string(rune('a'+i%26))+strings.Repeat("y", i/26))
	}
	if n := strings.Count(CheckScript(Names{Modules: many}), "ansible-doc -t module -j"); n != 3 {
		t.Errorf("%d ansible-doc calls for 130 names, want 3 chunks", n)
	}
	// nothing to look for still gives a script with all sections
	if e := CheckScript(Names{}); !strings.Contains(e, "@@binaries\n") || strings.Contains(e, "for n in") {
		t.Errorf("empty script:\n%s", e)
	}
}

func TestParseCheck(t *testing.T) {
	out := "@@modules\n" +
		`{"copy": {"doc": {"collection": "ansible.builtin"}}}` + "\n" +
		`{"community.docker.docker_container": {"doc": {"collection": "community.docker"}}}` + "\n" +
		"@@lookups\n" +
		`{"env": {"doc": {"collection": "ansible.builtin"}}}` + "\n" +
		"@@binaries\nmissing=jq\nmissing=/usr/bin/ip\nnoise\n"
	got, err := ParseCheck(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Modules) != 2 || got.Modules["community.docker.docker_container"].Collection != "community.docker" || len(got.Lookups) != 1 {
		t.Errorf("docs: modules %v lookups %v", got.Modules, got.Lookups)
	}
	if !reflect.DeepEqual(got.MissingBinaries, []string{"jq", "/usr/bin/ip"}) {
		t.Errorf("missing binaries = %v", got.MissingBinaries)
	}
	if _, err := ParseCheck("@@noansibledoc\n"); err == nil || !strings.Contains(err.Error(), "no ansible-doc") {
		t.Errorf("no ansible-doc: %v", err)
	}
	if _, err := ParseCheck("podman: error\n"); err == nil {
		t.Errorf("garbage must be an error")
	}
	// nothing found at all: empty sections are fine
	if got, err := ParseCheck("@@modules\n@@lookups\n@@binaries\n"); err != nil || len(got.Modules) != 0 {
		t.Errorf("empty sections: %+v, %v", got, err)
	}
}

func TestChecks(t *testing.T) {
	uses := []eeinit.ModuleUse{
		{Module: "docker_container", Collections: []string{"community.docker"}, Loc: "a.yml:1"},
		{Module: "docker_container", Collections: []string{"community.docker"}, Loc: "b.yml:2"},
		{Module: "copy", Loc: "a.yml:3"},
		{Module: "community.general.ufw", Loc: "a.yml:4"},
		{Module: "frobnicate", Loc: "a.yml:5"},
	}
	if got := ModuleNames(uses); !reflect.DeepEqual(got, []string{"community.docker.docker_container", "community.general.ufw", "copy", "docker_container", "frobnicate"}) {
		t.Errorf("names = %v", got)
	}
	found := map[string]eeinit.PluginDoc{
		"community.docker.docker_container": {Collection: "community.docker"},
		"copy":                              {Collection: "ansible.builtin"},
	}
	c := ModuleCheck(uses, found)
	want := Check{Total: 4, Missing: []Missing{{"community.general.ufw", []string{"a.yml:4"}}, {"frobnicate", []string{"a.yml:5"}}}}
	if !reflect.DeepEqual(c, want) {
		t.Errorf("got %+v\nwant %+v", c, want)
	}

	lc := LookupCheck([]eeinit.LookupUse{{Name: "env", Loc: "x:1"}, {Name: "dig", Collections: []string{"community.general"}, Loc: "x:2"}},
		map[string]eeinit.PluginDoc{"env": {}})
	if lc.Total != 2 || len(lc.Missing) != 1 || lc.Missing[0].Name != "dig" {
		t.Errorf("lookups: %+v", lc)
	}
	bc := BinaryCheck([]eeinit.BinaryUse{{Name: "jq", Locs: []string{"s:1"}}, {Name: "sort"}}, []string{"jq"})
	if bc.Total != 2 || len(bc.Missing) != 1 || bc.Missing[0].Name != "jq" {
		t.Errorf("binaries: %+v", bc)
	}
}

func TestRender(t *testing.T) {
	ok := Result{BuildDuration: 252 * time.Second, SyntaxRan: true, SyntaxOK: true, Checked: true,
		Modules: Check{Total: 3}, Lookups: Check{}, Binaries: Check{Total: 1}}
	text, code := Render(ok)
	want := `Build:           ok (4m12s)
Syntax check:    ok
Modules:         3 of 3 found in the image
Lookups:         none used by the playbook
Binaries:        1 of 1 found in the image
`
	if text != want || code != 0 {
		t.Errorf("code %d, text:\n%s", code, text)
	}

	bad := ok
	bad.SyntaxOK = false
	bad.SyntaxOutput = "ERROR! couldn't resolve module/action 'x'\nThe error appears to be in 'site.yml'\n"
	bad.Modules = Check{Total: 12, Missing: []Missing{{"community.docker.docker_container", []string{"site.yml:5"}}}}
	bad.Binaries = Check{Total: 2, Missing: []Missing{{"jq", []string{"site.yml:40", "a:1", "b:2"}}}}
	bad.Kept, bad.Tag = true, "localhost/x:1"
	text, code = Render(bad)
	for _, w := range []string{
		"Syntax check:    FAILED\n  ERROR! couldn't resolve module/action 'x'\n  The error appears to be in 'site.yml'\n",
		"Modules:         11 of 12 found in the image\n  not found: community.docker.docker_container (site.yml:5)\n    -> add the collection that provides it to the definition\n",
		"Binaries:        1 of 2 found in the image\n  missing: jq (site.yml:40, a:1, +1 more)\n    -> add it under dependencies.system (see ee init --resolve-packages)\n",
		"Image kept: localhost/x:1\n",
	} {
		if !strings.Contains(text, w) {
			t.Errorf("missing %q in:\n%s", w, text)
		}
	}
	if code != 1 {
		t.Errorf("code = %d", code)
	}

	// a failed build skips the rest
	text, code = Render(Result{BuildErr: "exit status 1", BuildTail: []string{"step 3 failed", "error: no such package"}, LogPath: ".tangsible/ee-verify.log"})
	for _, w := range []string{"Build:           FAILED (exit status 1)\n  step 3 failed\n", "  full log: .tangsible/ee-verify.log\n", "Syntax check:    skipped (the build failed)\n", "Modules:         skipped\n"} {
		if !strings.Contains(text, w) {
			t.Errorf("missing %q in:\n%s", w, text)
		}
	}
	if code != 1 {
		t.Errorf("code = %d", code)
	}

	// checks that couldn't run are failures, not passes
	text, code = Render(Result{BuildDuration: time.Second, SyntaxRan: true, SyntaxOK: true, CheckErr: "the image has no ansible-doc"})
	if !strings.Contains(text, "Modules:         could not run: the image has no ansible-doc\n") || code != 1 {
		t.Errorf("code %d, text:\n%s", code, text)
	}
	if text, code := Render(Result{Interrupted: true}); text != "Interrupted.\n" || code != 130 {
		t.Errorf("interrupted: %q %d", text, code)
	}
}

func TestParseCore(t *testing.T) {
	for in, want := range map[string]string{
		"ansible [core 2.21.6]\n  config file = None\n":            "2.21.6",
		"ansible-playbook [core 2.19.11]\n  python version = 3.13": "2.19.11",
		"ansible [core 2.20.0rc1]":                                 "2.20.0rc1",
		"ansible [core 2.18.1] (devel abc)":                        "2.18.1",
		"":                                                         "",
		"bash: ansible: command not found\n":                       "",
		"config file = None\nansible [core 2.2.0]\n":               "", // only the first line counts
	} {
		if got := ParseCore(in); got != want {
			t.Errorf("ParseCore(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCoreInCheck(t *testing.T) {
	if s := CheckScript(Names{}); !strings.HasPrefix(s, "echo @@core\nansible --version") {
		t.Errorf("the script must ask for the version first:\n%s", s)
	}
	got, err := ParseCheck("@@core\nansible [core 2.21.6]\n@@modules\n@@lookups\n@@binaries\n")
	if err != nil || got.AnsibleCore != "2.21.6" {
		t.Errorf("core: %+v, %v", got, err)
	}
	// no ansible in the image: no version, the checks still parse
	if got, err = ParseCheck("@@core\n@@modules\n@@lookups\n@@binaries\n"); err != nil || got.AnsibleCore != "" {
		t.Errorf("no core: %+v, %v", got, err)
	}
	// the version is read even when ansible-doc is missing
	if got, err = ParseCheck("@@core\nansible [core 2.21.6]\n@@noansibledoc\n"); err == nil || got.AnsibleCore != "2.21.6" {
		t.Errorf("no ansible-doc: %+v, %v", got, err)
	}
}

func TestRenderCore(t *testing.T) {
	base := Result{BuildDuration: time.Second, SyntaxRan: true, SyntaxOK: true, Checked: true}
	for _, c := range []struct {
		image, host, want string
	}{
		{"2.21.6", "2.19.11", "ansible-core:    2.21.6 in the image (host: 2.19.11)\n"},
		{"2.21.6", "", "ansible-core:    2.21.6 in the image\n"},
		{"2.21.6", "2.21.6", "ansible-core:    2.21.6 in the image (host: 2.21.6)\n"},
	} {
		r := base
		r.ImageCore, r.HostCore = c.image, c.host
		out, code := Render(r)
		if !strings.Contains(out, c.want) || code != 0 {
			t.Errorf("image %q host %q (exit %d):\n%s", c.image, c.host, code, out)
		}
	}
	// unknown in the image: no line; never a reason to fail
	if out, code := Render(base); strings.Contains(out, "ansible-core") || code != 0 {
		t.Errorf("no version known (exit %d):\n%s", code, out)
	}
}
