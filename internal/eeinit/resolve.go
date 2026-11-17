package eeinit

import (
	"fmt"
	"sort"
	"strings"
)

// Item is something that needs a collection: a module (Label "docker_container")
// or a role (Label "role ns.coll.web"), with where it was found.
type Item struct {
	Label string
	Locs  []string
}

// Needed is one collection the playbook needs and why.
type Needed struct {
	Collection string
	Items      []Item
	// Hints are documented requirements of the plugins that need it
	// ("dig: dnspython (python library)"), shown as comments only.
	Hints []string
}

// RoleUse is a role that came from a collection.
type RoleUse struct {
	Role, Collection string
}

// builtinCollections ship with ansible-core and never go into the image.
var builtinCollections = map[string]bool{"ansible.builtin": true, "ansible.legacy": true}

// fqcnCollection returns the "namespace.collection" of a fully qualified
// module name (three or more dot-separated parts).
func fqcnCollection(name string) (string, bool) {
	parts := strings.Split(name, ".")
	if len(parts) < 3 {
		return "", false
	}
	return parts[0] + "." + parts[1], true
}

// Candidates lists the names to ask the host about for a module that is
// not fully qualified, in the order ansible would try them: the
// collections: search list first, then the bare name (ansible-core's own
// redirects, and any collection installed). A fully qualified name needs
// no lookup.
func Candidates(u ModuleUse) []string {
	if _, ok := fqcnCollection(u.Module); ok {
		return nil
	}
	return searchOrder(u.Module, u.Collections)
}

// searchOrder is name qualified with each of colls in turn, then bare.
func searchOrder(name string, colls []string) []string {
	var out []string
	for _, c := range colls {
		out = append(out, c+"."+name)
	}
	return append(out, name)
}

// UniqueCandidates is every name worth asking the host about.
func UniqueCandidates(uses []ModuleUse) []string {
	seen := map[string]bool{}
	var out []string
	for _, u := range uses {
		for _, c := range Candidates(u) {
			if !seen[c] {
				seen[c] = true
				out = append(out, c)
			}
		}
	}
	sort.Strings(out)
	return out
}

