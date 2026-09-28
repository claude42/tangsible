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
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"code.aw.net/claude/tangsible/internal/config"
	"code.aw.net/claude/tangsible/internal/inventory"
	"code.aw.net/claude/tangsible/internal/playbook"
	"code.aw.net/claude/tangsible/internal/uikit"
	"github.com/rivo/tview"
)

func TestParseHostArgs(t *testing.T) {
	cases := []struct {
		name         string
		args         []string
		wantHost     string
		wantPlaybook string
		wantRest     []string
		wantOK       bool
	}{
		{"hostname only", []string{"web1"}, "web1", "", []string{}, true},
		{"hostname and playbook", []string{"web1", "site.yml"}, "web1", "site.yml", []string{}, true},
		{"hostname and flags, no playbook", []string{"web1", "-i", "inv.ini"}, "web1", "", []string{"-i", "inv.ini"}, true},
		{"hostname, playbook, and flags", []string{"web1", "site.yml", "-e", "x=1"}, "web1", "site.yml", []string{"-e", "x=1"}, true},
		{"no args", nil, "", "", nil, false},
		{"flag-shaped first arg", []string{"-i", "inv.ini"}, "", "", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			host, playbook, rest, ok := ParseHostArgs(c.args)
			if ok != c.wantOK || host != c.wantHost || playbook != c.wantPlaybook || !reflect.DeepEqual(rest, c.wantRest) {
				t.Errorf("parseHostArgs(%v) = (%q, %q, %v, %v), want (%q, %q, %v, %v)",
					c.args, host, playbook, rest, ok, c.wantHost, c.wantPlaybook, c.wantRest, c.wantOK)
			}
		})
	}
}

func inventoryGroupJSON(t *testing.T, hosts, children []string) json.RawMessage {
	t.Helper()
	g := inventory.AnsibleInventoryGroup{Hosts: hosts, Children: children}
	data, err := json.Marshal(g)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}

func TestHostGroupChain(t *testing.T) {
	// all -> [prod, other]; prod -> [web]; web -> hosts [web1]; other -> hosts [other1]
	raw := map[string]json.RawMessage{
		"all":   inventoryGroupJSON(t, nil, []string{"prod", "other"}),
		"prod":  inventoryGroupJSON(t, nil, []string{"web"}),
		"web":   inventoryGroupJSON(t, []string{"web1"}, nil),
		"other": inventoryGroupJSON(t, []string{"other1"}, nil),
	}

	got := HostGroupChain(raw, "web1")
	want := []GroupMembership{
		{Group: "web"},
		{Group: "prod", Via: "web"},
		{Group: "all", Via: "prod"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("hostGroupChain() = %+v, want %+v", got, want)
	}

	if got := HostGroupChain(raw, "nonexistent"); len(got) != 0 {
		t.Errorf("hostGroupChain(nonexistent) = %+v, want empty", got)
	}
}

func TestHostGroupChain_DiamondDedup(t *testing.T) {
	// host is a direct member of two groups (web, db), both children of prod -
	// "all" and "prod" must each appear exactly once in the result.
	raw := map[string]json.RawMessage{
		"all":  inventoryGroupJSON(t, nil, []string{"prod"}),
		"prod": inventoryGroupJSON(t, nil, []string{"web", "db"}),
		"web":  inventoryGroupJSON(t, []string{"combo1"}, nil),
		"db":   inventoryGroupJSON(t, []string{"combo1"}, nil),
	}
	got := HostGroupChain(raw, "combo1")
	seen := map[string]int{}
	for _, m := range got {
		seen[m.Group]++
	}
	for name, count := range seen {
		if count != 1 {
			t.Errorf("group %q appeared %d times, want exactly once: %+v", name, count, got)
		}
	}
	if seen["prod"] != 1 || seen["all"] != 1 || seen["web"] != 1 || seen["db"] != 1 {
		t.Errorf("hostGroupChain() = %+v, missing an expected ancestor", got)
	}
}

func TestDedupProcessorModels(t *testing.T) {
	// Real shape confirmed empirically: repeating [index, vendor, model] triples.
	realShape := []interface{}{
		"0", "AuthenticAMD", "AMD Ryzen 5 3600 6-Core Processor",
		"1", "AuthenticAMD", "AMD Ryzen 5 3600 6-Core Processor",
	}
	if got := DedupProcessorModels(realShape); !reflect.DeepEqual(got, []string{"AMD Ryzen 5 3600 6-Core Processor"}) {
		t.Errorf("dedupProcessorModels(realShape) = %v", got)
	}

	if got := DedupProcessorModels("not a list"); got != nil {
		t.Errorf("dedupProcessorModels(non-list) = %v, want nil", got)
	}

	if got := DedupProcessorModels([]interface{}{}); got != nil {
		t.Errorf("dedupProcessorModels(empty) = %v, want nil", got)
	}
}

func TestFormatRAM(t *testing.T) {
	if got := FormatRAM(float64(49152)); got != "48.0 GB" {
		t.Errorf("formatRAM(49152) = %q, want %q", got, "48.0 GB")
	}
	if got := FormatRAM("not a number"); got != "" {
		t.Errorf("formatRAM(non-number) = %q, want empty", got)
	}
}

func TestClassifyVirtualization(t *testing.T) {
	cases := []struct {
		name  string
		facts map[string]interface{}
		want  string
	}{
		{"bare metal (host role)", map[string]interface{}{"ansible_virtualization_role": "host"}, "Bare Metal"},
		{"bare metal (NA role)", map[string]interface{}{"ansible_virtualization_role": "NA"}, "Bare Metal"},
		{"guest lxc container", map[string]interface{}{"ansible_virtualization_role": "guest", "ansible_virtualization_type": "lxc"}, "Container"},
		{"guest kvm VM", map[string]interface{}{"ansible_virtualization_role": "guest", "ansible_virtualization_type": "kvm"}, "VM"},
		{
			"guest unknown type, tech_guest fallback to container",
			map[string]interface{}{
				"ansible_virtualization_role": "guest",
				"ansible_virtualization_type": "some-future-thing",
				"ansible_virtualization_tech_guest": []interface{}{
					"container", "docker",
				},
			},
			"Container",
		},
		{
			"guest, totally unrecognized type, no usable tech_guest",
			map[string]interface{}{"ansible_virtualization_role": "guest", "ansible_virtualization_type": "mystery-hypervisor"},
			"mystery-hypervisor",
		},
		{
			"guest, no type at all",
			map[string]interface{}{"ansible_virtualization_role": "guest"},
			"Guest (unknown type)",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ClassifyVirtualization(c.facts); got != c.want {
				t.Errorf("classifyVirtualization(%+v) = %q, want %q", c.facts, got, c.want)
			}
		})
	}
}

