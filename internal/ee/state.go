package ee

import (
	"path/filepath"
	"strings"
)

// StateSetting is one effective ansible setting as `ansible-config dump`
// reports it: its value (empty when unset) and where that came from
// ("default" when nobody configured it).
type StateSetting struct {
	Value  string
	Origin string
}

// StatePath is a place ansible writes to outside the project - the fact
// cache directory or the log file - that a container run must reach at its
// host path, or the run re-gathers facts every time and its log is lost.
type StatePath struct {
	Path string
	Dir  bool   // a directory (the fact cache); otherwise a file (the log)
	Env  string // the ANSIBLE_* variable that hands the path to the container
}

// factCachePlugins are the cache plugins that keep their data in a
// directory on disk (redis, memcached and the like name no path).
var factCachePlugins = map[string]bool{
	"jsonfile": true, "yaml": true, "pickle": true,
	"ansible.builtin.jsonfile": true, "ansible.builtin.yaml": true, "ansible.builtin.pickle": true,
}

// StatePaths picks, from the effective settings (keys are the names the dump
// uses: CACHE_PLUGIN, CACHE_PLUGIN_CONNECTION, DEFAULT_LOG_PATH), the paths
// that were configured and lie outside cwd, which is mounted anyway. Paths
// are made absolute against cwd and expanded like any other path option.
// plugin is the cache plugin in effect, for the caller's environment (the
// container must use the same one).
func StatePaths(settings map[string]StateSetting, cwd, home string, hostEnv []string) (paths []StatePath, plugin string) {
	env := envMap(hostEnv)
	outside := func(p string) bool {
		p = filepath.Clean(p)
		return p != cwd && !strings.HasPrefix(p, filepath.Clean(cwd)+string(filepath.Separator))
	}
	resolve := func(v string) string {
		v = expandPath(v, home, env)
		if v == "" {
			return ""
		}
		return absPath(cwd, v)
	}
	configured := func(name string) (string, bool) {
		s, ok := settings[name]
		if !ok || s.Value == "" || s.Origin == "" || s.Origin == "default" {
			return "", false
		}
		return s.Value, true
	}
	if pl, ok := configured("CACHE_PLUGIN"); ok && factCachePlugins[pl] {
		if conn, ok := configured("CACHE_PLUGIN_CONNECTION"); ok {
			if p := resolve(conn); p != "" && outside(p) {
				paths = append(paths, StatePath{Path: p, Dir: true, Env: "ANSIBLE_CACHE_PLUGIN_CONNECTION"})
				plugin = pl
			}
		}
	}
	if v, ok := configured("DEFAULT_LOG_PATH"); ok {
		if p := resolve(v); p != "" && outside(p) {
			paths = append(paths, StatePath{Path: p, Env: "ANSIBLE_LOG_PATH"})
		}
	}
	return paths, plugin
}
