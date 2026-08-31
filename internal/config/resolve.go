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

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// TangsibleDir is the project-local directory Tangsible reads its settings
// from and writes its state to - see settingsConfig/stateConfig below for
// the split and why it exists (design-docs/Dottangsible-directory.md).
// Replaces the single flat .tangsible file this project used before that
// split.
const TangsibleDir = ".tangsible"

// TangsibleConfigPath is .tangsible/config.toml - see settingsConfig's own
// doc comment. Shared by resolvePlaybook's read and main's
// defaultTreeExpanded caller. A var, not a const, since it's built via
// filepath.Join (same style as configHome below) for cross-platform
// correctness.
var TangsibleConfigPath = filepath.Join(TangsibleDir, "config.toml")

// SettingsConfig is the shape of Tangsible's user-authored settings files -
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
type SettingsConfig struct {
	General struct {
		DefaultPlaybook string `toml:"default_playbook"`
		// DefaultTreeState governs whether a freshly-started run's very
		// first task row starts expanded or collapsed - "expanded" or
		// "collapsed" (case-insensitive, see defaultTreeExpanded), only
		// ever read from the project-local .tangsible/config.toml, not the
		// global one - unlike DefaultPlaybook, this isn't part of any
		// resolution cascade.
		DefaultTreeState string `toml:"default_tree_state"`
		// TwoPaneLayout governs design-docs/TwoPanedLayout.md's two-pane
		// drill-down (tree pane kept visible alongside the output view on a
		// wide enough terminal). A *bool, not a plain bool, so an absent key
		// defaults to true (see twoPaneLayoutEnabled) without the
		// zero-value-collision problem a plain bool would have - unlike
		// DefaultTreeState's string-enum workaround for the same underlying
		// issue, a genuine tri-state (unset/true/false) is simpler here
		// since there's no third named value to give the unset case its own
		// meaning.
		TwoPaneLayout *bool `toml:"two_pane_layout"`
		// Color governs whether the collapsed task row's per-host color
		// list may render in color at all - design-docs/Morehosts.md.
		// Same *bool/nil-means-true shape as TwoPaneLayout, for the same
		// reason. "color = false" here is one of Morehosts.md's three
		// independent triggers (alongside insufficient width and the
		// terminal's own detected color capability) for switching that row
		// to a plain OK/Changed/Skipped/Failed/Unreachable count summary
		// instead - see tui.go's colorEnabledByUser callers.
		Color *bool `toml:"color"`
	} `toml:"general"`
	// Vault holds settings specific to the "vault" Verb (design-docs/
	// Vault.md) - its own table rather than folded into General, since
	// General is otherwise entirely TUI/session-behavior settings and a
	// credentials-adjacent path doesn't belong there.
	Vault struct {
		// PasswordFile is a default --vault-password-file path, used when
		// the vault Verb is invoked with neither --vault-password-file
		// nor --ask-vault-pass nor $ANSIBLE_VAULT_PASSWORD_FILE set - see
		// VaultPasswordFile.
		PasswordFile string `toml:"password_file"`
	} `toml:"vault"`
}

// VaultPasswordFile returns cfg.Vault.PasswordFile - the project's
// configured default vault password file, or "" if unset. A thin
// accessor purely for symmetry with DefaultTreeExpanded/
// TwoPaneLayoutEnabled/ColorEnabledByUser above, all of which read
// SettingsConfig through a named function rather than the raw field.
func VaultPasswordFile(cfg SettingsConfig) string {
	return cfg.Vault.PasswordFile
}

// DefaultTreeExpanded reports whether cfg.General.DefaultTreeState says a
// freshly-started run's first task should start expanded - true only for
// "expanded" (case-insensitive); everything else, including an unset or
// unrecognized value, means collapsed - the documented default, applied
// silently rather than warning on a typo, consistent with this project's
// general "swallow and fall back" convention for config values elsewhere
// (readDefaultPlaybook, DecodeHostResult).
func DefaultTreeExpanded(cfg SettingsConfig) bool {
	return strings.EqualFold(cfg.General.DefaultTreeState, "expanded")
}

// TwoPaneLayoutEnabled reports whether the two-pane drill-down
// (design-docs/TwoPanedLayout.md) should be used. Default true - an unset
// TwoPaneLayout (nil, the common case: most users never set this) means
// enabled; only an explicit "two_pane_layout = false" turns it off.
func TwoPaneLayoutEnabled(cfg SettingsConfig) bool {
	return cfg.General.TwoPaneLayout == nil || *cfg.General.TwoPaneLayout
}

// ColorEnabledByUser reports whether cfg.General.Color permits coloring
// the collapsed task row's per-host summary (design-docs/Morehosts.md).
// Default true - an unset Color (nil, the common case) means enabled;
// only an explicit "color = false" turns it off. This is one of three
// independent inputs tui.go's useColor combines (alongside the
// terminal's own detected color capability and the NO_COLOR
// environment variable) - all three must allow color for that row to
// ever render in color.
func ColorEnabledByUser(cfg SettingsConfig) bool {
	return cfg.General.Color == nil || *cfg.General.Color
}

