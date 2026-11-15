// Copyright 2026 Klaus Wissmann
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package ansibledoc

import (
	"reflect"
	"testing"
)

const moduleDocFixture = `{
 "ansible.builtin.uri": {"doc": {
  "short_description": "Interacts with webservices",
  "options": {
   "url": {"description": ["HTTP or HTTPS URL in the form (http|https)://host.domain[:port]/path.", "Second paragraph."], "required": true, "type": "str"},
   "status_code": {"default": [200], "description": ["A list of C(codes)."], "elements": "int", "type": "list"},
   "follow_redirects": {"choices": {"all": "x", "none": "y", "safe": "z"}, "default": "safe", "description": ["See O(url)."]},
   "backup": {"aliases": ["bak"], "default": false, "description": ["Make a backup."], "type": "bool"},
   "timeout": {"default": null, "description": "Seconds, see L(the docs,https://example.org/a\\,b).", "type": "int"},
   "headers": {"type": "dict", "description": ["Extra headers."], "suboptions": {
     "name": {"description": ["Header name."], "required": true}
   }}
  }}}
}`

func TestParseModuleDoc(t *testing.T) {
	m, err := parseModuleDoc([]byte(moduleDocFixture))
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "ansible.builtin.uri" || m.ShortDescription != "Interacts with webservices" {
		t.Errorf("header: %+v", m)
	}
	url, ok := m.Lookup([]string{"url"})
	if !ok || url.Description != "HTTP or HTTPS URL in the form (http|https)://host.domain[:port]/path." || !url.Required || url.Type != "str" {
		t.Errorf("url: %+v", url)
	}
	if o, _ := m.Lookup([]string{"status_code"}); o.Type != "list of int" || o.Default != "[200]" || !o.HasDefault || o.Description != "A list of codes." {
		t.Errorf("status_code: %+v", o)
	}
	if o, _ := m.Lookup([]string{"follow_redirects"}); !reflect.DeepEqual(o.Choices, []string{"all", "none", "safe"}) || o.Default != "safe" || o.Type != "str" || o.Description != "See url." {
		t.Errorf("follow_redirects: %+v", o)
	}
	if o, _ := m.Lookup([]string{"backup"}); !o.HasDefault || o.Default != "false" {
		t.Errorf("backup: %+v", o)
	}
	if o, ok := m.Lookup([]string{"bak"}); !ok || o.Description != "Make a backup." {
		t.Errorf("alias: %+v", o)
	}
	if o, _ := m.Lookup([]string{"timeout"}); o.HasDefault || o.Description != "Seconds, see the docs." {
		t.Errorf("timeout (null default): %+v", o)
	}
	if o, ok := m.Lookup([]string{"headers", "name"}); !ok || o.Description != "Header name." || !o.Required {
		t.Errorf("suboption: %+v", o)
	}
	if _, ok := m.Lookup([]string{"headers", "nope"}); ok {
		t.Error("unknown suboption found")
	}
	if _, ok := m.Lookup(nil); ok {
		t.Error("empty path found")
	}
}

func TestParseModuleDocNotFound(t *testing.T) {
	if _, err := parseModuleDoc([]byte(`{}`)); err == nil {
		t.Error("{} (ansible-doc's answer for an unknown module) must be an error")
	}
}

func TestParseKeywordDocs(t *testing.T) {
	k, err := parseKeywordDocs([]byte(`{"when": {"applies_to": ["Block","Task"], "description": "Conditional C(expression).", "type": "string"}}`))
	if err != nil {
		t.Fatal(err)
	}
	w := k["when"]
	if w.Description != "Conditional expression." || w.Type != "string" || !reflect.DeepEqual(w.AppliesTo, []string{"Block", "Task"}) {
		t.Errorf("when: %+v", w)
	}
}

func TestStripMarkup(t *testing.T) {
	cases := map[string]string{
		"plain text":                           "plain text",
		"Use O(src) or C(content).":            "Use src or content.",
		"If V(true), see I(this) and B(that).": "If true, see this and that.",
		"See L(the FAQ,https://x.org/a#b) now": "See the FAQ now",
		"At U(https://x.org).":                 "At https://x.org.",
		"Module M(ansible.builtin.copy).":      "Module ansible.builtin.copy.",
		"Returns RV(stdout).":                  "Returns stdout.",
		"a\n   b\t c":                          "a b c",
		`Escaped C(a\)b) and L(x\, y,u)`:       "Escaped a)b and x, y",
		"not markup: FOO(bar) and ABC(d)":      "not markup: FOO(bar) and ABC(d)",
		"":                                     "",
	}
	for in, want := range cases {
		if got := StripMarkup(in); got != want {
			t.Errorf("StripMarkup(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseKeywordNames(t *testing.T) {
	list := "\x1b[0maccelerate: '*DEPRECATED*, set to True to use accelerate connection plugin.'\n" +
		"accelerate_ipv6: '*DEPRECATED*, set to True to force accelerate plugin to use ipv6\n" +
		"  for its connection.'\n" +
		"when: Conditional expression, determines if an iteration of a task is run or not.\n" +
		"  See also: things\n"
	got := parseKeywordNames(list)
	want := []string{"accelerate", "accelerate_ipv6", "when"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
