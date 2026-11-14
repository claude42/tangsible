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
	"sync"

	"code.aw.net/claude/tangsible/callback"
	"code.aw.net/claude/tangsible/internal/config"
)

// actionPluginFile is our bundled action plugin (callback/action/
// tangsible_vars.py), behind the host verb's Variables tab.
const actionPluginFile = "tangsible_vars.py"

// embeddedActionPlugin is the plugin source compiled into the binary. A var
// so tests can substitute their own bytes.
var embeddedActionPlugin = callback.ActionPlugin

var extractedActionDir struct {
	once sync.Once
	dir  string
	err  error
}

// ResolveActionPluginDir returns the directory holding the action plugin,
// for ANSIBLE_ACTION_PLUGINS: <$TANGSIBLE_CALLBACK_DIR>/action when that
// override is set (it must contain the plugin - no silent fall-through),
// else the embedded copy, extracted once per version to
// $XDG_CACHE_HOME/tangsible/action-<hash>/. A separate directory from the
// callback plugin's: ansible loads every .py file in a plugin directory.
func ResolveActionPluginDir() (string, error) {
	if base := os.Getenv("TANGSIBLE_CALLBACK_DIR"); base != "" {
		dir := filepath.Join(base, "action")
		if info, err := os.Stat(filepath.Join(dir, actionPluginFile)); err != nil || info.IsDir() {
			return "", fmt.Errorf("$TANGSIBLE_CALLBACK_DIR=%s has no action/%s", base, actionPluginFile)
		}
		return dir, nil
	}
	extractedActionDir.once.Do(func() {
		extractedActionDir.dir, extractedActionDir.err = extractPluginFile(embeddedActionPlugin, actionPluginFile, "action-", config.CacheHome(), os.TempDir())
	})
	return extractedActionDir.dir, extractedActionDir.err
}

// ActionPluginEnv returns the env var an ansible-playbook invocation needs
// to find our action plugin, unioned onto the user's own
// ANSIBLE_ACTION_PLUGINS like CallbackPluginEnv does for callbacks.
func ActionPluginEnv() ([]string, error) {
	dir, err := ResolveActionPluginDir()
	if err != nil {
		return nil, err
	}
	return []string{"ANSIBLE_ACTION_PLUGINS=" + PrependCallbackPluginsPath(dir, os.Getenv("ANSIBLE_ACTION_PLUGINS"))}, nil
}
