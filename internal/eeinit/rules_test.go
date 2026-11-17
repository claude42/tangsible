package eeinit

import (
	"reflect"
	"testing"
)

func TestImpliedPackages(t *testing.T) {
	plugins := []Plugin{
		{Kind: "lookup", FQCN: "community.general.dig", Label: "dig", Loc: "a.yml:1", Controller: true},
		{Kind: "lookup", FQCN: "community.general.dig", Label: "dig", Loc: "b.yml:2", Controller: true},
		{Kind: "lookup", FQCN: "community.hashi_vault.hashi_vault", Label: "hashi_vault", Loc: "a.yml:5", Controller: true},
		{Kind: "lookup", FQCN: "ansible.builtin.env", Label: "env", Loc: "a.yml:6", Controller: true}, // no rule
		// synchronize's action plugin runs rsync on the controller whatever the task's host
		{Kind: "module", FQCN: "ansible.posix.synchronize", Label: "synchronize", Loc: "s.yml:9", Controller: false},
		// the same name as a module is not the lookup's rule
		{Kind: "module", FQCN: "community.general.dig", Label: "dig", Loc: "x.yml:1", Controller: true},
	}
	got := ImpliedPackages(plugins)
	want := Implied{
		Pip: []StanzaEntry{
			{"dnspython", "lookup dig (a.yml:1, b.yml:2): the dig lookup needs dnspython"},
			{"hvac", "lookup hashi_vault (a.yml:5): the hashi_vault lookup needs hvac"},
		},
		System: []StanzaEntry{{"rsync", "synchronize (s.yml:9): synchronize runs rsync on the controller"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
	if got := ImpliedPackages(nil); len(got.Pip)+len(got.System) != 0 {
		t.Errorf("nothing used, nothing implied: %+v", got)
	}
}

func TestImpliedPackagesControllerOnlyRule(t *testing.T) {
	saved := Rules
	defer func() { Rules = saved }()
	Rules = []Rule{{Plugin: "a.b.mod", Kind: "module", Pip: []string{"lib"}, Why: "needs lib", ControllerOnly: true}}
	remote := ImpliedPackages([]Plugin{{Kind: "module", FQCN: "a.b.mod", Label: "mod", Loc: "x:1", Controller: false}})
	if len(remote.Pip) != 0 {
		t.Errorf("a remote task's module needs its package on the target, not in the EE: %+v", remote)
	}
	local := ImpliedPackages([]Plugin{{Kind: "module", FQCN: "a.b.mod", Label: "mod", Loc: "x:2", Controller: true}})
	if len(local.Pip) != 1 || local.Pip[0].Package != "lib" {
		t.Errorf("a controller-side one needs it: %+v", local)
	}
}

func TestRulesAreWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range Rules {
		if r.Plugin == "" || (r.Kind != "module" && r.Kind != "lookup") || r.Why == "" || len(r.Pip)+len(r.System) == 0 {
			t.Errorf("malformed rule %+v", r)
		}
		if _, ok := fqcnCollection(r.Plugin); !ok {
			t.Errorf("%q is not fully qualified", r.Plugin)
		}
		if k := r.Kind + " " + r.Plugin; seen[k] {
			t.Errorf("duplicate rule %s", k)
		} else {
			seen[k] = true
		}
	}
}
