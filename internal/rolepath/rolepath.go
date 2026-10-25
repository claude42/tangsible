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
// /usr/share/ansible/roles, /etc/ansible/roles). It asks `ansible-config`
// rather than re-implementing that cascade, and falls back to the built-in
// defaults when that isn't possible.
package rolepath

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// configName is how `ansible-config dump` names the effective roles path.
const configName = "DEFAULT_ROLES_PATH"

var (
	dirsOnce sync.Once
	dirsVal  []string
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

func fetchDirs() []string {
	if out, err := exec.Command("ansible-config", "dump", "--format", "json").Output(); err == nil {
		if dirs := parseDirs(out); len(dirs) > 0 {
			return dirs
		}
	}
	return defaultDirs()
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
	var entries []struct {
		Name  string          `json:"name"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(jsonOutput, &entries); err != nil {
		return nil
	}
	for _, e := range entries {
		if e.Name != configName {
			continue
		}
		var raw []string
		if json.Unmarshal(e.Value, &raw) != nil {
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
	return nil
}

// Find returns the directory of the role called name: <base>/roles/<name>
// first (ansible's playbook-adjacent lookup; base is typically the cwd or
// the playbook's directory), then each of dirs in order. "" if none exists.
func Find(name, base string, dirs []string) string {
	if name == "" || strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
		return ""
	}
	candidates := append([]string{filepath.Join(base, "roles")}, dirs...)
	for _, d := range candidates {
		p := filepath.Join(d, name)
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			return p
		}
	}
	return ""
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
