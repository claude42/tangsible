package ee

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// passthroughVars are non-ANSIBLE_ host variables that still shape what
// ansible does inside the container: proxies and the no-color preference.
var passthroughVars = map[string]bool{
	"NO_COLOR":   true,
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
	// Paths on the managed host, not on this one.
	if name == "ANSIBLE_REMOTE_TEMP" || name == "ANSIBLE_FACT_PATH" {
		return false
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
func forwardEnv(host, extra, fixed []string, pass []string, set map[string]string) (env, dropped []string) {
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
		case passthroughVars[name]:
			picked[name] = val
		case strings.HasPrefix(name, "ANSIBLE_"):
			if hostPathy(name) {
				droppedSet[name] = true
			} else {
				picked[name] = val
			}
		}
	}
	// What the user asked for (pass_env, set_env) beats the automatic
	// filter - a path-like ANSIBLE_* variable they name is passed, they know
	// what it means - but never a variable tangsible sets itself.
	for _, kv := range host {
		name, val, ok := strings.Cut(kv, "=")
		if ok && !skip[name] && !isReservedEnv(name) && matchesAnyEnvPattern(pass, name) {
			picked[name] = val
			delete(droppedSet, name)
		}
	}
	for name, val := range set {
		if !skip[name] && !isReservedEnv(name) {
			picked[name] = val
			delete(droppedSet, name)
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

// ReservedEnv are variables tangsible sets itself for every container; they
// cannot be passed or set.
var ReservedEnv = []string{
	"ANSIBLE_CALLBACK_PLUGINS", "ANSIBLE_JSON_INDENT", "HOME", "SSH_AUTH_SOCK",
	"TANGSIBLE_EVENT_FD", "TANGSIBLE_EVENT_FILE",
}

var envNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// envPatternMatches: a pass_env entry is a variable name, or a name prefix
// with a trailing * ("AWS_*").
func envPatternMatches(pattern, name string) bool {
	if prefix, ok := strings.CutSuffix(pattern, "*"); ok {
		return prefix != "" && strings.HasPrefix(name, prefix)
	}
	return pattern == name
}

func matchesAnyEnvPattern(patterns []string, name string) bool {
	for _, p := range patterns {
		if envPatternMatches(p, name) {
			return true
		}
	}
	return false
}

func isReservedEnv(name string) bool {
	for _, r := range ReservedEnv {
		if r == name {
			return true
		}
	}
	return false
}

// CheckEnv validates pass_env and set_env: well-formed names, nothing
// tangsible manages itself, no whole-environment pattern. Every problem is
// reported (one line each); warnings name entries that pass nothing
// (a variable that isn't set, a prefix matching no variable) - ansible-navigator
// warns about the former too ("not currently set, discarded").
func CheckEnv(pass []string, set map[string]string, hostEnv []string) (warnings []string, err error) {
	var problems []string
	for _, p := range pass {
		name, isPrefix := strings.CutSuffix(p, "*")
		switch {
		case p == "*":
			problems = append(problems, `pass_env entry "*": a prefix is needed (e.g. "AWS_*"); passing the whole environment is not supported`)
		case !envNameRe.MatchString(name):
			problems = append(problems, fmt.Sprintf("pass_env entry %q: not a valid variable name (letters, digits and _, optionally ending in * for a prefix)", p))
		case !isPrefix && isReservedEnv(name):
			problems = append(problems, fmt.Sprintf("pass_env entry %q: tangsible sets this variable itself", p))
		}
	}
	for name := range set {
		switch {
		case !envNameRe.MatchString(name):
			problems = append(problems, fmt.Sprintf("set_env entry %q: not a valid variable name", name))
		case isReservedEnv(name):
			problems = append(problems, fmt.Sprintf("set_env entry %q: tangsible sets this variable itself", name))
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return nil, fmt.Errorf("%s", strings.Join(problems, "\n"))
	}
	have := map[string]bool{}
	var names []string
	for _, kv := range hostEnv {
		if name, _, ok := strings.Cut(kv, "="); ok {
			have[name] = true
			names = append(names, name)
		}
	}
	for _, p := range pass {
		if strings.HasSuffix(p, "*") {
			found := false
			for _, n := range names {
				if envPatternMatches(p, n) {
					found = true
					break
				}
			}
			if !found {
				warnings = append(warnings, fmt.Sprintf("pass_env: %q matches no variable in the environment", p))
			}
		} else if !have[p] {
			warnings = append(warnings, fmt.Sprintf("pass_env: %s is not set in the environment; nothing passed", p))
		}
	}
	return warnings, nil
}
