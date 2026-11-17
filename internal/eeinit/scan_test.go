package eeinit

import (
	"reflect"
	"testing"
)

func uses(fs FileScan) []string {
	var out []string
	for _, u := range fs.Uses {
		out = append(out, u.Module+"@"+u.Loc)
	}
	return out
}

func TestScanYAMLPlaybook(t *testing.T) {
	src := `- hosts: all
  collections: [community.general]
  pre_tasks:
    - name: p
      ping:
  tasks:
    - name: a
      docker_container: {name: x}
      when: foo
      with_items: [1]
    - block:
        - community.docker.docker_image: {name: z}
      rescue:
        - action: ansible.posix.synchronize src=a dest=b
      always:
        - local_action:
            module: community.crypto.openssl_csr
    - include_tasks: "{{ f }}.yml"
    - action: "{{ m }}"
    - name: collection on the task
      collections: [a.b]
      thing:
  handlers:
    - name: h
      listen: x
      service: {name: y}
  vars:
    x: 1
`
	got := ScanYAML("site.yml", []byte(src), nil)
	want := []string{
		"ping@site.yml:5",
		"docker_container@site.yml:8",
		"community.docker.docker_image@site.yml:12",
		"ansible.posix.synchronize@site.yml:14",
		"community.crypto.openssl_csr@site.yml:16",
		"include_tasks@site.yml:18",
		"thing@site.yml:22",
		"service@site.yml:26",
	}
	if g := uses(got); !reflect.DeepEqual(g, want) {
		t.Errorf("uses:\n got %v\nwant %v", g, want)
	}
	if len(got.Todos) != 2 {
		t.Fatalf("todos = %+v, want the templated include and action", got.Todos)
	}
	if got.Todos[0].Loc != "site.yml:18" || got.Todos[1].Loc != "site.yml:19" {
		t.Errorf("todo locations = %+v", got.Todos)
	}
	// collections: lists reach the tasks, the task's own first
	byName := map[string][]string{}
	for _, u := range got.Uses {
		byName[u.Module] = u.Collections
	}
	if !reflect.DeepEqual(byName["docker_container"], []string{"community.general"}) {
		t.Errorf("play collections = %v", byName["docker_container"])
	}
	if !reflect.DeepEqual(byName["thing"], []string{"a.b", "community.general"}) {
		t.Errorf("task collections = %v", byName["thing"])
	}
}

func TestScanYAMLTaskFileOwnCollection(t *testing.T) {
	src := "- name: a\n  docker_image: {}\n- name: b\n  import_role:\n    name: \"{{ r }}\"\n"
	got := ScanYAML("tasks/main.yml", []byte(src), []string{"community.docker"})
	if len(got.Uses) != 2 || !reflect.DeepEqual(got.Uses[0].Collections, []string{"community.docker"}) {
		t.Errorf("uses = %+v", got.Uses)
	}
	if len(got.Todos) != 1 {
		t.Errorf("todos = %+v, want the templated role name", got.Todos)
	}
}

func TestScanYAMLIgnoresOtherFiles(t *testing.T) {
	for name, src := range map[string]string{
		"vars file":   "a: 1\nb: 2\n",
		"broken":      "- [\n",
		"empty":       "",
		"scalar list": "- a\n- b\n",
	} {
		if got := ScanYAML("x.yml", []byte(src), nil); len(got.Uses)+len(got.Todos) != 0 {
			t.Errorf("%s: found %+v", name, got)
		}
	}
}

func cmds(fs FileScan) []string {
	var out []string
	for _, c := range fs.Commands {
		t := c.Text
		if len(c.Argv) > 0 {
			t = "argv:" + c.Argv[0]
		}
		out = append(out, c.Loc+" "+t)
	}
	return out
}

func TestScanYAMLControllerSideCommands(t *testing.T) {
	src := `- hosts: webservers
  tasks:
    - name: remote, not interesting
      command: remote-tool
    - name: delegated
      shell: deleg-tool | sort
      delegate_to: localhost
    - name: connection
      command: conn-tool
      connection: local
    - name: vars
      command: vars-tool
      vars:
        ansible_connection: local
    - name: local_action string
      local_action: command la-tool x
    - name: local_action mapping
      local_action:
        module: shell
        cmd: lam-tool
    - block:
        - command: block-tool
      delegate_to: 127.0.0.1
    - name: args form
      command:
        argv: [argv-tool, a]
      delegate_to: localhost
    - name: shell executable
      shell: echo hi
      args:
        executable: /bin/bash
      delegate_to: localhost
    - name: not a command module
      copy: {src: a, dest: b}
      delegate_to: localhost
- hosts: localhost
  tasks:
    - command: play-tool
    - ansible.builtin.shell: |
        multi-tool
        other-tool
- hosts: all
  connection: local
  tasks:
    - ansible.legacy.command: conn-play-tool
- hosts: [localhost, "::1"]
  tasks:
    - command: list-tool
- hosts: "{{ target }}"
  tasks:
    - command: templated-hosts-tool
`
	got := cmds(ScanYAML("site.yml", []byte(src), nil))
	want := []string{
		"site.yml:6 deleg-tool | sort",
		"site.yml:9 conn-tool",
		"site.yml:12 vars-tool",
		"site.yml:16 la-tool x",
		"site.yml:18 lam-tool",
		"site.yml:22 block-tool",
		"site.yml:25 argv:argv-tool",
		"site.yml:29 echo hi",
		"site.yml:38 play-tool",
		"site.yml:39 multi-tool\nother-tool\n",
		"site.yml:45 conn-play-tool",
		"site.yml:48 list-tool",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("commands:\n got %q\nwant %q", got, want)
	}
}

func TestScanYAMLRoleTaskFileHasNoPlayContext(t *testing.T) {
	src := "- command: plain-tool\n- command: local-tool\n  delegate_to: localhost\n"
	got := cmds(ScanYAML("roles/x/tasks/main.yml", []byte(src), nil))
	if !reflect.DeepEqual(got, []string{"roles/x/tasks/main.yml:2 local-tool"}) {
		t.Errorf("got %q", got)
	}
}

func TestScanYAMLCommandExecutable(t *testing.T) {
	src := "- shell: echo hi\n  args:\n    executable: /bin/bash\n  delegate_to: localhost\n"
	fs := ScanYAML("t.yml", []byte(src), nil)
	if len(fs.Commands) != 1 || fs.Commands[0].Executable != "/bin/bash" || !fs.Commands[0].Shell {
		t.Errorf("commands = %+v", fs.Commands)
	}
}
