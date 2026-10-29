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

// Warnings for a config.toml's own mistakes: an unrecognized key (a typo in
// a key name) or an invalid value for a fixed-choice [general] key (a typo
// in "terminal_progress = \"ok\""). The values still fall back to their
// defaults exactly as before - ParseNotificationKind and friends are
// unchanged - this only makes the fallback visible. Collected into
// pendingWarnings (theme.go) and printed after the TUI closes, like every
// other config warning (PrintPendingWarnings). Checked per file, not on
// the merged result, so the message names the file that holds the typo
// (also when a higher-priority tier shadows it).
package config

import (
	"fmt"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

// generalChoice is one fixed-choice [general] key: its allowed values
// (compared case-insensitively, like the parsers) and its default.
type generalChoice struct {
	key     string
	get     func(SettingsConfig) string
	allowed []string
	def     string
}

var generalChoices = []generalChoice{
	{"default_tree_state", func(c SettingsConfig) string { return c.General.DefaultTreeState }, []string{"expanded", "collapsed"}, "collapsed"},
	{"run_dialog", func(c SettingsConfig) string { return c.General.RunDialog }, []string{"default", "never", "always"}, "default"},
	{"notify_playbook_finished", func(c SettingsConfig) string { return c.General.NotifyPlaybookFinished }, []string{"off", "osc9", "osc777", "osc99", "bell"}, "off"},
	{"notify_task_failed", func(c SettingsConfig) string { return c.General.NotifyTaskFailed }, []string{"off", "osc9", "osc777", "osc99", "bell"}, "off"},
	{"terminal_progress", func(c SettingsConfig) string { return c.General.TerminalProgress }, []string{"auto", "on", "off"}, "auto"},
	{"color_scheme", func(c SettingsConfig) string { return c.General.ColorScheme }, []string{"dark", "light"}, "dark"},
}

// settingsWarnings returns the messages for one parsed config file: path is
// its location, undecoded the keys the decoder had no field for
// (toml.MetaData.Undecoded).
func settingsWarnings(path string, cfg SettingsConfig, undecoded []toml.Key) []string {
	var warnings []string
	for _, k := range undecoded {
		warnings = append(warnings, fmt.Sprintf("tangsible: %s: unknown setting %s, ignored", path, k))
	}
	for _, c := range generalChoices {
		v := c.get(cfg)
		if v == "" || slices.ContainsFunc(c.allowed, func(a string) bool { return strings.EqualFold(a, v) }) {
			continue
		}
		warnings = append(warnings, fmt.Sprintf("tangsible: %s: invalid value %q for [general] %s (allowed: %s), using default %q", path, v, c.key, strings.Join(c.allowed, ", "), c.def))
	}
	return warnings
}

// addPendingWarnings queues warnings for PrintPendingWarnings, skipping any
// already queued: MergedSettingsConfig is called from several places in one
// process, and each re-reads the files.
func addPendingWarnings(warnings []string) {
	for _, w := range warnings {
		if !slices.Contains(pendingWarnings, w) {
			pendingWarnings = append(pendingWarnings, w)
		}
	}
}
