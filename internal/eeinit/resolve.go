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
	var out []string
	for _, c := range u.Collections {
		out = append(out, c+"."+u.Module)
	}
	return append(out, u.Module)
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
