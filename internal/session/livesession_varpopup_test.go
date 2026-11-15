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

import (
	"encoding/json"
	"testing"
)

func TestVarPopupValueText(t *testing.T) {
	if got := varPopupValueText(vaultPlaceholder); got != "<vaulted>" {
		t.Errorf("placeholder: %q", got)
	}
	if got := varPopupValueText("hello"); got != `"hello"` {
		t.Errorf("string: %q", got)
	}
	if got := varPopupValueText(float64(8080)); got != "8080" {
		t.Errorf("number: %q", got)
	}
}

// A vaulted value (and a secret inside a derived string) is masked before
// the pop-up evaluates a word, so it can only ever show the placeholder.
func TestVarPopupMasksVaulted(t *testing.T) {
	vars := json.RawMessage(`{"pw":"hunter2hunter2","url":"db://hunter2hunter2@host","plain":"ok"}`)
	masked := maskSnapshot(vars, vaultMask{Vaulted: [][]any{{"pw"}}, Secrets: []string{"hunter2hunter2"}})
	for expr, want := range map[string]string{"pw": "<vaulted>", "url": "db://<vaulted>@host", "plain": `"ok"`} {
		res := evaluateWatchExpr(expr, masked)
		if res.kind != watchResolved {
			t.Fatalf("%s: not resolved", expr)
		}
		got := varPopupValueText(res.value)
		if expr == "url" {
			got = res.value.(string)
		}
		if got != want {
			t.Errorf("%s = %q, want %q", expr, got, want)
		}
	}
}
