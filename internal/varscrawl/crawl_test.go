package varscrawl

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func raw(s string) json.RawMessage { return json.RawMessage(s) }

func layer(level, entity string, vars map[string]rawDef) rawLayer {
	return rawLayer{Level: level, Entity: entity, Variables: vars}
}

func TestMergeOrdersHighestFirstAndNamesAreSorted(t *testing.T) {
	res := merge(rawResult{Host: "h", Layers: []rawLayer{
		layer("role defaults", "app", map[string]rawDef{"port": {Value: raw("80"), Path: "/p/d.yml", Line: 1}, "a": {Value: raw(`"x"`), Path: "/p/d.yml", Line: 2}}),
		layer("inventory host_vars", "h", map[string]rawDef{"port": {Value: raw("4000"), Path: "/p/h.yml", Line: 1}}),
	}})
	if len(res.Variables) != 2 || res.Variables[0].Name != "a" || res.Variables[1].Name != "port" {
		t.Fatalf("variables = %+v", res.Variables)
	}
	port := res.Variables[1]
	if len(port.Defs) != 2 || port.Defs[0].Level != "inventory host_vars" || port.Defs[1].Level != "role defaults" {
		t.Errorf("port definitions = %+v", port.Defs)
	}
	if port.Checked || port.Disagree {
		t.Errorf("no answer from ansible: nothing was checked, got %+v", port)
	}
}

func TestMergeAgreementKeepsTheOrder(t *testing.T) {
	res := merge(rawResult{
		Layers: []rawLayer{
			layer("a", "x", map[string]rawDef{"v": {Value: raw("1"), Path: "/f", Line: 1}}),
			layer("b", "y", map[string]rawDef{"v": {Value: raw("2"), Path: "/g", Line: 3}}),
		},
		Winners: map[string]rawDef{"v": {Value: raw("2"), Path: "/g", Line: 3}},
	})
	v := res.Variables[0]
	if !v.Checked || v.Disagree || v.Defs[0].Level != "b" {
		t.Errorf("got %+v", v)
	}
}

func TestMergeDisagreementMovesAnsiblesDefinitionToTheTop(t *testing.T) {
	res := merge(rawResult{
		Layers: []rawLayer{
			layer("a", "x", map[string]rawDef{"v": {Value: raw("1"), Path: "/f", Line: 1}}),
			layer("b", "y", map[string]rawDef{"v": {Value: raw("2"), Path: "/g", Line: 3}}),
		},
		Winners: map[string]rawDef{"v": {Value: raw("1"), Path: "/f", Line: 1}},
	})
	v := res.Variables[0]
	if !v.Disagree || v.Defs[0].Level != "a" || v.Defs[1].Level != "b" || v.Defs[0].FromAnsible {
		t.Errorf("got %+v", v)
	}
}

func TestMergeAnswerTheCrawlNeverFoundIsKept(t *testing.T) {
	res := merge(rawResult{
		Layers:  []rawLayer{layer("a", "x", map[string]rawDef{"v": {Value: raw("1"), Path: "/f", Line: 1}})},
		Winners: map[string]rawDef{"v": {Value: raw("9"), Path: "/elsewhere", Line: 7}},
	})
	v := res.Variables[0]
	if !v.Disagree || len(v.Defs) != 2 || !v.Defs[0].FromAnsible || v.Defs[0].Path != "/elsewhere" {
		t.Errorf("got %+v", v)
	}
}

