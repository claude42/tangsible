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

package config

import (
	"fmt"
	"io"
	"strings"
)

// VerbInfo is the one place a verb's synopsis and one-line summary live:
// the `help` verb, the top-level usage error and every verb's own "usage:"
// error all read it. Summary and Synopsis mirror the NAME and SYNOPSIS
// sections of man/tangsible-<verb>.1.scd (markup stripped) - help_test.go
// fails when they drift apart.
type VerbInfo struct {
	Verb     Verb
	Synopsis string
	Summary  string
}

// VerbHelpTable lists every verb in the order help prints them.
var VerbHelpTable = []VerbInfo{
	{VerbRun, "tangsible run [playbook.yml] [--start-at-play name] [--dialog | --no-dialog] [ansible-playbook args ...]", "run a playbook with a live, navigable tree UI"},
	{VerbRerun, "tangsible rerun [playbook.yml or role name] [--start-at-play name] [--dialog | --no-dialog] [ansible-playbook args ...]", "repeat the last playbook run"},
	{VerbRole, "tangsible role role_name [--dialog | --no-dialog] [ansible-playbook args ...]", "run a single role in isolation"},
	{VerbTemplate, "tangsible template path [hostSpec] [ansible-playbook args ...]", "debug a Jinja2 template against real inventory hosts"},
	{VerbHost, "tangsible host hostname [playbook.yml] [ansible-playbook args ...]", "everything Tangsible can determine about one host"},
	{VerbHosts, "tangsible hosts [playbook.yml] [ansible-playbook args ...]", "browse every inventory host"},
	{VerbInventory, "tangsible inventory [playbook.yml] [ansible-playbook args ...]", "browse the whole inventory as a tree"},
	{VerbRevisit, "tangsible revisit [playbook.yml or role name] [--tags | -t tags] [--limit | -l hosts]", "browse and reopen previous runs"},
	{VerbVault, "tangsible vault filename [--vault-password-file path | --ask-vault-pass]", "edit individually vault-encrypted variables in place"},
	{VerbDebug, "tangsible debug [playbook.yml] [--all-vars|-a] [ansible-playbook args ...]", "step through a playbook, set breakpoints, watch variables"},
	{VerbVersion, "tangsible version", "print version information about tangsible and the components it uses"},
	{VerbHelp, "tangsible help [verb]", "list all verbs, or show one verb's synopsis"},
}

// LookupVerbHelp returns the table entry for v.
func LookupVerbHelp(v Verb) (VerbInfo, bool) {
	for _, h := range VerbHelpTable {
		if h.Verb == v {
			return h, true
		}
	}
	return VerbInfo{}, false
}

// UsageLine is the "usage: ..." line a verb prints when given bad arguments.
func UsageLine(v Verb) string {
	if h, ok := LookupVerbHelp(v); ok {
		return "usage: " + h.Synopsis
	}
	return "usage: tangsible " + string(v)
}

// IsHelpFlag reports whether the first argument asks for top-level help.
// Only args[0] counts: after a verb, -h/--help belong to ansible-playbook.
func IsHelpFlag(arg string) bool { return arg == "-h" || arg == "--help" }

// WriteHelp prints help for the verb named by topic, or a one-line-per-verb
// overview when topic is empty. It returns false (printing nothing) for an unknown topic.
func WriteHelp(w io.Writer, topic string) bool {
	if topic != "" {
		h, ok := LookupVerbHelp(Verb(topic))
		if !ok {
			return false
		}
		fmt.Fprintf(w, "%s\n\n    %s\n\nSee also: man tangsible-%s\n", h.Synopsis, h.Summary, h.Verb)
		return true
	}
	fmt.Fprint(w, "tangsible - a live terminal UI for ansible-playbook\n\n")
	width := 0
	for _, h := range VerbHelpTable {
		width = max(width, len(h.Verb))
	}
	for _, h := range VerbHelpTable {
		fmt.Fprintf(w, "tangsible %-*s  %s\n", width, h.Verb, h.Summary)
	}
	fmt.Fprintln(w, "\nSynopsis of one verb: tangsible help <verb>")
	return true
}

// VerbNames is the verb list for the top-level usage error, e.g. "run|rerun|...".
func VerbNames() string {
	names := make([]string, len(VerbHelpTable))
	for i, h := range VerbHelpTable {
		names[i] = string(h.Verb)
	}
	return strings.Join(names, "|")
}
