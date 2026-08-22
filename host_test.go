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

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
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
			host, playbook, rest, ok := parseHostArgs(c.args)
			if ok != c.wantOK || host != c.wantHost || playbook != c.wantPlaybook || !reflect.DeepEqual(rest, c.wantRest) {
				t.Errorf("parseHostArgs(%v) = (%q, %q, %v, %v), want (%q, %q, %v, %v)",
					c.args, host, playbook, rest, ok, c.wantHost, c.wantPlaybook, c.wantRest, c.wantOK)
			}
		})
	}
}

func inventoryGroupJSON(t *testing.T, hosts, children []string) json.RawMessage {
	t.Helper()
	g := ansibleInventoryGroup{Hosts: hosts, Children: children}
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

	got := hostGroupChain(raw, "web1")
	want := []groupMembership{
		{Group: "web"},
		{Group: "prod", Via: "web"},
		{Group: "all", Via: "prod"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("hostGroupChain() = %+v, want %+v", got, want)
	}

	if got := hostGroupChain(raw, "nonexistent"); len(got) != 0 {
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
	got := hostGroupChain(raw, "combo1")
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
	if got := dedupProcessorModels(realShape); !reflect.DeepEqual(got, []string{"AMD Ryzen 5 3600 6-Core Processor"}) {
		t.Errorf("dedupProcessorModels(realShape) = %v", got)
	}

	if got := dedupProcessorModels("not a list"); got != nil {
		t.Errorf("dedupProcessorModels(non-list) = %v, want nil", got)
	}

	if got := dedupProcessorModels([]interface{}{}); got != nil {
		t.Errorf("dedupProcessorModels(empty) = %v, want nil", got)
	}
}

func TestFormatRAM(t *testing.T) {
	if got := formatRAM(float64(49152)); got != "48.0 GB" {
		t.Errorf("formatRAM(49152) = %q, want %q", got, "48.0 GB")
	}
	if got := formatRAM("not a number"); got != "" {
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
			if got := classifyVirtualization(c.facts); got != c.want {
				t.Errorf("classifyVirtualization(%+v) = %q, want %q", c.facts, got, c.want)
			}
		})
	}
}

func TestFilterLinkLocal(t *testing.T) {
	in := []string{"2a01:4f9:3080:14ad::104", "fe80::be24:11ff:fea8:39c", "::1"}
	want := []string{"2a01:4f9:3080:14ad::104", "::1"}
	if got := filterLinkLocal(in); !reflect.DeepEqual(got, want) {
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
	got := hostKeyLines(facts)
	want := []hostKeyLine{
		{label: "Host key (ed25519):", value: "ssh-ed25519 AAAAED"},
		{label: "Host key (ecdsa):", value: "ecdsa-sha2-nistp256 AAAAECDSA"},
		{label: "Host key (rsa):", value: "ssh-rsa AAAARSA"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("hostKeyLines() = %+v, want %+v", got, want)
	}

	if got := hostKeyLines(map[string]interface{}{}); len(got) != 0 {
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
	got := formatHostSummary("web1", facts)

	for _, want := range []string{
		"Host:           web1\n",
		"FQDN:           web1.example.com\n",
		"OS:             Linux, 6.1.0-13-amd64\n",
		"Distribution:   Debian, 13.6\n",
		"Architecture:   x86_64\n",
		"Processor:      AMD Ryzen 5 3600 6-Core Processor\n",
		"RAM:            48.0 GB\n",
		"Virtualization: Container\n",
		"IPv4:           10.0.0.104\n",
		"IPv6:           2a01:4f9:3080:14ad::104\n",
		"Host key (ed25519): ssh-ed25519 AAAAED\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("formatHostSummary() missing line %q\nfull output:\n%s", want, got)
		}
	}
	if strings.Contains(got, "fe80::abc") {
		t.Errorf("formatHostSummary() should have filtered the link-local IPv6 address, got:\n%s", got)
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
			got := extractInventoryDirs(c.args)
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

	got1 := discoverHostVarsFiles("web1", []string{dir1, dir2})
	want1 := []string{filepath.Join(dir1, "host_vars", "web1.yml")}
	if !reflect.DeepEqual(got1, want1) {
		t.Errorf("discoverHostVarsFiles(web1) = %v, want %v", got1, want1)
	}

	got2 := discoverHostVarsFiles("web2", []string{dir1, dir2})
	want2 := []string{
		filepath.Join(web2Dir, "a.yml"),
		filepath.Join(web2Dir, "b.yaml"),
	}
	if !reflect.DeepEqual(got2, want2) {
		t.Errorf("discoverHostVarsFiles(web2) = %v, want %v", got2, want2)
	}

	// Same directory listed twice must not duplicate results.
	gotDup := discoverHostVarsFiles("web1", []string{dir1, dir1})
	if !reflect.DeepEqual(gotDup, want1) {
		t.Errorf("discoverHostVarsFiles(web1, dup dirs) = %v, want %v", gotDup, want1)
	}

	if got := discoverHostVarsFiles("nonexistent", []string{dir1, dir2}); len(got) != 0 {
		t.Errorf("discoverHostVarsFiles(nonexistent) = %v, want empty", got)
	}
}
