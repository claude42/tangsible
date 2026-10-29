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
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"code.aw.net/claude/tangsible/callback"
	"code.aw.net/claude/tangsible/internal/config"
)

// callbackPluginName is our bundled plugin's own CALLBACK_NAME (callback/
// tangsible_events.py) - with ".py" appended, the file the directory
// ResolveCallbackPluginDir returns must contain.
const callbackPluginName = "tangsible_events"
const callbackPluginFile = callbackPluginName + ".py"

// embeddedPlugin is the plugin source compiled into the binary. A var so
// tests can substitute their own bytes.
var embeddedPlugin = callback.Plugin

// extractedDir memoizes the extraction for the life of the process: the
// embedded bytes can't change, and every spawn (and every one-shot scrape)
// asks for the directory again.
var extractedDir struct {
	once sync.Once
	dir  string
	err  error
}

// ResolveCallbackPluginDir returns the directory holding our ansible
// callback plugin (design-docs/OwnCallbackPlugin.md), for
// ANSIBLE_CALLBACK_PLUGINS:
//
//  1. $TANGSIBLE_CALLBACK_DIR, when set - an explicit override for plugin
//     development, patching it, or a packager who wants a fixed path. It
//     must contain the plugin; there is no silent fall-through, a
//     misconfigured override is an error.
//  2. The copy embedded in the binary, extracted once per version to
//     $XDG_CACHE_HOME/tangsible/plugin-<hash>/ (see extractPlugin).
//
// The plugin and the binary speak a private protocol, so an on-disk copy
// from some other version is deliberately never searched for.
func ResolveCallbackPluginDir() (string, error) {
	if dir := os.Getenv("TANGSIBLE_CALLBACK_DIR"); dir != "" {
		if !hasCallbackPlugin(dir) {
			return "", fmt.Errorf("$TANGSIBLE_CALLBACK_DIR=%s has no %s", dir, callbackPluginFile)
		}
		return dir, nil
	}
	extractedDir.once.Do(func() {
		extractedDir.dir, extractedDir.err = extractPlugin(embeddedPlugin, config.CacheHome(), os.TempDir())
	})
	return extractedDir.dir, extractedDir.err
}

func hasCallbackPlugin(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, callbackPluginFile))
	return err == nil && !info.IsDir()
}

// extractPlugin writes content as the plugin file into a directory named
// after its hash, so versions never overwrite each other: under
// <cacheHome>/tangsible/ if that works, else (no usable home or a read-only
// cache) a fresh private directory below tmpDir - which is not cleaned up,
// it holds one small file. The directory holds nothing but the plugin:
// ansible loads every .py file in a callback plugin directory.
func extractPlugin(content []byte, cacheHome, tmpDir string) (string, error) {
	sum := sha256.Sum256(content)
	name := "plugin-" + hex.EncodeToString(sum[:])[:12]
	var failures []string
	if cacheHome != "" {
		dir := filepath.Join(cacheHome, "tangsible", name)
		err := writePluginFile(dir, content)
		if err == nil {
			return dir, nil
		}
		failures = append(failures, err.Error())
	}
	dir, err := os.MkdirTemp(tmpDir, "tangsible-plugin-")
	if err == nil {
		if err = writePluginFile(dir, content); err == nil {
			return dir, nil
		}
	}
	failures = append(failures, err.Error())
	return "", fmt.Errorf("could not extract the %s callback plugin: %s", callbackPluginFile, strings.Join(failures, "; "))
}

// writePluginFile makes dir hold exactly content as the plugin file. An
// already-correct file is left alone (no write on a normal start; also a
// concurrent tangsible never sees it change). Otherwise it is written to a
// temp name - not .py, so ansible can't pick it up half-written - and
// renamed into place, which is atomic.
func writePluginFile(dir string, content []byte) error {
	target := filepath.Join(dir, callbackPluginFile)
	if have, err := os.ReadFile(target); err == nil && bytes.Equal(have, content) {
		return nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".extract-")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(content)
	cerr := tmp.Close()
	if err := firstErr(werr, cerr); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), target); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// CallbackPluginEnv returns the env vars any ansible-playbook invocation
// needs to pick up our bundled plugin - the parts shared by all four call
// sites (design-docs/OwnCallbackPlugin.md's "these don't need the fd dance"
// note for the three one-shot scrapes, "a shared helper... so all four
// sites stay in sync"): where to find it (ANSIBLE_CALLBACK_PLUGINS,
// unioned via PrependCallbackPluginsPath) - plus ANSIBLE_JSON_INDENT=0,
// pinned so a user's ansible.cfg overriding it to pretty-print can't break
// any of these four callers' own line-based scanners. The plugin is not
// listed in ANSIBLE_CALLBACKS_ENABLED: it sets CALLBACK_NEEDS_ENABLED =
// False, so it loads from the plugin path alone and the user's own
// callbacks_enabled is never touched (and never needs querying - that
// ansible-config run used to sit on the path to every first frame).
// SpawnGeneration additionally sets TANGSIBLE_EVENT_FD itself
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
// the ANSIBLE_CALLBACK_PLUGINS half - roadblock 5 is its
// ANSIBLE_CALLBACKS_ENABLED counterpart, since removed). existing is exactly what
// os.Getenv("ANSIBLE_CALLBACK_PLUGINS") returns - passed in rather than
// read here so this stays a pure, directly testable function.
func PrependCallbackPluginsPath(dir, existing string) string {
	if existing == "" {
		return dir
	}
	return dir + ":" + existing
}
