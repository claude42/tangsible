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