func TestFilterLinkLocal(t *testing.T) {
	in := []string{"2a01:4f9:3080:14ad::104", "fe80::be24:11ff:fea8:39c", "::1"}
	want := []string{"2a01:4f9:3080:14ad::104", "::1"}
	if got := FilterLinkLocal(in); !reflect.DeepEqual(got, want) {
		t.Errorf("filterLinkLocal() = %v, want %v", got, want)
	}
}

func TestHostKeyLines(t *testing.T) {
	facts := map[string]interface{}{
		"ansible_ssh_host_key_rsa_public":             "AAAARSA",
		"ansible_ssh_host_key_rsa_public_keytype":     "ssh-rsa",
		"ansible_ssh_host_key_ed25519_public":         "AAAAED",
		"ansible_ssh_host_key_ed25519_public_keytype": "ssh-ed25519",
		"ansible_ssh_host_key_ecdsa_public":           "AAAAECDSA",
		"ansible_ssh_host_key_ecdsa_public_keytype":   "ecdsa-sha2-nistp256",
	}
	got := HostKeyLines(facts)
	want := []HostKeyLine{
		{label: "Host key (ed25519):", value: "ssh-ed25519 AAAAED"},
		{label: "Host key (ecdsa):", value: "ecdsa-sha2-nistp256 AAAAECDSA"},
		{label: "Host key (rsa):", value: "ssh-rsa AAAARSA"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("hostKeyLines() = %+v, want %+v", got, want)
	}

	if got := HostKeyLines(map[string]interface{}{}); len(got) != 0 {
		t.Errorf("hostKeyLines(empty) = %+v, want empty", got)
	}
}

func TestFormatHostSummary(t *testing.T) {
	facts := map[string]interface{}{
		"ansible_fqdn":                                "web1.example.com",
		"ansible_system":                              "Linux",
		"ansible_kernel":                              "6.1.0-13-amd64",
		"ansible_distribution":                        "Debian",
		"ansible_distribution_version":                "13.6",
		"ansible_architecture":                        "x86_64",
		"ansible_processor":                           []interface{}{"0", "AuthenticAMD", "AMD Ryzen 5 3600 6-Core Processor"},
		"ansible_memtotal_mb":                         float64(49152),
		"ansible_virtualization_role":                 "guest",
		"ansible_virtualization_type":                 "lxc",
		"ansible_all_ipv4_addresses":                  []interface{}{"10.0.0.104"},
		"ansible_all_ipv6_addresses":                  []interface{}{"2a01:4f9:3080:14ad::104", "fe80::abc"},
		"ansible_ssh_host_key_ed25519_public":         "AAAAED",
		"ansible_ssh_host_key_ed25519_public_keytype": "ssh-ed25519",
	}
	got := formatCharacteristics("web1", facts)

	for _, want := range []string{
		"[green]Host:[-]           [lightsteelblue]web1[-]\n",
		"[green]FQDN:[-]           [lightsteelblue]web1.example.com[-]\n",
		"[green]OS:[-]             [lightsteelblue]Linux, 6.1.0-13-amd64[-]\n",
		"[green]Distribution:[-]   [lightsteelblue]Debian, 13.6[-]\n",
		"[green]Architecture:[-]   [lightsteelblue]x86_64[-]\n",
		"[green]Processor:[-]      [lightsteelblue]AMD Ryzen 5 3600 6-Core Processor[-]\n",
		"[green]RAM:[-]            [lightsteelblue]48.0 GB[-]\n",
		"[green]Virtualization:[-] [lightsteelblue]Container[-]\n",
		"[green]IPv4:[-]           [lightsteelblue]10.0.0.104[-]\n",
		"[green]IPv6:[-]           [lightsteelblue]2a01:4f9:3080:14ad::104[-]\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("formatCharacteristics() missing line %q\nfull output:\n%s", want, got)
		}
	}
	if got := formatHostKeyLines(HostKeyLines(facts)); !strings.Contains(got, "[green]Host key (ed25519):[-] [lightsteelblue]ssh-ed25519 AAAAED[-]\n") {
		t.Errorf("formatHostKeyLines(HostKeyLines(facts)) = %q, missing the ed25519 line", got)
	}
	if strings.Contains(got, "fe80::abc") {
		t.Errorf("formatCharacteristics() should have filtered the link-local IPv6 address, got:\n%s", got)
	}
}

