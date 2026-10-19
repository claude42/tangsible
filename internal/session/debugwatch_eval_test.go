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

package session

import "testing"

const watchTestSnapshot = `{
	"sometext": "something",
	"somebool": true,
	"somenull": null,
	"foo": 27,
	"nested": {"bar": "baz", "list": [10, 20, 30]},
	"items": ["a", "b", "c"],
	"complex": {"a": 1, "b": 2}
}`

func TestEvaluateWatchExpr(t *testing.T) {
	cases := []struct {
		name     string
		expr     string
		wantKind watchResultKind
		wantText string // only checked when wantKind == watchResolved
	}{
		{"bare string", "sometext", watchResolved, `"something"`},
		{"bare bool", "somebool", watchResolved, "true"},
		{"bare null", "somenull", watchResolved, "null"},
		{"bare int", "foo", watchResolved, "27"},
		{"dotted field", "nested.bar", watchResolved, `"baz"`},
		{"dotted then index", "nested.list[1]", watchResolved, "20"},
		{"negative index", "items[-1]", watchResolved, `"c"`},
		{"bracket single-quoted key", "nested['bar']", watchResolved, `"baz"`},
		{"bracket double-quoted key", "nested[\"bar\"]", watchResolved, `"baz"`},

		{"undefined root", "nosuchvar", watchUnavailable, ""},
		{"undefined field", "nested.nosuchkey", watchUnavailable, ""},
		{"index out of range", "items[99]", watchUnavailable, ""},
		{"negative index out of range", "items[-99]", watchUnavailable, ""},
		{"index into a scalar", "foo[0]", watchUnavailable, ""},
		{"field into a scalar", "foo.bar", watchUnavailable, ""},
		{"resolved but non-scalar (v1 scope)", "nested", watchUnavailable, ""},
		{"resolved list, non-scalar", "items", watchUnavailable, ""},

		{"empty string", "", watchRejected, ""},
		{"leading digit", "1abc", watchRejected, ""},
		{"trailing garbage", "foo +", watchRejected, ""},
		{"whitespace", "foo bar", watchRejected, ""},
		{"operator", "foo == 1", watchRejected, ""},
		{"unterminated bracket", "foo[0", watchRejected, ""},
		{"unterminated quote", "foo['bar", watchRejected, ""},
		{"empty brackets", "foo[]", watchRejected, ""},
		{"bare dash index", "foo[-]", watchRejected, ""},
		{"dot with nothing after", "foo.", watchRejected, ""},
		{"filter-like", "foo | default(1)", watchRejected, ""},
		{"call-like", "foo()", watchRejected, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := evaluateWatchExpr(c.expr, []byte(watchTestSnapshot))
			if got.kind != c.wantKind {
				t.Fatalf("evaluateWatchExpr(%q) kind = %v, want %v", c.expr, got.kind, c.wantKind)
			}
			if c.wantKind == watchResolved {
				if text := formatWatchScalar(got.value); text != c.wantText {
					t.Errorf("evaluateWatchExpr(%q) value = %s, want %s", c.expr, text, c.wantText)
				}
			}
		})
	}
}

func TestEvaluateWatchExprNoSnapshot(t *testing.T) {
	got := evaluateWatchExpr("foo", nil)
	if got.kind != watchUnavailable {
		t.Fatalf("evaluateWatchExpr with nil snapshot kind = %v, want watchUnavailable", got.kind)
	}
	// A syntax error must still be rejected before snapshot is even
	// consulted - rejection is a property of the text alone.
	got = evaluateWatchExpr("1abc", nil)
	if got.kind != watchRejected {
		t.Fatalf("evaluateWatchExpr(%q) with nil snapshot kind = %v, want watchRejected", "1abc", got.kind)
	}
}

func TestFormatWatchScalar(t *testing.T) {
	cases := []struct {
		name string
		v    any
		want string
	}{
		{"nil", nil, "null"},
		{"true", true, "true"},
		{"false", false, "false"},
		{"string", "hi", `"hi"`},
		{"integral float", float64(42), "42"},
		{"fractional float", 3.5, "3.5"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := formatWatchScalar(c.v); got != c.want {
				t.Errorf("formatWatchScalar(%v) = %q, want %q", c.v, got, c.want)
			}
		})
	}
}
