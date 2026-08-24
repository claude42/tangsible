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

package inventory

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestFlattenInventoryHosts(t *testing.T) {
	t.Run("dedupes and sorts across nested groups", func(t *testing.T) {
		raw := map[string]json.RawMessage{
			"all": json.RawMessage(`{"children": ["web", "db"]}`),
			"web": json.RawMessage(`{"hosts": ["zeta", "alpha"]}`),
			"db":  json.RawMessage(`{"hosts": ["mid", "alpha"]}`),
		}
		got := FlattenInventoryHosts(raw)
		want := []string{"alpha", "mid", "zeta"}
		if !slices.Equal(got, want) {
			t.Errorf("FlattenInventoryHosts() = %v, want %v", got, want)
		}
	})

	t.Run("a host with no vars is still found via the group's own hosts list", func(t *testing.T) {
		// _meta.hostvars alone would miss this - confirmed empirically
		// that ansible-inventory --list omits a no-vars host from
		// _meta.hostvars entirely, which is exactly why this walks the
		// group tree instead.
		raw := map[string]json.RawMessage{
			"all": json.RawMessage(`{"children": ["web"]}`),
			"web": json.RawMessage(`{"hosts": ["novars_host"]}`),
		}
		got := FlattenInventoryHosts(raw)
		if !slices.Equal(got, []string{"novars_host"}) {
			t.Errorf("FlattenInventoryHosts() = %v, want [novars_host]", got)
		}
	})

	t.Run("no all group at all - empty result, not a panic", func(t *testing.T) {
		if got := FlattenInventoryHosts(map[string]json.RawMessage{}); len(got) != 0 {
			t.Errorf("FlattenInventoryHosts(empty) = %v, want empty", got)
		}
	})

	t.Run("a cyclic children reference doesn't loop forever", func(t *testing.T) {
		raw := map[string]json.RawMessage{
			"all": json.RawMessage(`{"children": ["a"]}`),
			"a":   json.RawMessage(`{"hosts": ["h1"], "children": ["b"]}`),
			"b":   json.RawMessage(`{"hosts": ["h2"], "children": ["a"]}`),
		}
		got := FlattenInventoryHosts(raw)
		if !slices.Equal(got, []string{"h1", "h2"}) {
			t.Errorf("FlattenInventoryHosts() = %v, want [h1 h2]", got)
		}
	})
}
