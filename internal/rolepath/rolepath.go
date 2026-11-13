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

// Package rolepath answers "where does ansible look for roles?" beyond the
// project's own ./roles directory: $ANSIBLE_ROLES_PATH, ansible.cfg's
// roles_path, and ansible's built-in defaults (~/.ansible/roles,
// /usr/share/ansible/roles, /etc/ansible/roles) - and likewise for roles
// inside collections (the collections path, namespace.collection.role
// names). It asks `ansible-config` rather than re-implementing that
// cascade, and falls back to the built-in defaults when that isn't
// possible.
package rolepath

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"code.aw.net/claude/tangsible/internal/ansibleconfig"
)

// How `ansible-config dump` names the effective roles and collections paths.
const (
	configName            = "DEFAULT_ROLES_PATH"
	collectionsConfigName = "COLLECTIONS_PATHS"
)

var (
	dirsOnce sync.Once
	dirsVal  []string

	collDirsOnce sync.Once
	collDirsVal  []string
)

// Dirs returns the effective, absolute roles_path entries in ansible's own
// search order, resolved once per process (the cwd and environment don't
// change under a running tangsible). It does not include the
// playbook-adjacent ./roles directory, which ansible searches first and
// callers handle themselves.
func Dirs() []string {
	dirsOnce.Do(func() {
		dirsVal = fetchDirs()
	})
	return dirsVal
}

// SetDirs overrides Dirs' result (for tests in other packages, which can't
// feed ansible-config a fake roles path) and returns a func restoring the
// previous state.
func SetDirs(dirs []string) (restore func()) {
	dirsOnce.Do(func() {}) // a later Dirs() must not overwrite the override
	old := dirsVal
	dirsVal = dirs
	return func() { dirsVal = old }
}

// CollectionDirs returns the effective, absolute collections path entries
// (each a directory containing ansible_collections/), resolved once per
// process like Dirs. It does not include the playbook-adjacent
// ./collections directory, which ansible searches first and callers handle
// themselves.
func CollectionDirs() []string {
	collDirsOnce.Do(func() {
		collDirsVal = fetchDirsFor(collectionsConfigName, defaultCollectionDirs)
	})
	return collDirsVal
}

// SetCollectionDirs is SetDirs' counterpart for CollectionDirs.
func SetCollectionDirs(dirs []string) (restore func()) {
	collDirsOnce.Do(func() {})
	old := collDirsVal
	collDirsVal = dirs
	return func() { collDirsVal = old }
}

func fetchDirs() []string { return fetchDirsFor(configName, defaultDirs) }

func fetchDirsFor(name string, fallback func() []string) []string {
	// Once per process anyway, so any dump is recent enough.
	if v, ok := ansibleconfig.Find(ansibleconfig.Get(time.Hour), name); ok {
		if dirs := parseDirsValue(v); len(dirs) > 0 {
			return dirs
		}
	}
	return fallback()
}

// defaultCollectionDirs is ansible's own built-in collections path, used
// when ansible-config can't be asked.
func defaultCollectionDirs() []string {
	var dirs []string
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".ansible", "collections"))
	}
	return append(dirs, "/usr/share/ansible/collections")
}

// defaultDirs is ansible's own built-in roles_path, used when
// ansible-config can't be asked.
func defaultDirs() []string {
	var dirs []string
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".ansible", "roles"))
	}
	return append(dirs, "/usr/share/ansible/roles", "/etc/ansible/roles")
}

// parseDirs is fetchDirs' pure parsing half, split out so it's testable
// without a real ansible-config binary. Returns nil if the entry is
// missing or malformed. Relative entries are made absolute against the cwd.
func parseDirs(jsonOutput []byte) []string {
	var entries []ansibleconfig.Entry
	if err := json.Unmarshal(jsonOutput, &entries); err != nil {
		return nil
	}
	if v, ok := ansibleconfig.Find(entries, configName); ok {
		return parseDirsValue(v)
	}
	return nil
}

