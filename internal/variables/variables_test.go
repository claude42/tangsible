package variables

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"code.aw.net/claude/tangsible/internal/varscrawl"
)

var tags = regexp.MustCompile(`\[[^\]]*\]`)

func plain(s string) string { return strings.ReplaceAll(tags.ReplaceAllString(s, ""), "[]", "[") }

func def(level, entity, value, path string, line int) varscrawl.Definition {
	return varscrawl.Definition{Level: level, Entity: entity, Value: json.RawMessage(value), Path: path, Line: line}
}

func TestValueSummary(t *testing.T) {
	for raw, want := range map[string]string{
		`"plain"`:             "plain",
		`4000`:                "4000",
		`true`:                "true",
		`null`:                "null",
		`"first\nsecond\n"`:   "first …",
		`"only\n"`:            "only",
		`{"a": 1,  "b": [2]}`: `{"a":1,"b":[2]}`,
		`"<vaulted>"`:         "<vaulted>",
	} {
		if got := valueSummary(json.RawMessage(raw)); got != want {
			t.Errorf("valueSummary(%s) = %q, want %q", raw, got, want)
		}
	}
}

func TestLevelAndLocationLabels(t *testing.T) {
	if got := levelLabel(def("inventory group_vars", "web", `1`, "/p/g.yml", 1)); got != "inventory group_vars (web)" {
		t.Errorf("level = %q", got)
	}
	if got := levelLabel(varscrawl.Definition{Level: "extra vars", Entity: "-e"}); got != "extra vars" {
		t.Errorf("extra vars = %q", got)
	}
	if got := levelLabel(varscrawl.Definition{Level: "ansible's answer", FromAnsible: true}); got != "ansible's answer" {
		t.Errorf("ansible's answer = %q", got)
	}
	if got := locationLabel(def("x", "y", `1`, "/proj/group_vars/web.yml", 3), "/proj"); got != "group_vars/web.yml:3" {
		t.Errorf("location = %q", got)
	}
	if got := locationLabel(varscrawl.Definition{Desc: "<CLI option '-e'>"}, "/proj"); got != "-e" {
		t.Errorf("location of -e = %q", got)
	}
}

func TestFlattenExpandsOnlyOpenVariables(t *testing.T) {
	vars := []varscrawl.Variable{
		{Name: "a", Defs: []varscrawl.Definition{def("l1", "x", `1`, "/f", 1), def("l2", "y", `2`, "/g", 1)}},
		{Name: "b", Defs: []varscrawl.Definition{def("l1", "x", `3`, "/f", 2)}, Disagree: true},
	}
	rows := flatten(vars, map[string]bool{"b": true})
	kinds := []rowKind{rowVariable, rowVariable, rowInfo, rowDefinition}
	if len(rows) != len(kinds) {
		t.Fatalf("rows = %+v", rows)
	}
	for i, k := range kinds {
		if rows[i].kind != k {
			t.Errorf("row %d kind = %v, want %v", i, rows[i].kind, k)
		}
	}
	if rows[2].text != disagreementText {
		t.Errorf("the disagreement line = %q", rows[2].text)
	}
	if got := flatten(vars, nil); len(got) != 2 {
		t.Errorf("collapsed: %d rows, want 2", len(got))
	}
}

func TestRowsLineUpAndShowTheWinner(t *testing.T) {
	vars := []varscrawl.Variable{
		{Name: "listen_port", Defs: []varscrawl.Definition{
			def("inventory host_vars", "web1", `4000`, "/proj/inventory/host_vars/web1.yml", 1),
			def("role defaults", "app", `80`, "/proj/roles/app/defaults/main.yml", 1),
		}},
		{Name: "x", Disagree: true, Defs: []varscrawl.Definition{def("role vars", "app", `"v"`, "/proj/roles/app/vars/main.yml", 2)}},
	}
	c := measure(vars, "/proj", 100)
	row := plain(variableRow(vars[0], false, false, c, "/proj"))
	for _, want := range []string{"▸ listen_port", "4000", "inventory/host_vars/web1.yml:1"} {
		if !strings.Contains(row, want) {
			t.Errorf("variable row lacks %q: %q", want, row)
		}
	}
	if got := plain(variableRow(vars[1], true, false, c, "/proj")); !strings.Contains(got, "▾ x ?") {
		t.Errorf("a disagreement is marked with ? after the name: %q", got)
	}
	win := plain(definitionRow(vars[0].Defs[0], true, false, c, "/proj"))
	other := plain(definitionRow(vars[0].Defs[1], false, false, c, "/proj"))
	if !strings.Contains(win, "● 4000") || !strings.Contains(win, "inventory host_vars (web1)") {
		t.Errorf("winner row: %q", win)
	}
	if !strings.Contains(other, "· 80") || !strings.Contains(other, "role defaults (app)") {
		t.Errorf("other row: %q", other)
	}
	if len([]rune(win)) != len([]rune(other)) {
		t.Errorf("rows of one list have different widths:\n%q\n%q", win, other)
	}
}

func TestFitCutsAndPads(t *testing.T) {
	if got := fit("abcdef", 4); got != "abc…" {
		t.Errorf("cut = %q", got)
	}
	if got := fit("ab", 4); got != "ab  " {
		t.Errorf("pad = %q", got)
	}
	if got := fit("ab", 0); got != "" {
		t.Errorf("zero = %q", got)
	}
}