// Verb identifies which top-level command Tangsible was invoked with -
// "run" (the direct successor of the original bare-playbook invocation),
// "rerun" (see Rerun.md), "role" (see design-docs/Tangsible role.md),
// "template" (see design-docs/Tangsible template.md), "host"/"hosts"
// (see design-docs/HostVerb.md), "vault" (see design-docs/Vault.md,
// per-variable vault editing - no TUI at all, unlike every other Verb),
// or "version" (print build stamps and exit - no TUI, no inventory).
// A Verb is now mandatory: unlike the playbook argument, there's no
// shape-based way to tell "no Verb given" from "Verb given" (every Verb
// looks like a plain word, same as a playbook path), and more verbs were
// expected to follow Rerun.md's own rationale for introducing this - so
// requiring one explicitly, as a breaking change, is simpler than trying
// to keep guessing.
type Verb string

const (
	VerbRun      Verb = "run"
	VerbRerun    Verb = "rerun"
	VerbRole     Verb = "role"
	VerbTemplate Verb = "template"
	VerbHost     Verb = "host"
	VerbHosts    Verb = "hosts"
	VerbRevisit  Verb = "revisit"
	VerbVault    Verb = "vault"
	VerbVersion  Verb = "version"
)

// ParseVerb reads args[0] (os.Args[1:]) as the verb Tangsible was invoked
// with. ok is false if args is empty or its first element isn't a
// recognized verb - the caller treats that as a usage error.
func ParseVerb(args []string) (v Verb, rest []string, ok bool) {
	if len(args) == 0 {
		return "", nil, false
	}
	switch Verb(args[0]) {
	case VerbRun, VerbRerun, VerbRole, VerbTemplate, VerbHost, VerbHosts, VerbRevisit, VerbVault, VerbVersion:
		return Verb(args[0]), args[1:], true
	default:
		return "", nil, false
	}
}

// SplitPlaybookArgs splits args (everything after the verb) into the playbook path and
// the remaining ansible-playbook passthrough args. Tangsible's calling
// convention has always put the playbook first, with everything after it
// passed straight through - so the only way to tell "no playbook given"
// from "playbook given" without breaking that is: if args is empty or
// its first element looks like a flag (starts with '-'), no playbook was
// given positionally, and the whole of args is passthrough; otherwise
// args[0] is the playbook, exactly as before this feature existed.
func SplitPlaybookArgs(args []string) (playbook string, rest []string, explicit bool) {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		return args[0], args[1:], true
	}
	return "", args, false
}

// ConfigHome implements the XDG Base Directory Specification's rule for
// this exact case: $XDG_CONFIG_HOME if set and non-empty, else
// $HOME/.config. Returns "" if neither can be determined (no HOME set),
// in which case resolvePlaybook simply skips that source.
func ConfigHome() string {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config")
}

// ReadTOMLFile decodes path as TOML into a T, returning T's zero value if
// the file doesn't exist (silently - the common case for most of
// resolvePlaybook's sources, not worth a warning) or can't be parsed (a
// warning is printed to stderr - an existing-but-broken file shouldn't
// fail silently and invisibly; safe to print directly, since every caller
// runs before the TUI is ever constructed). Shared by readSettingsConfig
// (this file) and history.go's readState - the two TOML shapes Tangsible
// ever reads from disk - so there's exactly one place that knows the
// stat/decode/warn dance, even though what it decodes into now differs
// between config.toml and state.toml.
func ReadTOMLFile[T any](path string) T {
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

// ReadSettingsConfig reads path (a config.toml, local or global - see
// settingsConfig's own doc comment) via readTOMLFile.
func ReadSettingsConfig(path string) SettingsConfig {
	return ReadTOMLFile[SettingsConfig](path)
}

// ReadDefaultPlaybook returns path's general.default_playbook value - ""
// if the file doesn't exist, can't be parsed, or simply doesn't set that
// key. See readTOMLFile for the shared read/warn behavior.
func ReadDefaultPlaybook(path string) string {
	return ReadSettingsConfig(path).General.DefaultPlaybook
}

// ResolvePlaybook determines which playbook to run when none was given
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
func ResolvePlaybook() (path, source string) {
	if v := os.Getenv("TANGSIBLE_PLAYBOOK"); v != "" {
		return v, "TANGSIBLE_PLAYBOOK"
	}
	if v := ReadDefaultPlaybook(TangsibleConfigPath); v != "" {
		return v, "./.tangsible/config.toml"
	}
	if home := ConfigHome(); home != "" {
		configPath := filepath.Join(home, "tangsible", "config.toml")
		if v := ReadDefaultPlaybook(configPath); v != "" {
			return v, configPath
		}
	}
	if _, err := os.Stat("site.yml"); err == nil {
		return "site.yml", "./site.yml"
	}
	return "", ""
}
