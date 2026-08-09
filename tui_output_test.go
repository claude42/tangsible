package main

import "testing"

func TestRoleFromPath(t *testing.T) {
	cases := []struct {
		name string
		path string
		want string
	}{
		{
			name: "role-sourced task",
			path: "/home/user/project/roles/myrole/tasks/main.yml:1",
			want: "myrole",
		},
		{
			name: "role-sourced handler",
			path: "/home/user/project/roles/myrole/handlers/main.yml:3",
			want: "myrole",
		},
		{
			name: "play-level task, not role-sourced",
			path: "/home/user/project/site.yml:8",
			want: "",
		},
		{
			name: "included task file outside any role",
			path: "/home/user/project/tasks/setup.yml:1",
			want: "",
		},
		{
			name: "empty path",
			path: "",
			want: "",
		},
		{
			name: "role directory in the name but not the expected layout",
			path: "/home/user/project/not-roles/myrole/tasks/main.yml:1",
			want: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := roleFromPath(c.path); got != c.want {
				t.Errorf("roleFromPath(%q) = %q, want %q", c.path, got, c.want)
			}
		})
	}
}
