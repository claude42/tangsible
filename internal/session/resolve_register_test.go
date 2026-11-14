package session

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestResolveRegisterInText(t *testing.T) {
	src := "- name: t\n  command: echo hi\n  register: out\n"
	cases := []struct {
		name, snap, want string
	}{
		{"scalar", `{"out": "hi"}`, "- name: t\n  command: echo hi\n  register: out = \"hi\"\n"},
		{"missing", `{"other": 1}`, src},
		{"badsnap", `nope`, src},
		{"dict", `{"out": {"rc": 0, "stdout": "a long enough value to exceed the inline limit of sixty"}}`,
			"- name: t\n  command: echo hi\n  register: out =\n    {\n      \"rc\": 0,\n      \"stdout\": \"a long enough value to exceed the inline limit of sixty\"\n    }\n"},
	}
	for _, c := range cases {
		if got := resolveRegisterInText(src, json.RawMessage(c.snap)); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
	if got := resolveRegisterInText("- name: t\n  copy:\n    content: x\n", json.RawMessage(`{"out":1}`)); strings.Contains(got, "=") {
		t.Errorf("no register line must be unchanged, got %q", got)
	}
}

func TestResolveRegisterInTextTruncates(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"out":[`)
	for i := 0; i < 100; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString("1")
	}
	b.WriteString("]}")
	got := resolveRegisterInText("- name: t\n  register: out\n", json.RawMessage(b.String()))
	if !strings.Contains(got, "more lines)") {
		t.Errorf("expected truncation marker, got %q", got)
	}
}
