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

package host

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"code.aw.net/claude/tangsible/internal/inventory"
	"code.aw.net/claude/tangsible/internal/uikit"
)

// rawInventory builds a map[string]json.RawMessage fixture the same shape
// `ansible-inventory --list` produces, from a plain group-name -> group map
// - a small test-only convenience, same spirit as host_test.go's own
// fixture builders.
func rawInventory(t *testing.T, groups map[string]inventory.AnsibleInventoryGroup) map[string]json.RawMessage {
	t.Helper()
	raw := make(map[string]json.RawMessage, len(groups))
	for name, g := range groups {
		data, err := json.Marshal(g)
		if err != nil {
			t.Fatalf("marshal group %q: %v", name, err)
		}
		raw[name] = data
	}
	return raw
}

// findChild returns n's own direct child named name (of either kind), or
// nil - a small test-only lookup helper, since invNode exposes no public
// accessor of its own.
func findChild(n *invNode, name string) *invNode {
	for _, c := range n.children {
		if c.name == name {
			return c
		}
	}
	return nil
}

func TestBuildInventoryTree_BasicStructure(t *testing.T) {
	raw := rawInventory(t, map[string]inventory.AnsibleInventoryGroup{
		"all":       {Children: []string{"web", "db"}},
		"web":       {Hosts: []string{"web1", "web2"}},
		"db":        {Hosts: []string{"db1"}},
		"ungrouped": {},
	})
	root := buildInventoryTree(raw, "all")

	if root.kind != invGroup || root.name != "all" {
		t.Fatalf("root = %+v, want the invisible 'all' group node", root)
	}
	if len(root.children) != 2 {
		t.Fatalf("root has %d children, want 2 (web, db)", len(root.children))
	}

	web := findChild(root, "web")
	if web == nil {
		t.Fatal("root has no 'web' child")
	}
	if web.parent != root {
		t.Error("web.parent != root")
	}
	var webHosts []string
	for _, c := range web.children {
		if c.kind != invHost {
			t.Errorf("web's child %q has kind %v, want invHost", c.name, c.kind)
		}
		if c.parent != web {
			t.Errorf("web1/web2's parent isn't web")
		}
		webHosts = append(webHosts, c.name)
	}
	if !reflect.DeepEqual(webHosts, []string{"web1", "web2"}) {
		t.Errorf("web's own hosts = %v, want sorted [web1 web2]", webHosts)
	}
}

// TestBuildInventoryTree_DuplicateHost confirms a host that's a direct
// member of two different groups gets its own distinct *invNode under
// each - design-docs/HostVerb.md's own decision, matching real
// `ansible-inventory --graph` output.
func TestBuildInventoryTree_DuplicateHost(t *testing.T) {
	raw := rawInventory(t, map[string]inventory.AnsibleInventoryGroup{
		"all":    {Children: []string{"web", "webext"}},
		"web":    {Hosts: []string{"web1", "web2"}, Children: []string{"webext"}},
		"webext": {Hosts: []string{"web2"}},
	})
	root := buildInventoryTree(raw, "all")

	web := findChild(root, "web")
	webext := findChild(root, "webext")
	if web == nil || webext == nil {
		t.Fatal("expected both 'web' and 'webext' groups")
	}
	webextUnderWeb := findChild(web, "webext")
	if webextUnderWeb == nil {
		t.Fatal("expected 'webext' nested under 'web' too (it's a child group of web)")
	}
	if webextUnderWeb == webext {
		t.Error("webext's two occurrences (direct child of root, and nested under web) share one *invNode - they must be distinct so expand state never bleeds between them")
	}

	web2UnderWeb := findChild(web, "web2")
	web2UnderWebext := findChild(webext, "web2")
	if web2UnderWeb == nil || web2UnderWebext == nil {
		t.Fatal("expected web2 under both web and webext")
	}
	if web2UnderWeb == web2UnderWebext {
		t.Error("web2's two occurrences share one *invNode - they must be distinct")
	}
}