func TestExtractInventoryDirs(t *testing.T) {
	dir := t.TempDir()
	invFile := filepath.Join(dir, "inventory.ini")
	if err := os.WriteFile(invFile, []byte("[all]\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		args []string
		want []string
	}{
		{"short flag with file", []string{"-i", invFile}, []string{dir}},
		{"long flag equals with dir", []string{"--inventory=" + dir}, []string{dir}},
		{"nonexistent path skipped", []string{"-i", filepath.Join(dir, "nope.ini")}, nil},
		{"unrelated flags ignored", []string{"-e", "x=1", "--tags", "foo"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ExtractInventoryDirs(c.args)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("extractInventoryDirs(%v) = %v, want %v", c.args, got, c.want)
			}
		})
	}
}

func TestDiscoverHostVarsFiles(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	if err := os.MkdirAll(filepath.Join(dir1, "host_vars"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir1, "host_vars", "web1.yml"), []byte("a: 1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	web2Dir := filepath.Join(dir2, "host_vars", "web2")
	if err := os.MkdirAll(web2Dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(web2Dir, "b.yaml"), []byte("b: 2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(web2Dir, "a.yml"), []byte("a: 1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	got1 := DiscoverHostVarsFiles("web1", []string{dir1, dir2})
	want1 := []string{filepath.Join(dir1, "host_vars", "web1.yml")}
	if !reflect.DeepEqual(got1, want1) {
		t.Errorf("discoverHostVarsFiles(web1) = %v, want %v", got1, want1)
	}

	got2 := DiscoverHostVarsFiles("web2", []string{dir1, dir2})
	want2 := []string{
		filepath.Join(web2Dir, "a.yml"),
		filepath.Join(web2Dir, "b.yaml"),
	}
	if !reflect.DeepEqual(got2, want2) {
		t.Errorf("discoverHostVarsFiles(web2) = %v, want %v", got2, want2)
	}

	// Same directory listed twice must not duplicate results.
	gotDup := DiscoverHostVarsFiles("web1", []string{dir1, dir1})
	if !reflect.DeepEqual(gotDup, want1) {
		t.Errorf("discoverHostVarsFiles(web1, dup dirs) = %v, want %v", gotDup, want1)
	}

	if got := DiscoverHostVarsFiles("nonexistent", []string{dir1, dir2}); len(got) != 0 {
		t.Errorf("discoverHostVarsFiles(nonexistent) = %v, want empty", got)
	}
}

func TestHostRowText(t *testing.T) {
	if got, want := HostRowText("web1", false, false, false), "[gray]●[-]  [white]web1[-]"; got != want {
		t.Errorf("HostRowText(selected=false) = %q, want %q", got, want)
	}
	if got, want := HostRowText("web1", true, false, false), "[gray]●[-]  ["+uikit.PureBlack+":lightgray:b]web1[-:-:-]"; got != want {
		t.Errorf("HostRowText(selected=true) = %q, want %q", got, want)
	}
}

// TestHostRowTextEscapesBrackets confirms a hostname containing a literal
// "[" (unusual, but not impossible - an inventory hostname is arbitrary
// user-authored text) doesn't get misread as a color tag.
func TestHostRowTextEscapesBrackets(t *testing.T) {
	got := HostRowText("host[1]", false, false, false)
	want := "[gray]●[-]  [white]" + tview.Escape("host[1]") + "[-]"
	if got != want {
		t.Errorf("HostRowText() = %q, want %q", got, want)
	}
}

// TestHostRowTextPingDot confirms the connectivity dot (design-docs/
// HostVerb.md's own "New ideas") leads the row (dot, then hostname),
// gray while pingKnown is false (that row's own ping hasn't landed yet)
// and colored per pingOK once it has - green for a successful
// ansible.builtin.ping, red otherwise.
func TestHostRowTextPingDot(t *testing.T) {
	if got, want := HostRowText("web1", false, false, true), "[gray]●[-]  [white]web1[-]"; got != want {
		t.Errorf("HostRowText(pingKnown=false) = %q, want %q (gray while pending)", got, want)
	}
	if got, want := HostRowText("web1", false, true, true), "[green]●[-]  [white]web1[-]"; got != want {
		t.Errorf("HostRowText(pingKnown=true, pingOK=true) = %q, want %q", got, want)
	}
	if got, want := HostRowText("web1", false, true, false), "[red]●[-]  [white]web1[-]"; got != want {
		t.Errorf("HostRowText(pingKnown=true, pingOK=false) = %q, want %q", got, want)
	}
}

// TestCollectRecentRunCandidates confirms design-docs/HostVerb.md's own
// "Recent" decisions: every PlaybookHistory entry contributes (not just
// one playbook), an invocation with no RunID is dropped before anything
// else, and the result is newest-first regardless of which entry (or
// which playbook/role) it came from.
func TestCollectRecentRunCandidates(t *testing.T) {
	cfg := config.StateConfig{
		History: []config.PlaybookHistory{
			{
				Playbook: "site.yml",
				Invocations: []config.InvocationRecord{
					{Time: "2026-09-07T11:11:00Z", RunID: "r1"},
					{Time: "2026-09-10T10:12:00Z", RunID: "r3"},
					{Time: "2026-09-09T00:00:00Z", RunID: ""}, // no RunID - dropped
				},
			},
			{
				Role: "postfix",
				Invocations: []config.InvocationRecord{
					{Time: "2026-09-08T01:23:00Z", RunID: "r2"},
				},
			},
		},
	}
	got := collectRecentRunCandidates(cfg)
	var gotRunIDs []string
	for _, c := range got {
		gotRunIDs = append(gotRunIDs, c.RunID)
	}
	want := []string{"r3", "r2", "r1"}
	if !reflect.DeepEqual(gotRunIDs, want) {
		t.Errorf("collectRecentRunCandidates() RunIDs = %v, want %v", gotRunIDs, want)
	}
	if got[1].name != "postfix" {
		t.Errorf("collectRecentRunCandidates()[1].name = %q, want %q (a role entry's own Role field)", got[1].name, "postfix")
	}
}

// TestCollectRecentRunCandidates_ScanCap confirms the RecentRunsScanLimit
// cap applies project-wide (across every PlaybookHistory entry pooled
// together), not per playbook - design-docs/HostVerb.md's own "Bounded
// scan, not bounded results" decision.
func TestCollectRecentRunCandidates_ScanCap(t *testing.T) {
	var invocations []config.InvocationRecord
	for i := 0; i < RecentRunsScanLimit+5; i++ {
		invocations = append(invocations, config.InvocationRecord{
			Time:  time.Date(2026, 1, 1, 0, 0, i, 0, time.UTC).Format(time.RFC3339),
			RunID: fmt.Sprintf("r%d", i),
		})
	}
	cfg := config.StateConfig{History: []config.PlaybookHistory{{Playbook: "site.yml", Invocations: invocations}}}
	got := collectRecentRunCandidates(cfg)
	if len(got) != RecentRunsScanLimit {
		t.Fatalf("collectRecentRunCandidates() returned %d candidates, want %d", len(got), RecentRunsScanLimit)
	}
	if got[0].RunID != fmt.Sprintf("r%d", RecentRunsScanLimit+4) {
		t.Errorf("collectRecentRunCandidates()[0].RunID = %q, want the single newest invocation", got[0].RunID)
	}
}

// TestHostCountsFor builds a minimal PlaybookState directly via Apply
// (the same public entry point a live/replayed run uses) and confirms
// hostCountsFor tallies outcomes/warnings/ignored per host, matching
// internal/session/recap.go's own recapForHost approach.
func TestHostCountsFor(t *testing.T) {
	s := &playbook.PlaybookState{}
	s.Apply(playbook.RawEvent{Event: "v2_playbook_on_play_start", Play: &playbook.PlayRef{Name: "p"}})
	s.Apply(playbook.RawEvent{Event: "v2_playbook_on_task_start", Task: &playbook.TaskRef{ID: "t1", Name: "one"}})
	s.Apply(playbook.RawEvent{Event: "v2_runner_on_ok", Task: &playbook.TaskRef{ID: "t1"},
		Hosts: map[string]json.RawMessage{"web1": json.RawMessage(`{"changed":false}`)}})
	s.Apply(playbook.RawEvent{Event: "v2_playbook_on_task_start", Task: &playbook.TaskRef{ID: "t2", Name: "two"}})
	s.Apply(playbook.RawEvent{Event: "v2_runner_on_failed", Task: &playbook.TaskRef{ID: "t2"},
		Hosts: map[string]json.RawMessage{"web1": json.RawMessage(`{"failed":true,"ignore_errors":true}`)}})

	counts, found := hostCountsFor(s, "web1")
	if !found {
		t.Fatal("hostCountsFor(state, \"web1\") found = false, want true")
	}
	if counts.OK != 1 || counts.Failed != 1 || counts.Ignored != 1 {
		t.Errorf("hostCountsFor() = %+v, want OK=1 Failed=1 Ignored=1", counts)
	}

	if _, found := hostCountsFor(s, "web2"); found {
		t.Error("hostCountsFor(state, \"web2\") found = true, want false (never appeared in this run)")
	}
}

func TestFormatRecentTime(t *testing.T) {
	if got, want := formatRecentTime("2026-09-10T10:12:00Z"), "2026-09-10 10:12"; got != want {
		t.Errorf("formatRecentTime() = %q, want %q", got, want)
	}
	// Falls back to the raw string on a parse failure - shouldn't happen
	// for anything this app itself ever wrote, but not trusted blindly.
	if got, want := formatRecentTime("not a timestamp"), "not a timestamp"; got != want {
		t.Errorf("formatRecentTime() = %q, want %q", got, want)
	}
}

func TestFormatRecentRunLines(t *testing.T) {
	got := formatRecentRunLines([]recentRunLine{
		{name: "site.yml", time: "2026-09-10T10:12:00Z", counts: hostRunCounts{OK: 189, Skipped: 50, Changed: 7}},
	})
	want := "[green]2026-09-10 10:12 site.yml:[-] [lightsteelblue]ok=189  skipped=50   changed=7    unreachable=0    failed=0    warnings=0    ignored=0[-]\n"
	if got != want {
		t.Errorf("formatRecentRunLines() = %q, want %q", got, want)
	}
}

// TestHighlightRolePrefix confirms the Plays tab's own cosmetic pass: a
// role-sourced task ("<role> : <task>", ansible's own --list-tasks
// format for a role task, confirmed empirically) gets just its role
// portion highlighted (bright white, bold), the rest dropped to silver;
// a plain play-level task (no " : " at all) renders unstyled but still
// escaped.
func TestHighlightRolePrefix(t *testing.T) {
	if got, want := highlightRolePrefix("webserver : install packages"), "[green]webserver[-][lightsteelblue] : install packages[-]"; got != want {
		t.Errorf("highlightRolePrefix() = %q, want %q", got, want)
	}
	if got, want := highlightRolePrefix("say hi"), "say hi"; got != want {
		t.Errorf("highlightRolePrefix() = %q, want %q", got, want)
	}
}

func TestHighlightYAMLKeys(t *testing.T) {
	got := highlightYAMLKeys("name: install packages\n  - foo: bar\n# a comment\n")
	want := "[green]name:[-][lightsteelblue] install packages[-]\n  - [green]foo:[-][lightsteelblue] bar[-]\n# a comment\n"
	if got != want {
		t.Errorf("highlightYAMLKeys() = %q, want %q", got, want)
	}
}

func TestHighlightJSONKeys(t *testing.T) {
	got := highlightJSONKeys(`{
    "ansible_host": "192.0.2.254",
    "nested": {
        "inner": 1
    }
}`)
	want := "{\n    [green]\"ansible_host\":[-][lightsteelblue] \"192.0.2.254\",[-]\n    [green]\"nested\":[-][lightsteelblue] {[-]\n        [green]\"inner\":[-][lightsteelblue] 1[-]\n    }\n}"
	if got != want {
		t.Errorf("highlightJSONKeys() = %q, want %q", got, want)
	}
}

// TestHighlightYAMLKeys_ListItems confirms a bare block-sequence item
// (no key of its own, e.g. "- alpha") gets its value highlighted too -
// previously only a list's own flow-style opening (attached to its
// owning key) was colored at all.
func TestHighlightYAMLKeys_ListItems(t *testing.T) {
	got := highlightYAMLKeys("mylist:\n  - alpha\n  - beta\n")
	want := "[green]mylist:[-][lightsteelblue][-]\n  - [lightsteelblue]alpha[-]\n  - [lightsteelblue]beta[-]\n"
	if got != want {
		t.Errorf("highlightYAMLKeys() = %q, want %q", got, want)
	}
}

// TestHighlightYAMLKeys_BlockScalar confirms an ansible-vault-shaped
// multi-line block scalar ("key: !vault |" followed by more-indented
// continuation lines) gets its whole value highlighted, not just the
// "!vault |" opener itself - and that a later, equal-or-lower-indented
// key correctly ends the continuation and resumes normal key/value
// highlighting.
func TestHighlightYAMLKeys_BlockScalar(t *testing.T) {
	got := highlightYAMLKeys("secret: !vault |\n  line one\n  line two\nplain: value\n")
	want := "[green]secret:[-][lightsteelblue] !vault |[-]\n" +
		"[lightsteelblue]  line one[-]\n" +
		"[lightsteelblue]  line two[-]\n" +
		"[green]plain:[-][lightsteelblue] value[-]\n"
	if got != want {
		t.Errorf("highlightYAMLKeys() = %q, want %q", got, want)
	}
}

// TestHighlightJSONKeys_ArrayElements confirms a bare array element (no
// key of its own) gets highlighted too - previously only an array's own
// opening "[" (part of its owning key's own value) was ever colored.
func TestHighlightJSONKeys_ArrayElements(t *testing.T) {
	got := highlightJSONKeys("{\n    \"tags\": [\n        \"web\",\n        \"prod\"\n    ]\n}")
	want := "{\n    [green]\"tags\":[-][lightsteelblue] [[-]\n        [lightsteelblue]\"web\"[-],\n        [lightsteelblue]\"prod\"[-]\n    ]\n}"
	if got != want {
		t.Errorf("highlightJSONKeys() = %q, want %q", got, want)
	}
}

func TestHostSectionHeading(t *testing.T) {
	if got, want := hostSectionHeading("Recent"), "[green]Recent\n======[-]\n\n"; got != want {
		t.Errorf("hostSectionHeading() = %q, want %q", got, want)
	}
	// A label containing a literal "[" (a host_vars file path could,
	// however unlikely) must not be misread as a color tag.
	if got, want := hostSectionHeading("a[b"), "[green]"+tview.Escape("a[b")+"\n"+strings.Repeat("=", 3)+"[-]\n\n"; got != want {
		t.Errorf("hostSectionHeading() = %q, want %q", got, want)
	}
}

func TestTabIndexByName(t *testing.T) {
	names := []string{"Summary", "Groups", "Plays", "host_vars", "Everything known"}
	cases := []struct {
		name string
		want int
	}{
		{"Summary", 0},
		{"Plays", 2},
		{"Everything known", 4},
		{"no such tab", 0}, // documented fallback, not an error
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := TabIndexByName(names, c.name); got != c.want {
				t.Errorf("TabIndexByName(names, %q) = %d, want %d", c.name, got, c.want)
			}
		})
	}
}