// parseDirsValue turns the DEFAULT_ROLES_PATH entry's raw value into
// absolute directories; nil if it's malformed.
func parseDirsValue(value json.RawMessage) []string {
	var raw []string
	if json.Unmarshal(value, &raw) != nil {
		return nil
	}
	var dirs []string
	for _, d := range raw {
		if d = strings.TrimSpace(d); d == "" {
			continue
		}
		if abs, err := filepath.Abs(d); err == nil {
			dirs = append(dirs, abs)
		}
	}
	return dirs
}

// Find returns the directory of the role called name: <base>/roles/<name>
// first (ansible's playbook-adjacent lookup; base is typically the cwd or
// the playbook's directory), then each of dirs in order, and last <base>/
// <name> itself, ansible's final fallback. "" if none exists.
func Find(name, base string, dirs []string) string {
	if name == "" || strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
		return ""
	}
	candidates := append([]string{filepath.Join(base, "roles")}, dirs...)
	candidates = append(candidates, base)
	for _, d := range candidates {
		p := filepath.Join(d, name)
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			return p
		}
	}
	return ""
}

// SplitFQCN splits a fully qualified role name "namespace.collection.role"
// into its parts; ok is false for anything else - a bare name, or a legacy
// Galaxy-style "namespace.role" directory name, which stays a plain
// roles_path lookup.
func SplitFQCN(name string) (namespace, collection, role string, ok bool) {
	parts := strings.Split(name, ".")
	if len(parts) != 3 {
		return "", "", "", false
	}
	for _, p := range parts {
		if p == "" || strings.ContainsAny(p, `/\`) {
			return "", "", "", false
		}
	}
	return parts[0], parts[1], parts[2], true
}

// FindInCollection returns the directory of the role in the collection
// "namespace.collection": <base>/collections first (ansible's playbook-
// adjacent lookup, like roles/ for plain roles), then each of dirs, each
// holding ansible_collections/<ns>/<collection>/roles/<role>. "" if none.
func FindInCollection(namespace, collection, role, base string, dirs []string) string {
	candidates := append([]string{filepath.Join(base, "collections")}, dirs...)
	for _, d := range candidates {
		p := filepath.Join(d, "ansible_collections", namespace, collection, "roles", role)
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			return p
		}
	}
	return ""
}

// Lookup resolves a role name the way ansible does: a fully qualified name
// is looked up in its collection; a bare one in each of the play's
// collections: list (collections) in order; then, for everything that
// didn't resolve, as a plain role (Find). It returns the role's directory
// and the "namespace.collection" it came from ("" for a plain role), or
// "" for both if nothing was found. Uses the effective roles and
// collections paths.
func Lookup(name string, collections []string, base string) (dir, collection string) {
	if ns, coll, role, ok := SplitFQCN(name); ok {
		if d := FindInCollection(ns, coll, role, base, CollectionDirs()); d != "" {
			return d, ns + "." + coll
		}
	} else if !strings.ContainsAny(name, `/\`) && name != "." && name != ".." {
		for _, c := range collections {
			if ns, coll, ok := strings.Cut(c, "."); ok && !strings.Contains(coll, ".") {
				if d := FindInCollection(ns, coll, name, base, CollectionDirs()); d != "" {
					return d, c
				}
			}
		}
	}
	return Find(name, base, Dirs()), ""
}

// FromPath returns the directory and name of the role that path (absolute
// or relative) lies inside, judged by it being under "<dir>/<name>/" for
// one of dirs - which, unlike a "/roles/<name>/" pattern, also covers a
// roles_path directory that isn't called "roles". "" for both if path is
// under none of them.
func FromPath(path string, dirs []string) (dir, name string) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", ""
	}
	for _, d := range dirs {
		rel, err := filepath.Rel(d, abs)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		name, _, found := strings.Cut(rel, string(filepath.Separator))
		if !found {
			continue // a file directly in d, not inside a role
		}
		return filepath.Join(d, name), name
	}
	return "", ""
}