// TestBuildInventoryTree_CycleGuard confirms a cyclic children reference
// (a malformed inventory) terminates rather than recursing forever - the
// cyclic node still appears once, just without expanding into itself
// again.
func TestBuildInventoryTree_CycleGuard(t *testing.T) {
	raw := rawInventory(t, map[string]inventory.AnsibleInventoryGroup{
		"all": {Children: []string{"a"}},
		"a":   {Children: []string{"b"}},
		"b":   {Children: []string{"a"}},
	})
	root := buildInventoryTree(raw, "all")

	a := findChild(root, "a")
	if a == nil {
		t.Fatal("expected 'a' under root")
	}
	b := findChild(a, "b")
	if b == nil {
		t.Fatal("expected 'b' under 'a'")
	}
	aAgain := findChild(b, "a")
	if aAgain == nil {
		t.Fatal("expected 'a' to still appear once more under 'b' (shown, not recursed into)")
	}
	if len(aAgain.children) != 0 {
		t.Errorf("cyclic 'a' occurrence has %d children, want 0 (recursion must stop here)", len(aAgain.children))
	}
}

func TestApplyDefaultExpand(t *testing.T) {
	raw := rawInventory(t, map[string]inventory.AnsibleInventoryGroup{
		"all":    {Children: []string{"web"}},
		"web":    {Hosts: []string{"web1"}, Children: []string{"webext"}},
		"webext": {},
	})

	collapsed := buildInventoryTree(raw, "all")
	applyDefaultExpand(collapsed, false)
	if !collapsed.expanded {
		t.Error("root must always be expanded, regardless of descendantsExpanded")
	}
	web := findChild(collapsed, "web")
	if web.expanded {
		t.Error("web should default to collapsed when descendantsExpanded=false")
	}
	webext := findChild(web, "webext")
	if webext.expanded {
		t.Error("webext (nested deeper) should also default to collapsed")
	}

	expanded := buildInventoryTree(raw, "all")
	applyDefaultExpand(expanded, true)
	web2 := findChild(expanded, "web")
	if !web2.expanded {
		t.Error("web should be expanded when descendantsExpanded=true")
	}
	if !findChild(web2, "webext").expanded {
		t.Error("webext should also be expanded when descendantsExpanded=true")
	}
}

func TestCollectUniqueHostnames(t *testing.T) {
	raw := rawInventory(t, map[string]inventory.AnsibleInventoryGroup{
		"all":    {Children: []string{"web", "webext"}},
		"web":    {Hosts: []string{"web1", "web2"}, Children: []string{"webext"}},
		"webext": {Hosts: []string{"web2"}},
	})
	root := buildInventoryTree(raw, "all")
	got := collectUniqueHostnames(root)
	// Display order lists a group's own child groups before its direct
	// hosts (buildInventoryTree's own doc comment, matching real
	// `ansible-inventory --graph` output) - "webext" (nested under "web")
	// is reached before "web"'s own direct "web1"/"web2", so web2's first
	// occurrence in depth-first order is the one under webext.
	want := []string{"web2", "web1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("collectUniqueHostnames() = %v, want %v (web2 deduplicated despite two occurrences)", got, want)
	}
}

func TestCountInventoryNodes(t *testing.T) {
	raw := rawInventory(t, map[string]inventory.AnsibleInventoryGroup{
		"all":    {Children: []string{"web", "webext", "db"}},
		"web":    {Hosts: []string{"web1", "web2"}, Children: []string{"webext"}},
		"webext": {Hosts: []string{"web2"}},
		"db":     {Hosts: []string{"db1"}},
	})
	root := buildInventoryTree(raw, "all")
	groups, hosts := countInventoryNodes(root)
	if groups != 3 { // web, webext, db - "all" itself is never counted, never rendered
		t.Errorf("groups = %d, want 3", groups)
	}
	if hosts != 3 { // web1, web2 (deduplicated), db1
		t.Errorf("hosts = %d, want 3", hosts)
	}
}

func TestAllHostNodesInOrder(t *testing.T) {
	raw := rawInventory(t, map[string]inventory.AnsibleInventoryGroup{
		"all":    {Children: []string{"web"}},
		"web":    {Hosts: []string{"web2"}, Children: []string{"webext"}},
		"webext": {Hosts: []string{"web2"}},
	})
	root := buildInventoryTree(raw, "all")

	nodes := allHostNodesInOrder(root)
	// Display order lists "web"'s own child group "webext" before "web"'s
	// own direct hosts (buildInventoryTree's own doc comment), so the
	// occurrence nested under webext comes first, then web's own direct
	// one - web2 listed twice, each occurrence its own distinct entry,
	// not deduplicated by name.
	web := findChild(root, "web")
	wantFirst := findChild(findChild(web, "webext"), "web2")
	wantSecond := findChild(web, "web2")
	if len(nodes) != 2 {
		t.Fatalf("len(nodes) = %d, want 2 (web2 appears twice)", len(nodes))
	}
	if nodes[0] != wantFirst {
		t.Error("nodes[0] should be web2's occurrence nested under webext")
	}
	if nodes[1] != wantSecond {
		t.Error("nodes[1] should be web2's own direct occurrence under web")
	}
}

