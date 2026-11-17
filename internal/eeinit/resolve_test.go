package eeinit

import (
	"reflect"
	"testing"
)

func TestCandidates(t *testing.T) {
	u := ModuleUse{Module: "docker_container", Collections: []string{"community.docker", "x.y"}}
	want := []string{"community.docker.docker_container", "x.y.docker_container", "docker_container"}
	if got := Candidates(u); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got := Candidates(ModuleUse{Module: "community.docker.docker_container"}); got != nil {
		t.Errorf("a qualified name needs no lookup, got %v", got)
	}
}

func TestResolve(t *testing.T) {
	uses := []ModuleUse{
		{Module: "docker_container", Collections: []string{"community.docker"}, Loc: "a.yml:1"},
		{Module: "copy", Loc: "a.yml:2"},
		{Module: "frobnicate", Loc: "a.yml:3"},
		{Module: "community.general.ufw", Loc: "a.yml:4"},
		{Module: "ansible.builtin.file", Loc: "a.yml:5"},
		{Module: "docker_container", Collections: []string{"community.docker"}, Loc: "b.yml:1"},
		{Module: "docker_container", Collections: []string{"community.docker"}, Loc: "b.yml:1"},
	}
	found := map[string]string{
		"community.docker.docker_container": "community.docker",
		"copy":                              "ansible.builtin",
	}
	roles := []RoleUse{{Role: "x.y.web", Collection: "x.y"}}
	needed, todos := Resolve(uses, roles, found)
	want := []Needed{
		{Collection: "community.docker", Items: []Item{{"docker_container", []string{"a.yml:1", "b.yml:1"}}}},
		{Collection: "community.general", Items: []Item{{"community.general.ufw", []string{"a.yml:4"}}}},
		{Collection: "x.y", Items: []Item{{"role x.y.web", nil}}},
	}
	if !reflect.DeepEqual(needed, want) {
		t.Errorf("needed:\n got %+v\nwant %+v", needed, want)
	}
	if len(todos) != 1 || todos[0].Loc != "a.yml:3" {
		t.Errorf("todos = %+v", todos)
	}
}

func TestResolveSearchListOrder(t *testing.T) {
	u := ModuleUse{Module: "m", Collections: []string{"a.a", "b.b"}, Loc: "x:1"}
	needed, _ := Resolve([]ModuleUse{u}, nil, map[string]string{"b.b.m": "b.b", "m": "z.z"})
	if len(needed) != 1 || needed[0].Collection != "b.b" {
		t.Errorf("needed = %+v, want the search list to win over the bare name", needed)
	}
}

func TestLookupCandidates(t *testing.T) {
	uses := []LookupUse{
		{Name: "dig", Collections: []string{"community.general"}},
		{Name: "env"},
		{Name: "community.hashi_vault.hashi_vault"},
		{Name: "env"},
	}
	want := []string{"community.general.dig", "community.hashi_vault.hashi_vault", "dig", "env"}
	if got := LookupCandidates(uses); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestResolveLookups(t *testing.T) {
	uses := []LookupUse{
		{Name: "dig", Loc: "a.yml:1"},
		{Name: "dig", Loc: "b.yml:2"},
		{Name: "community.general.dig", Loc: "c.yml:3"},
		{Name: "env", Loc: "a.yml:3"},
		{Name: "community.hashi_vault.hashi_vault", Loc: "a.yml:4"},
		{Name: "frobnicate", Loc: "a.yml:5"},
		{Name: "short", Collections: []string{"x.y"}, Loc: "a.yml:6"},
	}
	found := map[string]PluginDoc{
		"dig":                               {Collection: "community.general", Requirements: []string{"dnspython (python library)"}},
		"community.general.dig":             {Collection: "community.general", Requirements: []string{"dnspython (python library)"}},
		"env":                               {Collection: "ansible.builtin"},
		"community.hashi_vault.hashi_vault": {Collection: "community.hashi_vault", Requirements: []string{"hvac"}},
		"x.y.short":                         {Collection: "x.y"},
	}
	needed, plugins, todos := ResolveLookups(uses, found)
	wantNeeded := []Needed{
		{Collection: "community.general", Items: []Item{{"lookup community.general.dig", []string{"c.yml:3"}}, {"lookup dig", []string{"a.yml:1", "b.yml:2"}}}, Hints: []string{"dig: dnspython (python library)"}},
		{Collection: "community.hashi_vault", Items: []Item{{"lookup community.hashi_vault.hashi_vault", []string{"a.yml:4"}}}, Hints: []string{"community.hashi_vault.hashi_vault: hvac"}},
		{Collection: "x.y", Items: []Item{{"lookup short", []string{"a.yml:6"}}}},
	}
	if !reflect.DeepEqual(needed, wantNeeded) {
		t.Errorf("needed:\n got %+v\nwant %+v", needed, wantNeeded)
	}
	var fq []string
	for _, p := range plugins {
		if !p.Controller || p.Kind != "lookup" {
			t.Errorf("a lookup always runs on the controller: %+v", p)
		}
		fq = append(fq, p.FQCN)
	}
	// builtin lookups are kept (a rule may apply) but add no collection
	wantFQ := []string{"community.general.dig", "community.general.dig", "community.general.dig", "ansible.builtin.env", "community.hashi_vault.hashi_vault", "x.y.short"}
	if !reflect.DeepEqual(fq, wantFQ) {
		t.Errorf("plugins = %v, want %v", fq, wantFQ)
	}
	if len(todos) != 1 || todos[0].Loc != "a.yml:5" {
		t.Errorf("todos = %+v", todos)
	}
}

func TestMergeNeeded(t *testing.T) {
	a := []Needed{{Collection: "c.a", Items: []Item{{"m1", []string{"x:1"}}}}, {Collection: "c.b", Items: []Item{{"m2", nil}}}}
	b := []Needed{{Collection: "c.a", Items: []Item{{"lookup l", []string{"y:2"}}}, Hints: []string{"l: lib"}}, {Collection: "c.0", Items: []Item{{"lookup z", nil}}}}
	got := MergeNeeded(a, b)
	want := []Needed{
		{Collection: "c.0", Items: []Item{{"lookup z", nil}}},
		{Collection: "c.a", Items: []Item{{"lookup l", []string{"y:2"}}, {"m1", []string{"x:1"}}}, Hints: []string{"l: lib"}},
		{Collection: "c.b", Items: []Item{{"m2", nil}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestModulePlugins(t *testing.T) {
	uses := []ModuleUse{
		{Module: "synchronize", Collections: []string{"ansible.posix"}, Loc: "a:1"},
		{Module: "community.general.ufw", Loc: "a:2", Controller: true},
		{Module: "copy", Loc: "a:3"},
		{Module: "unknown", Loc: "a:4"},
	}
	found := map[string]string{"ansible.posix.synchronize": "ansible.posix", "copy": "ansible.builtin"}
	got := ModulePlugins(uses, found)
	var fq []string
	for _, p := range got {
		fq = append(fq, p.FQCN)
	}
	if want := []string{"ansible.posix.synchronize", "community.general.ufw", "ansible.builtin.copy"}; !reflect.DeepEqual(fq, want) {
		t.Errorf("got %v, want %v", fq, want)
	}
	if got[1].Controller != true || got[0].Controller != false {
		t.Errorf("controller flags: %+v", got)
	}
}
