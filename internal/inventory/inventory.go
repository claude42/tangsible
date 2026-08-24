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

// Package inventory is a small ansible-inventory --list client - the one
// piece host.go and template.go both genuinely need from each other
// (host.go's ListInventoryHosts calls this package's own FlattenInventoryHosts;
// template.go's resolveInventoryHost calls host.go's ListInventoryHosts) -
// pulled out into its own package specifically to break that two-way
// cycle rather than merging the two (much larger) files together.
package inventory

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// ansibleInventoryGroup is the shape of one non-"_meta" entry in
// `ansible-inventory --list`'s own JSON output - a group's own direct
// hosts and child group names. Confirmed empirically: a host with no vars
// of its own is entirely absent from "_meta.hostvars", so that map alone
// can't be trusted as the full host list - the group tree (starting from
// "all") is the only reliably complete source.
type AnsibleInventoryGroup struct {
	Hosts    []string `json:"hosts"`
	Children []string `json:"children"`
}

// FlattenInventoryHosts walks raw's group tree from "all" down through
// Children, collecting every group's own Hosts into a deduplicated,
// alphabetically sorted list. Sorted deliberately, not left in whatever
// order the source happened to produce - `ansible-inventory --list` and
// `ansible ... --list-hosts` were both confirmed empirically to return
// hosts in an inventory/pattern-matching order that isn't even consistent
// between the two tools, and Go's own map iteration order is
// unspecified - "the first host" needs one well-defined answer, not
// whatever an upstream tool's internal traversal happens to produce.
func FlattenInventoryHosts(raw map[string]json.RawMessage) []string {
	seen := map[string]bool{}
	visited := map[string]bool{}
	var walk func(name string)
	walk = func(name string) {
		if visited[name] {
			return // guards against a (malformed or cyclic) children loop
		}
		visited[name] = true
		data, ok := raw[name]
		if !ok {
			return
		}
		var g AnsibleInventoryGroup
		if err := json.Unmarshal(data, &g); err != nil {
			return
		}
		for _, h := range g.Hosts {
			seen[h] = true
		}
		for _, c := range g.Children {
			walk(c)
		}
	}
	walk("all")

	hosts := make([]string, 0, len(seen))
	for h := range seen {
		hosts = append(hosts, h)
	}
	sort.Strings(hosts)
	return hosts
}

// ListInventoryHosts runs `ansible-inventory --list`, forwarding
// passthroughArgs verbatim, and returns every host it finds
// (FlattenInventoryHosts, above) - shared by the "hosts" Verb's own
// full listing (host.go) and, via resolveInventoryHost (template.go),
// "template"'s single-host resolution.
func ListInventoryHosts(passthroughArgs []string) ([]string, error) {
	cmd := exec.Command("ansible-inventory", append([]string{"--list"}, passthroughArgs...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("ansible-inventory --list failed: %s", msg)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(out, &raw); err != nil {
		snippet := strings.TrimSpace(string(out))
		if len(snippet) > 300 {
			snippet = snippet[:300] + "..."
		}
		return nil, fmt.Errorf("ansible-inventory --list didn't produce valid JSON (%v) - it printed:\n%s", err, snippet)
	}
	return FlattenInventoryHosts(raw), nil
}
