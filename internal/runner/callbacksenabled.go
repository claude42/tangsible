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
	"encoding/json"
	"os/exec"
	"strings"
)

// callbacksEnabledConfigName is how `ansible-config dump` itself names the
// callbacks_enabled setting - confirmed empirically against ansible-core
// 2.19 ("CALLBACKS_ENABLED", not "callbacks_enabled" or the pre-2.11 name
// "DEFAULT_CALLBACK_WHITELIST" - design-docs/OwnCallbackPlugin.md
// roadblock 6).
const callbacksEnabledConfigName = "CALLBACKS_ENABLED"

// ResolveCallbacksEnabled returns the ANSIBLE_CALLBACKS_ENABLED value the
// spawned ansible-playbook should actually get: whatever the user already
// has configured (via ansible.cfg or their own shell's environment - both
// covered by asking `ansible-config dump` rather than parsing ansible.cfg
// ourselves, see currentCallbacksEnabled), with pluginName unioned in -
// never a bare overwrite. ANSIBLE_CALLBACKS_ENABLED *replaces* rather than
// merges with whatever ansible.cfg/env already set (design-docs/
// OwnCallbackPlugin.md roadblock 5); naively setting it to just pluginName
// would silently stop the user's own configured callbacks (profile_tasks,
// timer, ...) from running at all.
func ResolveCallbacksEnabled(pluginName string) string {
	return unionCallbacksEnabled(currentCallbacksEnabled(), pluginName)
}

// currentCallbacksEnabled shells out to `ansible-config dump --only-changed
// --format json` - the same command, run with the same environment and
// working directory SpawnGeneration's own ansible-playbook invocation will
// see - and extracts the effective callbacks_enabled list, or nil if none
// is configured (the common case) or the command/parse failed for any
// reason. Best-effort by design: ansible-config missing, a malformed
// ansible.cfg, or an unexpected output shape all just fall back to nil
// (ResolveCallbacksEnabled then unions pluginName onto an empty list,
// exactly the step-3 behavior from before this existed) - a real
// ansible.cfg problem still surfaces normally, moments later, from
// ansible-playbook's own pre-flight gate; this is never worth blocking a
// generation over.
func currentCallbacksEnabled() []string {
	out, err := exec.Command("ansible-config", "dump", "--only-changed", "--format", "json").Output()
	if err != nil {
		return nil
	}
	return parseCallbacksEnabled(out)
}

// parseCallbacksEnabled is currentCallbacksEnabled's pure parsing half,
// split out so it's testable without a real ansible-config binary. Each
// entry is normally {"name": ..., "value": ...}, except one observed
// anomaly (GALAXY_SERVERS) shaped as a bare {"<key>": {...}} with no
// "name"/"value" at all - decodes to a zero-value entry here, which simply
// never matches callbacksEnabledConfigName below, same tolerance as every
// other entry that isn't the one we're looking for.
func parseCallbacksEnabled(jsonOutput []byte) []string {
	var entries []struct {
		Name  string          `json:"name"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(jsonOutput, &entries); err != nil {
		return nil
	}
	for _, e := range entries {
		if e.Name != callbacksEnabledConfigName {
			continue
		}
		var names []string
		if json.Unmarshal(e.Value, &names) != nil {
			return nil
		}
		return names
	}
	return nil
}

// unionCallbacksEnabled joins existing and pluginName into the comma-
// separated form ANSIBLE_CALLBACKS_ENABLED expects, adding pluginName only
// if it isn't already present (e.g. the user already explicitly enabled it
// themselves) - pure, so it's testable without any subprocess at all.
func unionCallbacksEnabled(existing []string, pluginName string) string {
	for _, name := range existing {
		if name == pluginName {
			return strings.Join(existing, ",")
		}
	}
	return strings.Join(append(existing, pluginName), ",")
}