func TestStyledSelectedIsOneRunAndEscapes(t *testing.T) {
	sel := styled(true, seg{"a", "red"}, seg{"[b]", "blue"})
	if strings.Count(sel, "[-:-:-]") != 1 || !strings.Contains(sel, "a[b[]") {
		t.Errorf("selected = %q", sel)
	}
	if got := styled(false, seg{"x", ""}); got != "x" {
		t.Errorf("no color must not emit an empty tag: %q", got)
	}
}

func TestNotes(t *testing.T) {
	if got := notes(varscrawl.Result{Notes: []string{"n1"}}, ""); len(got) != 2 || !strings.Contains(got[0], "no playbook") {
		t.Errorf("notes = %v", got)
	}
	if got := notes(varscrawl.Result{}, "site.yml"); len(got) != 0 {
		t.Errorf("notes = %v", got)
	}
}

func TestDefinitionSpan(t *testing.T) {
	lines := strings.Split("a: 1\nb:\n  c: 2\n  d: 3\n- listy\n\ne: 5\n[ini]\nx=1", "\n")
	for _, c := range []struct{ line, start, end int }{
		{1, 0, 0}, // a one-line value
		{2, 1, 4}, // b's nested lines, and the list item that follows
		{3, 2, 2}, // an indented line is just itself
		{7, 6, 6}, // trailing blank lines are not part of it
		{9, 8, 8}, // an ini line
		{0, -1, -1},
		{10, -1, -1},
	} {
		if s, e := definitionSpan(lines, c.line); s != c.start || e != c.end {
			t.Errorf("definitionSpan(line %d) = %d,%d, want %d,%d", c.line, s, e, c.start, c.end)
		}
	}
}

func TestScrollOffset(t *testing.T) {
	for _, c := range []struct{ start, height, want int }{
		{0, 40, 0}, {10, 40, 0}, {37, 40, 0}, {38, 40, 25}, {100, 40, 87}, {5, 3, 4}, {-1, 40, 0},
	} {
		if got := scrollOffset(c.start, c.height); got != c.want {
			t.Errorf("scrollOffset(%d, %d) = %d, want %d", c.start, c.height, got, c.want)
		}
	}
}

func TestRenderFile(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	yml := write("g.yml", "a: 1\nport: 2\nb:\n  c: 3\nz: [x]\n")
	text := renderFile(yml, 3)
	lines := strings.Split(text, "\n")
	if len(lines) != 5 {
		t.Fatalf("one text line per file line, got %d:\n%s", len(lines), text)
	}
	if !strings.Contains(lines[2], "[-:-:-]") || !strings.Contains(lines[3], "[-:-:-]") || strings.Contains(lines[1], "[-:-:-]") || strings.Contains(lines[4], "[-:-:-]") {
		t.Errorf("only the definition's lines (b: and its child) are marked:\n%s", text)
	}
	if got := plain(lines[3]); !strings.Contains(got, "  c: 3") {
		t.Errorf("the marked text keeps its indentation: %q", got)
	}
	if !strings.Contains(write("x.ini", "[all]\nh [x]\n"), "x.ini") || !strings.Contains(renderFile(filepath.Join(dir, "x.ini"), 2), "h [x[]") {
		t.Errorf("text with [ must be escaped")
	}
	if got := renderFile(write("v.yml", "$ANSIBLE_VAULT;1.1;AES256\n3633\n"), 1); !strings.Contains(got, "encrypted with ansible-vault") || strings.Contains(got, "3633") {
		t.Errorf("a vaulted file is not shown: %q", got)
	}
	if got := renderFile(filepath.Join(dir, "nope.yml"), 1); !strings.Contains(got, "Could not read") {
		t.Errorf("missing file: %q", got)
	}
	if got := renderFile(write("bin.dat", "a\x00b"), 1); !strings.Contains(got, "not text") {
		t.Errorf("binary: %q", got)
	}
	if got := renderFile(yml, 0); strings.Contains(got, "[-:-:-]") {
		t.Errorf("no line: nothing marked: %q", got)
	}
}

func TestPlainListing(t *testing.T) {
	vars := []varscrawl.Variable{
		{Name: "port", Defs: []varscrawl.Definition{
			{Level: "inventory host_vars", Entity: "web1", Value: json.RawMessage(`4444`), Path: "/p/host_vars/web1.yml", Line: 2},
			{Level: "role defaults", Entity: "app", Value: json.RawMessage(`80`), Path: "/p/roles/app/defaults/main.yml", Line: 1},
		}},
		{Name: "solo", Defs: []varscrawl.Definition{{Level: "extra vars", Entity: "-e", Value: json.RawMessage(`"x"`), Desc: "<CLI option '-e'>"}}},
	}
	got := plainListing(vars, "/p")
	for _, want := range []string{"port: 4444  # host_vars/web1.yml:2", "inventory host_vars (web1): 4444", "role defaults (app): 80", "solo: x"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Count(got, "solo") != 1 {
		t.Errorf("a variable with one definition has no sub-lines:\n%s", got)
	}
}
