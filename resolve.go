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

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// tangsibleDir is the project-local directory Tangsible reads its settings
// from and writes its state to - see settingsConfig/stateConfig below for
// the split and why it exists (design-docs/Dottangsible-directory.md).
// Replaces the single flat .tangsible file this project used before that
// split.
const tangsibleDir = ".tangsible"

// tangsibleConfigPath is .tangsible/config.toml - see settingsConfig's own
// doc comment. Shared by resolvePlaybook's read and main's
// defaultTreeExpanded caller. A var, not a const, since it's built via
// filepath.Join (same style as configHome below) for cross-platform
// correctness.
var tangsibleConfigPath = filepath.Join(tangsibleDir, "config.toml")

// settingsConfig is the shape of Tangsible's user-authored settings files -
// the project-local .tangsible/config.toml (tangsibleConfigPath) and the
// global $XDG_CONFIG_HOME/tangsible/config.toml (resolved via configHome)
// - both read-only from Tangsible's own point of view: nothing in this
// program ever opens either one for writing. That read-only guarantee is
// the actual fix design-docs/Dottangsible-directory.md exists to make
// possible - not just moving invocation history out of the way, but
// removing every code path that could ever clobber a user's own comments
// or formatting in this file. The two files keep sharing this one type
// (and readSettingsConfig) rather than forking it, since their shape truly
// is identical - the global file's own DefaultTreeState is simply never
// consulted in practice (see its own doc comment below), harmlessly
// unused rather than modeled separately.
//
// State that Tangsible itself owns and writes - invocation history, and
// which target ran most recently - lives in the separate stateConfig
// (history.go), local-only, never in this type.
type settingsConfig struct {
	General struct {
		DefaultPlaybook string `toml:"default_playbook"`
		// DefaultTreeState governs whether a freshly-started run's very
		// first task row starts expanded or collapsed - "expanded" or
		// "collapsed" (case-insensitive, see defaultTreeExpanded), only
		// ever read from the project-local .tangsible/config.toml, not the
		// global one - unlike DefaultPlaybook, this isn't part of any
		// resolution cascade.
		DefaultTreeState string `toml:"default_tree_state"`
	} `toml:"general"`
}

// defaultTreeExpanded reports whether cfg.General.DefaultTreeState says a
// freshly-started run's first task should start expanded - true only for
// "expanded" (case-insensitive); everything else, including an unset or
// unrecognized value, means collapsed - the documented default, applied
// silently rather than warning on a typo, consistent with this project's
// general "swallow and fall back" convention for config values elsewhere
// (readDefaultPlaybook, decodeHostResult).
func defaultTreeExpanded(cfg settingsConfig) bool {
	return strings.EqualFold(cfg.General.DefaultTreeState, "expanded")
}

// verb identifies which top-level command Tangsible was invoked with -
// "run" (the direct successor of the original bare-playbook invocation),
// "rerun" (see Rerun.md), "role" (see design-docs/Tangsible role.md), or
// "template" (see design-docs/Tangsible template.md). A verb is now
// mandatory: unlike the playbook argument, there's no shape-based way to
// tell "no verb given" from "verb given" (every verb looks like a plain
// word, same as a playbook path), and more verbs were expected to follow
// Rerun.md's own rationale for introducing this - so requiring one
// explicitly, as a breaking change, is simpler than trying to keep
// guessing.
type verb string

const (
	verbRun      verb = "run"
	verbRerun    verb = "rerun"
	verbRole     verb = "role"
	verbTemplate verb = "template"
)

// parseVerb reads args[0] (os.Args[1:]) as the verb Tangsible was invoked
// with. ok is false if args is empty or its first element isn't a
// recognized verb - the caller treats that as a usage error.
func parseVerb(args []string) (v verb, rest []string, ok bool) {
	if len(args) == 0 {
		return "", nil, false
	}
	switch verb(args[0]) {
	case verbRun, verbRerun, verbRole, verbTemplate:
		return verb(args[0]), args[1:], true
	default:
		return "", nil, false
	}
}

// splitPlaybookArgs splits args (everything after the verb) into the playbook path and
// the remaining ansible-playbook passthrough args. Tangsible's calling
// convention has always put the playbook first, with everything after it
// passed straight through - so the only way to tell "no playbook given"
// from "playbook given" without breaking that is: if args is empty or
// its first element looks like a flag (starts with '-'), no playbook was
// given positionally, and the whole of args is passthrough; otherwise
// args[0] is the playbook, exactly as before this feature existed.
func splitPlaybookArgs(args []string) (playbook string, rest []string, explicit bool) {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		return args[0], args[1:], true
	}
	return "", args, false
}

// configHome implements the XDG Base Directory Specification's rule for
// this exact case: $XDG_CONFIG_HOME if set and non-empty, else
// $HOME/.config. Returns "" if neither can be determined (no HOME set),
// in which case resolvePlaybook simply skips that source.
func configHome() string {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config")
}

// readTOMLFile decodes path as TOML into a T, returning T's zero value if
// the file doesn't exist (silently - the common case for most of
// resolvePlaybook's sources, not worth a warning) or can't be parsed (a
// warning is printed to stderr - an existing-but-broken file shouldn't
// fail silently and invisibly; safe to print directly, since every caller
// runs before the TUI is ever constructed). Shared by readSettingsConfig
// (this file) and history.go's readState - the two TOML shapes Tangsible
// ever reads from disk - so there's exactly one place that knows the
// stat/decode/warn dance, even though what it decodes into now differs
// between config.toml and state.toml.
func readTOMLFile[T any](path string) T {
	var v T
	if _, err := os.Stat(path); err != nil {
		return v
	}
	if _, err := toml.DecodeFile(path, &v); err != nil {
		fmt.Fprintf(os.Stderr, "tangsible: couldn't parse %s: %v\n", path, err)
		var zero T
		return zero
	}
	return v
}

// readSettingsConfig reads path (a config.toml, local or global - see
// settingsConfig's own doc comment) via readTOMLFile.
func readSettingsConfig(path string) settingsConfig {
	return readTOMLFile[settingsConfig](path)
}

// readDefaultPlaybook returns path's general.default_playbook value - ""
// if the file doesn't exist, can't be parsed, or simply doesn't set that
// key. See readTOMLFile for the shared read/warn behavior.
func readDefaultPlaybook(path string) string {
	return readSettingsConfig(path).General.DefaultPlaybook
}

// resolvePlaybook determines which playbook to run when none was given
// explicitly on the command line, trying each source in order and
// returning the first hit along with a short description of where it
// came from (for the startup note main prints to stderr). Returns
// ("", "") if nothing could be determined - the caller treats that as a
// usage error.
//
// No path joining/resolution happens here - whatever string is found
// (the env var's value, a TOML value, or the literal "site.yml") is
// passed straight through exactly like an explicit command-line argument
// always has been; ansible-playbook (and buildTaskSourceIndex) resolve a
// relative path against Tangsible's own cwd on their own, same as today.
func resolvePlaybook() (path, source string) {
	if v := os.Getenv("TANGSIBLE_PLAYBOOK"); v != "" {
		return v, "TANGSIBLE_PLAYBOOK"
	}
	if v := readDefaultPlaybook(tangsibleConfigPath); v != "" {
		return v, "./.tangsible/config.toml"
	}
	if home := configHome(); home != "" {
		configPath := filepath.Join(home, "tangsible", "config.toml")
		if v := readDefaultPlaybook(configPath); v != "" {
			return v, configPath
		}
	}
	if _, err := os.Stat("site.yml"); err == nil {
		return "site.yml", "./site.yml"
	}
	return "", ""
}
