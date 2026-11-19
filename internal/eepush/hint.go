package eepush

import "strings"

// AuthFailure is whether podman's output says the push was refused for lack of
// a login. Only the wording seen from a real registry (registry:2 with
// htpasswd, with no login and with wrong credentials: "... : authentication
// required") is matched; other registries word it differently, and no hint is
// better than a wrong one.
func AuthFailure(output string) bool {
	return strings.Contains(strings.ToLower(output), "authentication required")
}

// LoginHint is the line shown after an authentication failure for registry.
func LoginHint(registry string) string {
	return "not logged in? run: podman login " + registry
}
