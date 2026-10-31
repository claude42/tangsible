package ee

import (
	"sort"
	"strings"
)

var proxyVars = map[string]bool{
	"HTTP_PROXY": true, "HTTPS_PROXY": true, "NO_PROXY": true, "ALL_PROXY": true,
	"http_proxy": true, "https_proxy": true, "no_proxy": true, "all_proxy": true,
}

// hostPathy reports whether an ANSIBLE_* variable holds something that
// only makes sense on the host (a path, a config location, a plugin
// search path) and would point nowhere - or at the wrong thing - inside
// the container.
func hostPathy(name string) bool {
	if name == "ANSIBLE_INVENTORY" || name == "ANSIBLE_VAULT_IDENTITY_LIST" {
		return true
	}
	for _, frag := range []string{"PATH", "FILE", "CONFIG", "HOME", "DIR", "PLUGINS", "LIBRARY", "TMP"} {
		if strings.Contains(name, frag) {
			return true
		}
	}
	return false
}

// forwardEnv picks which host variables enter the container: proxies and
// ANSIBLE_* settings that aren't host paths, then extra (the caller's own
// additions, which win over host values). fixed holds the NAME=value
// settings the builder sets itself; those names are never forwarded.
// Both results are sorted by name.
func forwardEnv(host, extra, fixed []string) (env, dropped []string) {
	skip := map[string]bool{}
	for _, kv := range fixed {
		name, _, _ := strings.Cut(kv, "=")
		skip[name] = true
	}
	picked := map[string]string{}
	droppedSet := map[string]bool{}
	for _, kv := range host {
		name, val, ok := strings.Cut(kv, "=")
		if !ok || skip[name] {
			continue
		}
		switch {
		case proxyVars[name]:
			picked[name] = val
		case strings.HasPrefix(name, "ANSIBLE_"):
			if hostPathy(name) {
				droppedSet[name] = true
			} else {
				picked[name] = val
			}
		}
	}
	for _, kv := range extra {
		if name, val, ok := strings.Cut(kv, "="); ok && !skip[name] {
			picked[name] = val
			delete(droppedSet, name)
		}
	}
	for name, val := range picked {
		env = append(env, name+"="+val)
	}
	for name := range droppedSet {
		dropped = append(dropped, name)
	}
	sort.Strings(env)
	sort.Strings(dropped)
	return env, dropped
}
