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
		{"community.docker", []Item{{"docker_container", []string{"a.yml:1", "b.yml:1"}}}},
		{"community.general", []Item{{"community.general.ufw", []string{"a.yml:4"}}}},
		{"x.y", []Item{{"role x.y.web", nil}}},
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
