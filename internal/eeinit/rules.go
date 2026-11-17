package eeinit

import (
	"sort"
	"strings"
)

// Rule says that using a plugin needs packages its collection doesn't
// declare (ansible-builder already installs each collection's own
// requirements.txt and bindep.txt, so this table only holds the gaps). Add a
// rule once it is checked against the plugin's documentation.
type Rule struct {
	// Plugin is the fully qualified name; Kind "module" or "lookup".
	Plugin, Kind string
	// Pip are Python requirements (pip syntax), System system packages
	// (bindep syntax).
	Pip, System []string
	// Why is one line, shown after the entry.
	Why string
	// ControllerOnly limits a module rule to tasks that run on the
	// controller: the module's own packages are otherwise its target's
	// business. A lookup always runs on the controller, so it is ignored
	// there, and so is it for a module whose action plugin runs on the
	// controller whatever the host (synchronize).
	ControllerOnly bool
}

// Rules is the embedded table (design-docs/EEInit.md, "The rules table").
var Rules = []Rule{
	{Plugin: "community.general.dig", Kind: "lookup", Pip: []string{"dnspython"}, Why: "the dig lookup needs dnspython"},
	{Plugin: "community.hashi_vault.hashi_vault", Kind: "lookup", Pip: []string{"hvac"}, Why: "the hashi_vault lookup needs hvac"},
	{Plugin: "ansible.posix.synchronize", Kind: "module", System: []string{"rsync"}, Why: "synchronize runs rsync on the controller"},
}

// StanzaEntry is one line of a commented stanza: the package and the comment
// that follows it.
type StanzaEntry struct {
	Package, Comment string
}

// Implied are the packages the rules ask for, by kind of package.
type Implied struct {
	Pip, System []StanzaEntry
}

// ImpliedPackages applies Rules to the plugins the playbook uses. A package
// asked for by several is listed once, with all the evidence.
func ImpliedPackages(plugins []Plugin) Implied {
	type key struct{ kind, fqcn string }
	locs := map[key][]string{}
	label := map[key]string{}
	for _, p := range plugins {
		k := key{p.Kind, p.FQCN}
		if rule, ok := ruleFor(k.kind, k.fqcn); !ok || (rule.ControllerOnly && !p.Controller) {
			continue
		}
		if label[k] == "" {
			label[k] = p.Label
			if p.Kind == "lookup" {
				label[k] = "lookup " + p.Label
			}
		}
		if !contains(locs[k], p.Loc) {
			locs[k] = append(locs[k], p.Loc)
		}
	}
	pip, sys := map[string][]string{}, map[string][]string{}
	for k, l := range locs {
		rule, _ := ruleFor(k.kind, k.fqcn)
		comment := label[k] + " (" + locsText(l) + "): " + rule.Why
		for _, p := range rule.Pip {
			pip[p] = append(pip[p], comment)
		}
		for _, p := range rule.System {
			sys[p] = append(sys[p], comment)
		}
	}
	return Implied{Pip: entries(pip), System: entries(sys)}
}

func ruleFor(kind, fqcn string) (Rule, bool) {
	for _, r := range Rules {
		if r.Kind == kind && r.Plugin == fqcn {
			return r, true
		}
	}
	return Rule{}, false
}

// entries turns package -> comments into sorted entries.
func entries(m map[string][]string) []StanzaEntry {
	var out []StanzaEntry
	for pkg, comments := range m {
		sort.Strings(comments)
		out = append(out, StanzaEntry{pkg, strings.Join(comments, "; ")})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Package < out[j].Package })
	return out
}
