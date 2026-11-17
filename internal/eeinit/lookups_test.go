package eeinit

import (
	"reflect"
	"testing"
)

func lookupNames(us []LookupUse) []string {
	var out []string
	for _, u := range us {
		out = append(out, u.Name+"@"+u.Loc)
	}
	return out
}

func TestScanLookupsYAML(t *testing.T) {
	src := `- hosts: all
  collections: [community.general]
  vars:
    ip: "{{ lookup('dig', 'example.org') }}"
    home: '{{ lookup("env", "HOME") }}'
    q1: "{{ query('community.hashi_vault.hashi_vault', 'secret/x') }}"
    q2: "{{ q('ansible.builtin.items', [1]) }}"
    both: "{{ lookup('file', 'a') ~ lookup('file', 'b') }}"
    var: "{{ lookup(kind, 'x') }}"
    prose: "a sentence mentioning the word lookup and (parentheses)"
    num: 42
  tasks:
    - name: with loops
      debug: msg={{ item }}
      with_fileglob: "*.txt"
    - name: condition
      debug: msg=x
      when: lookup('env', 'DEBUG') | length > 0
    - name: block scalar
      shell: |
        echo hi
        echo "{{ lookup('pipe', 'date') }}"
    - name: collection on the task
      collections: [other.coll]
      debug:
        msg: "{{ lookup('thing', 1) }}"
`
	uses, tmpl := ScanLookups("site.yml", []byte(src), nil)
	want := []string{
		"dig@site.yml:4",
		"env@site.yml:5",
		"community.hashi_vault.hashi_vault@site.yml:6",
		"ansible.builtin.items@site.yml:7",
		"file@site.yml:8",
		"file@site.yml:8",
		"fileglob@site.yml:15",
		"env@site.yml:18",
		"pipe@site.yml:22",
		"thing@site.yml:26",
	}
	if got := lookupNames(uses); !reflect.DeepEqual(got, want) {
		t.Errorf("uses:\n got %v\nwant %v", got, want)
	}
	if tmpl != 1 {
		t.Errorf("templated = %d, want 1 (lookup(kind, ...))", tmpl)
	}
	byName := map[string][]string{}
	for _, u := range uses {
		byName[u.Name] = u.Collections
	}
	if !reflect.DeepEqual(byName["dig"], []string{"community.general"}) || !reflect.DeepEqual(byName["thing"], []string{"other.coll", "community.general"}) {
		t.Errorf("collections: dig %v, thing %v", byName["dig"], byName["thing"])
	}
}

func TestScanLookupsVarsFileAndOwnCollection(t *testing.T) {
	uses, _ := ScanLookups("group_vars/all.yml", []byte("secret: \"{{ lookup('hashi_vault', 'x') }}\"\nlist:\n  - \"{{ query('dig', 'y') }}\"\n"), []string{"acme.tools"})
	if got := lookupNames(uses); !reflect.DeepEqual(got, []string{"hashi_vault@group_vars/all.yml:1", "dig@group_vars/all.yml:3"}) {
		t.Errorf("got %v", got)
	}
	if !reflect.DeepEqual(uses[0].Collections, []string{"acme.tools"}) {
		t.Errorf("a collection role's own collection: %v", uses[0].Collections)
	}
	if u, n := ScanLookups("x.yml", []byte("- [\n"), nil); u != nil || n != 0 {
		t.Errorf("an unparseable file yields nothing")
	}
}

func TestScanTemplate(t *testing.T) {
	text := "line1 {{ lookup('env', 'A') }}\n{# {{ lookup('file', 'commented') }}\n spans lines #}\n{% set x = query(\"dig\", 'h') %}\n{{ lookup(name, 1) }}\n"
	uses, tmpl := ScanTemplate("t.j2", text)
	if got := lookupNames(uses); !reflect.DeepEqual(got, []string{"env@t.j2:1", "dig@t.j2:4"}) {
		t.Errorf("got %v", got)
	}
	if tmpl != 1 {
		t.Errorf("templated = %d", tmpl)
	}
}