func TestAgreesWithoutAnOriginComparesValues(t *testing.T) {
	d := Definition{Value: raw("true"), Path: "/f", Line: 2}
	if !agrees(d, rawDef{Value: raw("true")}) {
		t.Error("equal values, winner untagged: agree")
	}
	if agrees(d, rawDef{Value: raw("false")}) {
		t.Error("different values, winner untagged: disagree")
	}
	if !agrees(Definition{Desc: "<CLI option '-e'>"}, rawDef{Desc: "<CLI option '-e'>"}) {
		t.Error("the same description agrees")
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	if _, err := Parse([]byte("not json")); err == nil {
		t.Error("want an error")
	}
}

// The real loader against the fixture project (needs ansible).
func TestRunFixtureProject(t *testing.T) {
	if _, err := exec.LookPath("ansible-playbook"); err != nil {
		t.Skip("ansible-playbook not installed")
	}
	root, _ := filepath.Abs("../../testdata/variables")
	res, err := Run("web1", filepath.Join(root, "site.yml"), []string{"-i", filepath.Join(root, "inventory", "hosts.ini")})
	if err != nil {
		t.Fatal(err)
	}
	get := func(name string) Variable {
		for _, v := range res.Variables {
			if v.Name == name {
				return v
			}
		}
		t.Fatalf("variable %q not found in %+v", name, res.Variables)
		return Variable{}
	}
	port := get("listen_port")
	var levels []string
	for _, d := range port.Defs {
		levels = append(levels, d.Level+"/"+d.Entity)
	}
	want := "inventory host_vars/web1,playbook group_vars/web,inventory group_vars/web,inventory group_vars/all,role defaults/app"
	if strings.Join(levels, ",") != want {
		t.Errorf("listen_port precedence:\n got %s\nwant %s", strings.Join(levels, ","), want)
	}
	if !port.Checked || port.Disagree {
		t.Errorf("the crawl and ansible must agree on the fixture: %+v", port)
	}
	if string(port.Defs[0].Value) != "4000" || !strings.HasSuffix(port.Defs[0].Path, "inventory/host_vars/web1.yml") || port.Defs[0].Line != 1 {
		t.Errorf("winner = %+v", port.Defs[0])
	}
	for _, v := range res.Variables {
		if v.Disagree {
			t.Errorf("%s: the crawl disagrees with ansible: %+v", v.Name, v.Defs)
		}
	}
	// a boolean carries no origin of its own in ansible; the crawl finds its line
	pb := get("playbook_only")
	if string(pb.Defs[0].Value) != "true" || !strings.HasSuffix(pb.Defs[0].Path, "group_vars/web.yml") || pb.Defs[0].Line != 2 {
		t.Errorf("playbook_only = %+v", pb.Defs[0])
	}
	if got := get("app_name").Defs[0]; got.Level != "role vars" || string(got.Value) != `"app-role-vars"` {
		t.Errorf("app_name winner = %+v", got)
	}
	if got := get("tier").Defs[0]; got.Level != "inventory group_vars" || got.Entity != "all" {
		t.Errorf("a group_vars file beats the inventory file's [all:vars]: %+v", got)
	}
	for _, name := range []string{"inventory_hostname", "group_names", "inventory_file"} {
		for _, v := range res.Variables {
			if v.Name == name {
				t.Errorf("magic variable %s must not be listed", name)
			}
		}
	}
}

func TestRunUnknownHostAndNoPlaybook(t *testing.T) {
	if _, err := exec.LookPath("ansible-playbook"); err != nil {
		t.Skip("ansible-playbook not installed")
	}
	root, _ := filepath.Abs("../../testdata/variables")
	inv := []string{"-i", filepath.Join(root, "inventory", "hosts.ini")}
	if _, err := Run("nosuchhost", "", inv); err == nil || !strings.Contains(err.Error(), "nosuchhost") {
		t.Errorf("unknown host: err = %v", err)
	}
	res, err := Run("db1", "", inv)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Plays) != 0 {
		t.Errorf("no playbook, no plays: %v", res.Plays)
	}
	_ = os.Stdout
}

// An inventory beside the playbook shares its group_vars/host_vars directories:
// the files are listed once, as the playbook's (the higher level).
func TestRunInventoryBesidePlaybookListsSharedFilesOnce(t *testing.T) {
	if _, err := exec.LookPath("ansible-playbook"); err != nil {
		t.Skip("ansible-playbook not installed")
	}
	dir := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("hosts.ini", "[web]\nweb1\n")
	write("site.yml", "- hosts: web\n  gather_facts: false\n  tasks: []\n")
	write("group_vars/web.yml", "colour: blue\n")
	write("host_vars/web1.yml", "size: 3\n")
	res, err := Run("web1", filepath.Join(dir, "site.yml"), []string{"-i", filepath.Join(dir, "hosts.ini")})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Variables) != 2 {
		t.Fatalf("variables = %+v", res.Variables)
	}
	for _, v := range res.Variables {
		if len(v.Defs) != 1 || !strings.HasPrefix(v.Defs[0].Level, "playbook ") || v.Disagree {
			t.Errorf("%s should have one definition, the playbook's: %+v", v.Name, v.Defs)
		}
	}
	// Without a playbook there is no playbook level: the inventory's own.
	res, err = Run("web1", "", []string{"-i", filepath.Join(dir, "hosts.ini")})
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range res.Variables {
		if len(v.Defs) != 1 || !strings.HasPrefix(v.Defs[0].Level, "inventory ") {
			t.Errorf("%s without a playbook should be an inventory definition: %+v", v.Name, v.Defs)
		}
	}
}
