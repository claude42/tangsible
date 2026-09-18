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

package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"code.aw.net/claude/tangsible/internal/config"
)

// callbackPluginName is our bundled plugin's own CALLBACK_NAME (callback/
// tangsible_jsonl.py) - the name ANSIBLE_CALLBACKS_ENABLED must carry
// (callbacksenabled.go) and, with ".py" appended, the file
// ResolveCallbackPluginDir looks for in each candidate directory - checking
// for the file itself, not just the directory's existence, so a stale or
// empty directory doesn't silently pass.
const callbackPluginName = "tangsible_jsonl"
const callbackPluginFile = callbackPluginName + ".py"

// ResolveCallbackPluginDir finds the directory holding our bundled ansible
// callback plugin (design-docs/OwnCallbackPlugin.md), tried in order:
//
//  1. $TANGSIBLE_CALLBACK_DIR - an explicit override. This is the only
//     candidate that resolves during local development/testing, since
//     none of the locations below exist until the release-packaging step
//     (OwnCallbackPlugin.md's "Shipping the .py from a Go binary") is
//     actually built.
//  2. "<prefix>/share/tangsible", where <prefix> is derived structurally
//     from the running executable's own path (its bin/ dir's own parent) -
//     not from any environment variable or install-time state. This is
//     deliberately not "$XDG_DATA_HOME/tangsible sibling to the binary" -
//     data belongs under .../share, not mixed into .../bin (install.sh's
//     own CALLBACK_DIR comment) - and it's what makes a single rule
//     correctly cover both the default install (~/.local/bin/tangsible ->
//     ~/.local/share/tangsible, exactly $XDG_DATA_HOME's own default) and
//     a --prefix one (/opt/x/bin/tangsible -> /opt/x/share/tangsible,
//     exactly what install.sh's own --prefix-aware DATA_DIR computes)
//     without the two ever needing to agree via a shared environment
//     variable at install time.
//  3. $XDG_DATA_HOME/tangsible (or ~/.local/share/tangsible) - only
//     diverges from 2 when $XDG_DATA_HOME has been customized independently
//     of where the binary itself was installed (a default, no-prefix
//     install with a non-default $XDG_DATA_HOME) - install.sh's own
//     DATA_DIR already follows $XDG_DATA_HOME in that same case, so this
//     candidate is what actually finds it then.
//
// Returns an error naming every location tried if none of them panned out.
func ResolveCallbackPluginDir() (string, error) {
	var tried []string

	if dir := os.Getenv("TANGSIBLE_CALLBACK_DIR"); dir != "" {
		tried = append(tried, dir)
		if hasCallbackPlugin(dir) {
			return dir, nil
		}
	}

	if exe, err := os.Executable(); err == nil {
		dir := prefixRelativeShareDir(exe)
		tried = append(tried, dir)
		if hasCallbackPlugin(dir) {
			return dir, nil
		}
	}

	if dataHome := config.DataHome(); dataHome != "" {
		dir := filepath.Join(dataHome, "tangsible")
		tried = append(tried, dir)
		if hasCallbackPlugin(dir) {
			return dir, nil
		}
	}

	return "", fmt.Errorf(
		"could not find the %s callback plugin (tried: %s) - set $TANGSIBLE_CALLBACK_DIR during development, or see README.md for install locations",
		callbackPluginFile, strings.Join(tried, ", "),
	)
}

func hasCallbackPlugin(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, callbackPluginFile))
	return err == nil && !info.IsDir()
}

// prefixRelativeShareDir returns "<prefix>/share/tangsible" for an
// executable living at "<prefix>/bin/<name>" - the structural derivation
// ResolveCallbackPluginDir's own doc comment explains, split out here as
// a pure function of exe's path so it's directly testable without a real
// os.Executable() call or any filesystem state.
func prefixRelativeShareDir(exe string) string {
	prefix := filepath.Dir(filepath.Dir(exe))
	return filepath.Join(prefix, "share", "tangsible")
}

// CallbackPluginEnv returns the env vars any ansible-playbook invocation
// needs to pick up our bundled plugin - the parts shared by all four call
// sites (design-docs/OwnCallbackPlugin.md's "these don't need the fd dance"
// note for the three one-shot scrapes, "a shared helper... so all four
// sites stay in sync"): where to find it (ANSIBLE_CALLBACK_PLUGINS,
// unioned via PrependCallbackPluginsPath) and that it's turned on
// (ANSIBLE_CALLBACKS_ENABLED, unioned via ResolveCallbacksEnabled) - plus
// ANSIBLE_JSON_INDENT=0, pinned so a user's ansible.cfg overriding it to
// pretty-print can't break any of these four callers' own line-based
// scanners. SpawnGeneration additionally sets TANGSIBLE_EVENT_FD itself
// (fd-transport setup is specific to the live TUI run, not to a one-shot
// scrape that just captures cmd.Output() and tolerates our plugin's own
// stdout-fallback line - along with whatever the run's own default stdout
// callback also writes there - the same "skip anything that isn't valid
// JSON" scanning these sites already did for jsonl.py's own output).
func CallbackPluginEnv() ([]string, error) {
	pluginDir, err := ResolveCallbackPluginDir()
	if err != nil {
		return nil, err
	}
	return []string{
		"ANSIBLE_CALLBACK_PLUGINS=" + PrependCallbackPluginsPath(pluginDir, os.Getenv("ANSIBLE_CALLBACK_PLUGINS")),
		"ANSIBLE_CALLBACKS_ENABLED=" + ResolveCallbacksEnabled(callbackPluginName),
		"ANSIBLE_JSON_INDENT=0",
	}, nil
}

// PrependCallbackPluginsPath returns the ANSIBLE_CALLBACK_PLUGINS value the
// spawned ansible-playbook should get: dir prepended onto the user's own
// existing value (from their environment or ansible.cfg's own
// callback_plugins setting, both already reflected in os.Environ() by the
// time SpawnGeneration reads it), ':'-joined - never a bare overwrite,
// which would stop ansible-playbook from finding the user's own callback
// plugins directory at all (design-docs/OwnCallbackPlugin.md roadblock 8,
// the ANSIBLE_CALLBACK_PLUGINS half - roadblock 5/callbacksenabled.go is
// its ANSIBLE_CALLBACKS_ENABLED counterpart). existing is exactly what
// os.Getenv("ANSIBLE_CALLBACK_PLUGINS") returns - passed in rather than
// read here so this stays a pure, directly testable function.
func PrependCallbackPluginsPath(dir, existing string) string {
	if existing == "" {
		return dir
	}
	return dir + ":" + existing
}