// Resolve maps module uses (and the collection roles) to the collections
// they need. found is the host's answer for each candidate name (name ->
// owning collection; absent when the host doesn't know it). Modules the host
// can't place become Todos; ansible.builtin ones are dropped.
func Resolve(uses []ModuleUse, roles []RoleUse, found map[string]string) (needed []Needed, todos []Todo) {
	byColl := map[string]map[string][]string{} // collection -> label -> locs
	add := func(coll, label, loc string) {
		if builtinCollections[coll] {
			return
		}
		if byColl[coll] == nil {
			byColl[coll] = map[string][]string{}
		}
		locs, seen := byColl[coll][label]
		switch {
		case loc != "" && !contains(locs, loc):
			byColl[coll][label] = append(locs, loc)
		case !seen:
			byColl[coll][label] = nil
		}
	}
	for _, u := range uses {
		if coll, ok := fqcnCollection(u.Module); ok {
			add(coll, u.Module, u.Loc)
			continue
		}
		coll := ""
		for _, c := range Candidates(u) {
			if coll = found[c]; coll != "" {
				break
			}
		}
		if coll == "" {
			todos = append(todos, Todo{Loc: u.Loc, What: fmt.Sprintf("module %q: no collection found on this host", u.Module)})
			continue
		}
		add(coll, u.Module, u.Loc)
	}
	for _, r := range roles {
		add(r.Collection, "role "+r.Role, "")
	}
	for coll, items := range byColl {
		n := Needed{Collection: coll}
		for label, locs := range items {
			n.Items = append(n.Items, Item{label, locs})
		}
		sort.Slice(n.Items, func(i, j int) bool { return n.Items[i].Label < n.Items[j].Label })
		needed = append(needed, n)
	}
	sort.Slice(needed, func(i, j int) bool { return needed[i].Collection < needed[j].Collection })
	return needed, todos
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// Plugin is a module or lookup the playbook uses, resolved to its fully
// qualified name; the implied-dependency rules are matched against these.
type Plugin struct {
	Kind string // "module" or "lookup"
	FQCN string
	// Label is the name as written, Loc where it was found.
	Label, Loc string
	// Controller is true when it runs on the controller: always for a
	// lookup, for a module when its task does.
	Controller bool
}

// ModulePlugins qualifies the module uses (builtin ones included, since a
// rule may apply to one) that the host could place; found maps a candidate
// name to its collection.
func ModulePlugins(uses []ModuleUse, found map[string]string) []Plugin {
	var out []Plugin
	for _, u := range uses {
		fqcn := u.Module
		if _, ok := fqcnCollection(u.Module); !ok {
			fqcn = ""
			for _, c := range Candidates(u) {
				if coll := found[c]; coll != "" {
					fqcn = coll + "." + u.Module
					break
				}
			}
		}
		if fqcn != "" {
			out = append(out, Plugin{Kind: "module", FQCN: fqcn, Label: u.Module, Loc: u.Loc, Controller: u.Controller})
		}
	}
	return out
}

// LookupCandidates is every name worth asking the host's ansible-doc about
// for the lookups: the search-list candidates of a short name, and a
// qualified name itself (for its documented requirements).
func LookupCandidates(uses []LookupUse) []string {
	seen := map[string]bool{}
	var out []string
	for _, u := range uses {
		names := []string{u.Name}
		if _, ok := fqcnCollection(u.Name); !ok {
			names = searchOrder(u.Name, u.Collections)
		}
		for _, n := range names {
			if !seen[n] {
				seen[n] = true
				out = append(out, n)
			}
		}
	}
	sort.Strings(out)
	return out
}

// ResolveLookups maps the lookups to the collections they need (like Resolve
// does for modules; ansible.builtin ones are dropped), with the documented
// requirements of each as hints, the fully qualified plugins for the rules,
// and a Todo for each the host can't place.
func ResolveLookups(uses []LookupUse, found map[string]PluginDoc) (needed []Needed, plugins []Plugin, todos []Todo) {
	byColl := map[string]map[string][]string{}
	// hints: collection -> plugin (fully qualified, so dig and
	// community.general.dig give one hint) -> the hint text.
	hints := map[string]map[string]string{}
	for _, u := range uses {
		coll, fqcn, doc := "", u.Name, found[u.Name]
		if c, ok := fqcnCollection(u.Name); ok {
			coll = c
		} else {
			for _, cand := range searchOrder(u.Name, u.Collections) {
				if d, ok := found[cand]; ok {
					coll, doc = d.Collection, d
					fqcn = coll + "." + u.Name
					break
				}
			}
		}
		if coll == "" {
			todos = append(todos, Todo{Loc: u.Loc, What: fmt.Sprintf("lookup %q: no collection found on this host", u.Name)})
			continue
		}
		plugins = append(plugins, Plugin{Kind: "lookup", FQCN: fqcn, Label: u.Name, Loc: u.Loc, Controller: true})
		if builtinCollections[coll] {
			continue
		}
		label := "lookup " + u.Name
		if byColl[coll] == nil {
			byColl[coll] = map[string][]string{}
			hints[coll] = map[string]string{}
		}
		if !contains(byColl[coll][label], u.Loc) {
			byColl[coll][label] = append(byColl[coll][label], u.Loc)
		}
		if _, seen := hints[coll][fqcn]; !seen && len(doc.Requirements) > 0 {
			hints[coll][fqcn] = u.Name + ": " + strings.Join(doc.Requirements, "; ")
		}
	}
	for coll, items := range byColl {
		n := Needed{Collection: coll}
		for label, locs := range items {
			n.Items = append(n.Items, Item{label, locs})
		}
		sort.Slice(n.Items, func(i, j int) bool { return n.Items[i].Label < n.Items[j].Label })
		for _, h := range hints[coll] {
			n.Hints = append(n.Hints, h)
		}
		sort.Strings(n.Hints)
		needed = append(needed, n)
	}
	sort.Slice(needed, func(i, j int) bool { return needed[i].Collection < needed[j].Collection })
	return needed, plugins, todos
}

// MergeNeeded combines two lists of needed collections: items and hints of
// the same collection are joined.
func MergeNeeded(a, b []Needed) []Needed {
	by := map[string]*Needed{}
	var order []string
	for _, list := range [][]Needed{a, b} {
		for _, n := range list {
			m, ok := by[n.Collection]
			if !ok {
				by[n.Collection] = &Needed{Collection: n.Collection}
				m = by[n.Collection]
				order = append(order, n.Collection)
			}
			m.Items = append(m.Items, n.Items...)
			for _, h := range n.Hints {
				if !contains(m.Hints, h) {
					m.Hints = append(m.Hints, h)
				}
			}
		}
	}
	sort.Strings(order)
	out := make([]Needed, 0, len(order))
	for _, c := range order {
		m := by[c]
		sort.SliceStable(m.Items, func(i, j int) bool { return m.Items[i].Label < m.Items[j].Label })
		sort.Strings(m.Hints)
		out = append(out, *m)
	}
	return out
}

// CheckNames are the names to ask an image's ansible-doc about to see
// whether a module use resolves there: the use itself if it is fully
// qualified, otherwise Candidates.
func CheckNames(u ModuleUse) []string {
	if _, ok := fqcnCollection(u.Module); ok {
		return []string{u.Module}
	}
	return Candidates(u)
}

// LookupCheckNames is CheckNames for a lookup.
func LookupCheckNames(u LookupUse) []string {
	if _, ok := fqcnCollection(u.Name); ok {
		return []string{u.Name}
	}
	return searchOrder(u.Name, u.Collections)
}
