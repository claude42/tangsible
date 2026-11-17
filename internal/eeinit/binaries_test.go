package eeinit

import (
	"reflect"
	"testing"
)

func TestCommandBinariesShell(t *testing.T) {
	for _, c := range []struct {
		name, text string
		want       []string
		templated  bool
	}{
		{"plain", "jq . file.json", []string{"jq"}, false},
		{"pipeline", "cat f | jq .a | sort -u", []string{"cat", "jq", "sort"}, false},
		{"and/or/semicolon", "make && ./run.sh || echo failed; ls", []string{"make", "ls"}, false},
		{"builtins", "cd /x && echo hi && test -f y && export A=1", nil, false},
		{"assignments", "FOO=1 BAR='a b' tool --x", []string{"tool"}, false},
		{"sudo", "sudo -u root -E tool", []string{"tool"}, false},
		{"env", "env -i A=1 tool", []string{"tool"}, false},
		{"time and nohup", "time nohup tool &", []string{"tool"}, false},
		{"command -v only looks up", "command -v jq", nil, false},
		{"command runs", "command tool", []string{"tool"}, false},
		{"quotes keep operators", `echo "a | b; c" && tool 'x && y'`, []string{"tool"}, false},
		{"absolute path", "/usr/local/bin/tool x", []string{"/usr/local/bin/tool"}, false},
		{"relative path is the project's", "./script.sh && bin/other", nil, false},
		{"if then else", "if grep -q x f; then tool; else other; fi", []string{"grep", "tool", "other"}, false},
		{"for loop", "for h in a b; do ping -c1 $h; done", []string{"ping"}, false},
		{"subshell", "(cd d && tool)", []string{"tool"}, false},
		{"command substitution is not split", "echo $(date | tr a b)", nil, false},
		{"comment", "# jq . f\ntool # jq\n", []string{"tool"}, false},
		{"redirections", "2>/dev/null tool > out", []string{"tool"}, false},
		{"heredoc body is data", "cat <<EOF\njq here\nEOF\ntool", []string{"cat", "tool"}, false},
		{"here-string", "tool <<< 'jq'", []string{"tool"}, false},
		{"multi-line", "tool1 a \\\n  b\ntool2", []string{"tool1", "tool2"}, false},
		{"templated binary", "{{ cli }} run", nil, true},
		{"variable as command", "$CLI run && tool", []string{"tool"}, true},
		{"eval", "eval \"$x\"", nil, true},
		{"a filter pipe in a template is not a shell pipe", "tool {{ a | default('x') }} | sort", []string{"tool", "sort"}, false},
		{"templated argument is fine", "tool {{ arg }}", []string{"tool"}, false},
	} {
		got, tmpl := commandBinaries(Command{Shell: true, Text: c.text})
		if !reflect.DeepEqual(got, c.want) || tmpl != c.templated {
			t.Errorf("%s: %q -> %v, templated=%v; want %v, %v", c.name, c.text, got, tmpl, c.want, c.templated)
		}
	}
}

func TestCommandBinariesNoShell(t *testing.T) {
	for _, c := range []struct {
		name string
		cmd  Command
		want []string
		tmpl bool
	}{
		{"free-form: first word only", Command{Text: "jq . f | sort"}, []string{"jq"}, false},
		{"quoted binary", Command{Text: `"/opt/my tool/run" x`}, nil, false}, // a space: not a plain path
		{"argv", Command{Argv: []string{"jq", "."}}, []string{"jq"}, false},
		{"argv templated", Command{Argv: []string{"{{ b }}"}}, nil, true},
		{"executable counts too", Command{Shell: true, Text: "echo hi", Executable: "/bin/bash"}, []string{"/bin/bash"}, false},
	} {
		got, tmpl := commandBinaries(c.cmd)
		if !reflect.DeepEqual(got, c.want) || tmpl != c.tmpl {
			t.Errorf("%s: -> %v, %v; want %v, %v", c.name, got, tmpl, c.want, c.tmpl)
		}
	}
}

func TestScanBinaries(t *testing.T) {
	cmds := []Command{
		{Loc: "a.yml:1", Shell: true, Text: "jq . f | sort"},
		{Loc: "b.yml:9", Text: "jq ."},
		{Loc: "a.yml:5", Shell: true, Text: "/proj/scripts/run.sh && /usr/bin/ip a"},
		{Loc: "a.yml:7", Shell: true, Text: "{{ x }}"},
		{Loc: "a.yml:8", Shell: true, Text: "{{ y }}"},
	}
	uses, tmpl := ScanBinaries(cmds, "/proj")
	want := []BinaryUse{
		{"/usr/bin/ip", []string{"a.yml:5"}},
		{"jq", []string{"a.yml:1", "b.yml:9"}},
		{"sort", []string{"a.yml:1"}},
	}
	if !reflect.DeepEqual(uses, want) || tmpl != 2 {
		t.Errorf("got %+v, templated %d; want %+v, 2", uses, tmpl, want)
	}
}
