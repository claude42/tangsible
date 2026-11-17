package eeinit

import "testing"

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
		{nil, Options{}, true},
		{[]string{"build"}, Options{}, true},
		{[]string{"init", "--base"}, Options{}, true},
		{[]string{"init", "--base="}, Options{}, true},
		{[]string{"init", "--base", "--other"}, Options{}, true},
		{[]string{"init", "a.yml", "b.yml"}, Options{}, true},
		{[]string{"init", "-i", "hosts"}, Options{}, true},
	} {
		got, err := ParseArgs(c.args)
		if (err != nil) != c.bad || (!c.bad && got != c.want) {
			t.Errorf("ParseArgs(%v) = %+v, %v; want %+v (bad=%v)", c.args, got, err, c.want, c.bad)
		}
	}
}
