package eeinit

import (
	"reflect"
	"testing"
)

func TestParseArgs(t *testing.T) {
	for _, c := range []struct {
		args []string
		want Options
		bad  bool
	}{
		{[]string{"init"}, Options{}, false},
		{[]string{"init", "site.yml"}, Options{Playbook: "site.yml"}, false},
		{[]string{"init", "--base", "img:1", "site.yml"}, Options{Playbook: "site.yml", Base: "img:1"}, false},
		{[]string{"init", "site.yml", "--base=img:1"}, Options{Playbook: "site.yml", Base: "img:1"}, false},
		{[]string{"init", "--resolve-packages"}, Options{ResolvePackages: true}, false},
		{[]string{"init", "--resolve-packages", "site.yml", "--base", "i"}, Options{Playbook: "site.yml", Base: "i", ResolvePackages: true}, false},
		{[]string{"init", "--no-backup"}, Options{NoBackup: true}, false},
		{[]string{"init", "site.yml", "--no-backup", "--base", "i"}, Options{Playbook: "site.yml", Base: "i", NoBackup: true}, false},
		{[]string{"init", "--offline"}, Options{Offline: true}, false},
		{[]string{"init", "--latest", "site.yml"}, Options{Playbook: "site.yml", Latest: true}, false},
		{[]string{"init", "--offline", "--resolve-packages"}, Options{Offline: true, ResolvePackages: true}, false},
		{[]string{"init", "--offline", "--latest"}, Options{}, true},
		{[]string{"init", "-t", "a:1", "--tag", "b:2", "site.yml"}, Options{Playbook: "site.yml", Tags: []string{"a:1", "b:2"}}, false},
		{[]string{"init", "--tag=a:1"}, Options{Tags: []string{"a:1"}}, false},
		{[]string{"init", "-t"}, Options{}, true},
		{[]string{"init", "-t", "--latest"}, Options{}, true},
		{[]string{"init", "--tag="}, Options{}, true},
		{nil, Options{}, true},
		{[]string{"build"}, Options{}, true},
		{[]string{"init", "--base"}, Options{}, true},
		{[]string{"init", "--base="}, Options{}, true},
		{[]string{"init", "--base", "--other"}, Options{}, true},
		{[]string{"init", "a.yml", "b.yml"}, Options{}, true},
		{[]string{"init", "-i", "hosts"}, Options{}, true},
	} {
		got, err := ParseArgs(c.args)
		if (err != nil) != c.bad || (!c.bad && !reflect.DeepEqual(got, c.want)) {
			t.Errorf("ParseArgs(%v) = %+v, %v; want %+v (bad=%v)", c.args, got, err, c.want, c.bad)
		}
	}
}