func TestExpandAncestors(t *testing.T) {
	raw := rawInventory(t, map[string]inventory.AnsibleInventoryGroup{
		"all":    {Children: []string{"web"}},
		"web":    {Children: []string{"webext"}},
		"webext": {Hosts: []string{"web1"}},
	})
	root := buildInventoryTree(raw, "all")
	applyDefaultExpand(root, false)

	web := findChild(root, "web")
	webext := findChild(web, "webext")
	host := findChild(webext, "web1")
	if web.expanded || webext.expanded {
		t.Fatal("expected both ancestors collapsed before expandAncestors")
	}

	expandAncestors(host)
	if !web.expanded || !webext.expanded {
		t.Error("expandAncestors should have expanded every ancestor group up to root")
	}
}

func TestFlattenInventoryRows_RespectsExpandState(t *testing.T) {
	raw := rawInventory(t, map[string]inventory.AnsibleInventoryGroup{
		"all": {Children: []string{"web"}},
		"web": {Hosts: []string{"web1"}},
	})
	root := buildInventoryTree(raw, "all")
	web := findChild(root, "web")

	web.expanded = false
	rows := flattenInventoryRows(root, nil, nil, nil)
	if len(rows) != 1 {
		t.Fatalf("collapsed: got %d rows, want 1 (just 'web')", len(rows))
	}
	if rows[0].node != web {
		t.Errorf("collapsed: rows[0].node = %q, want 'web'", rows[0].node.name)
	}

	web.expanded = true
	rows = flattenInventoryRows(root, nil, nil, nil)
	if len(rows) != 2 {
		t.Fatalf("expanded: got %d rows, want 2 (web, web1)", len(rows))
	}
	if rows[1].node.kind != invHost || rows[1].node.name != "web1" {
		t.Errorf("expanded: rows[1] = %+v, want host web1", rows[1].node)
	}
}

func TestInvGroupRowText(t *testing.T) {
	n := &invNode{kind: invGroup, name: "web", hostCount: 1}

	got := invGroupRowText(n, 1, false)
	if !strings.Contains(got, "web (1)") {
		t.Errorf("group row = %q, want the name followed by its host count", got)
	}
	if !strings.HasPrefix(got, "  ") {
		t.Errorf("group row = %q, want a 2-space indent for depth 1", got)
	}
	if !strings.Contains(got, uikit.PlayTitleColor) {
		t.Errorf("group row = %q, want it colored with PlayTitleColor (hostCount > 0)", got)
	}

	empty := &invNode{kind: invGroup, name: "empty", hostCount: 0}
	emptyText := invGroupRowText(empty, 0, false)
	if !strings.Contains(emptyText, "empty (0)") {
		t.Errorf("empty group row = %q, want the name followed by (0)", emptyText)
	}
	if !strings.Contains(emptyText, uikit.NothingReportedColor) {
		t.Errorf("empty group row = %q, want it colored with NothingReportedColor", emptyText)
	}
}

func TestComputeHostCounts(t *testing.T) {
	raw := rawInventory(t, map[string]inventory.AnsibleInventoryGroup{
		"all":    {Children: []string{"web", "empty"}},
		"web":    {Children: []string{"webext"}, Hosts: []string{"web1"}},
		"webext": {Hosts: []string{"web1", "web2"}}, // web1 repeated: must still count once
		"empty":  {},
	})
	root := buildInventoryTree(raw, "all")
	computeHostCounts(root)

	web := findChild(root, "web")
	if web.hostCount != 2 {
		t.Errorf("web.hostCount = %d, want 2 (web1, web2 - web1 not double-counted)", web.hostCount)
	}
	webext := findChild(web, "webext")
	if webext.hostCount != 2 {
		t.Errorf("webext.hostCount = %d, want 2", webext.hostCount)
	}
	empty := findChild(root, "empty")
	if empty.hostCount != 0 {
		t.Errorf("empty.hostCount = %d, want 0", empty.hostCount)
	}
}

func TestInvHostRowText(t *testing.T) {
	n := &invNode{kind: invHost, name: "web1"}
	got := invHostRowText(n, 2, false, true, true)
	if !strings.HasPrefix(got, "    ") {
		t.Errorf("host row = %q, want a 4-space indent for depth 2", got)
	}
	if !strings.Contains(got, "web1") {
		t.Errorf("host row = %q, want the hostname", got)
	}
}
