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
// the project-local .tangsible/config.toml (tangsibleConfigPath), the
// global $XDG_CONFIG_HOME/tangsible/config.toml (resolved via configHome),
// and the system-wide SystemConfigPath (/etc/tangsible/config.toml, an
// admin-set default below both of those) - all three read-only from
// Tangsible's own point of view: nothing in this program ever opens any of
// them for writing. That read-only guarantee is the actual fix
// design-docs/Dottangsible-directory.md exists to make possible - not just
// moving invocation history out of the way, but removing every code path
// that could ever clobber a user's own comments or formatting in this
// file. All three files keep sharing this one type (and
// readSettingsConfig) rather than forking it, since their shape truly is
// identical.
//
// Every key below cascades project -> user -> system, the more specific
// tier's own value winning whenever it sets the key at all
// (MergedSettingsConfig) - originally only General.DefaultPlaybook worked
// this way, with every other key read from the project-local file alone,
// but a key that behaves differently from its neighbor for no reason a
// user could discover just from reading config.toml was confusing enough
// on its own to be worth fixing, independent of whether any one key's own
// individual case for staying project-local was reasonable.
//
// State that Tangsible itself owns and writes - invocation history, and
// which target ran most recently - lives in the separate stateConfig
// (history.go), local-only, never in this type.
type SettingsConfig struct {
	General struct {
		DefaultPlaybook string `toml:"default_playbook"`
		// DefaultTreeState governs whether a freshly-started run's very
		// first task row starts expanded or collapsed - "expanded" or
		// "collapsed" (case-insensitive, see defaultTreeExpanded).
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
		// RunDialog governs whether the re-run dialog opens at a session's
		// startup (design-docs/RerunDialog.md) - "default" (show for
		// "rerun", not for "run"/"role" - the pre-existing behavior),
		// "never", or "always", case-insensitive (see
		// RunDialogPreference). An explicit --dialog/--no-dialog on the
		// command line overrides this per invocation.
		RunDialog string `toml:"run_dialog"`
		// NotifyPlaybookFinished/NotifyTaskFailed govern design-docs/
		// Notifications.md's terminal notifications - "off" (the default),
		// "osc9", "osc777", "osc99", or "bell", case-insensitive (see
		// ParseNotificationKind). No auto-detection: the design doc's own
		// first draft proposed a "desktop" option that guessed which OSC
		// variant the terminal supported, dropped after review since
		// there's no reliable way to query that (the same reasoning
		// CopyToClipboard.md already applied to reject tcell's own
		// XTermLike-gated clipboard detection) - the user picks the exact
		// protocol their terminal supports instead.
		NotifyPlaybookFinished string `toml:"notify_playbook_finished"`
		NotifyTaskFailed       string `toml:"notify_task_failed"`
		// NotifyTaskFailedMax caps how many notify_task_failed
		// notifications fire per generation (design-docs/Notifications.md)
		// - a *int, nil-means-default-5, same shape as TwoPaneLayout/Color
		// above, so an unset key doesn't collide with the meaningful value
		// 0. See NotifyTaskFailedMax's own doc comment for what happens
		// once the cap is reached.
		NotifyTaskFailedMax *int `toml:"notify_task_failed_max"`
		// TemplateHostsMax caps how many hosts the "template" Verb will
		// render against without an explicit confirmation prompt
		// (design-docs/Tangsible template.md) - reached when its own
		// comma-separated hostname/group positional (including the "all"
		// keyword) expands past this count. *int, nil-means-default-10, same
		// shape as NotifyTaskFailedMax above and for the same reason.
		TemplateHostsMax *int `toml:"template_hosts_max"`
		// ColorScheme is design-docs/ColorConfig.md's own addendum -
		// "dark" or "light", case-insensitive, governing which of
		// ColorsConfig's own Dark/Light sub-tables (and a loaded
		// theme's own [dark]/[light] sections) applies - see
		// ColorSchemePreference. Deliberately no "auto": this app
		// already rejected guessing a terminal's own capabilities once
		// (NotifyPlaybookFinished's own doc comment, above), and a
		// terminal's background color is, if anything, less reliably
		// queryable than that. Defaults to "dark" - this app's existing
		// hardcoded colors were always tuned for a dark surrounding
		// terminal.
		ColorScheme string `toml:"color_scheme"`
		// Theme names a color theme file to load - design-docs/
		// ColorConfig.md's own addendum, see ResolveThemePath/LoadTheme
		// (theme.go). "" (the default) means no theme; ColorsConfig's
		// own keys and the hardcoded defaults are all that apply.
		Theme string `toml:"theme"`
	} `toml:"general"`

	// Colors is design-docs/ColorConfig.md's own `[colors]` table - see
	// ColorsConfig's own doc comment (colorsconfig.go) for its shape and
	// naming rule.
	Colors ColorsConfig `toml:"colors"`
}

// NotificationKind is General.NotifyPlaybookFinished/NotifyTaskFailed's
// parsed form (design-docs/Notifications.md) - which terminal escape
// sequence, if any, a notification event should send. See
// uikit.SendNotification for how each kind is actually turned into bytes.
type NotificationKind int

const (
	NotificationOff NotificationKind = iota
	NotificationOSC9
	NotificationOSC777
	NotificationOSC99
	NotificationBell
)

// ParseNotificationKind reads a General.NotifyPlaybookFinished/
// NotifyTaskFailed value as a NotificationKind, case-insensitively -
// "osc9"/"osc777"/"osc99"/"bell" match explicitly; everything else,
// including an unset or unrecognized value, falls back to NotificationOff,
// the same "swallow and fall back" convention as DefaultTreeExpanded/
// RunDialogPreference.
func ParseNotificationKind(s string) NotificationKind {
	switch {
	case strings.EqualFold(s, "osc9"):
		return NotificationOSC9
	case strings.EqualFold(s, "osc777"):
		return NotificationOSC777
	case strings.EqualFold(s, "osc99"):
		return NotificationOSC99
	case strings.EqualFold(s, "bell"):
		return NotificationBell
	default:
		return NotificationOff
	}
}

// NotifyPlaybookFinishedKind reads cfg.General.NotifyPlaybookFinished as a
// NotificationKind (design-docs/Notifications.md).
func NotifyPlaybookFinishedKind(cfg SettingsConfig) NotificationKind {
	return ParseNotificationKind(cfg.General.NotifyPlaybookFinished)
}

// NotifyTaskFailedKind reads cfg.General.NotifyTaskFailed as a
// NotificationKind (design-docs/Notifications.md).
func NotifyTaskFailedKind(cfg SettingsConfig) NotificationKind {
	return ParseNotificationKind(cfg.General.NotifyTaskFailed)
}

// defaultNotifyTaskFailedMax is design-docs/Notifications.md's own default
// for General.NotifyTaskFailedMax.
const defaultNotifyTaskFailedMax = 5

// NotifyTaskFailedMax reads cfg.General.NotifyTaskFailedMax, defaulting to
// defaultNotifyTaskFailedMax when unset (design-docs/Notifications.md).
// Once this many notify_task_failed notifications have fired in one
// generation, further failures are counted but not individually notified -
// see session.tui.go's own use of this for the "N further task failures
// suppressed" notice sent once the generation finishes.
func NotifyTaskFailedMax(cfg SettingsConfig) int {
	if cfg.General.NotifyTaskFailedMax == nil {
		return defaultNotifyTaskFailedMax
	}
	return *cfg.General.NotifyTaskFailedMax
}

// defaultTemplateHostsMax is design-docs/Tangsible template.md's own
// default for General.TemplateHostsMax.
const defaultTemplateHostsMax = 10

// TemplateHostsMax reads cfg.General.TemplateHostsMax, defaulting to
// defaultTemplateHostsMax when unset (design-docs/Tangsible template.md).
// Once the "template" Verb's own resolved host list - from its
// comma-separated hostname/group positional, including the "all" keyword -
// exceeds this count, it asks for confirmation before rendering against
// all of them.
func TemplateHostsMax(cfg SettingsConfig) int {
	if cfg.General.TemplateHostsMax == nil {
		return defaultTemplateHostsMax
	}
	return *cfg.General.TemplateHostsMax
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

// DialogPreference is General.RunDialog's parsed form (design-docs/
// RerunDialog.md): DialogPreferenceDefault leaves each Verb's own built-in
// dialog-visibility behavior alone; DialogPreferenceNever/Always force it
// off/on regardless of Verb - see ResolveDialogVisibility, which combines
// this with an explicit --dialog/--no-dialog CLI override.
type DialogPreference int

const (
	DialogPreferenceDefault DialogPreference = iota
	DialogPreferenceNever
	DialogPreferenceAlways
)

// RunDialogPreference reads cfg.General.RunDialog as a DialogPreference,
// case-insensitively - "never"/"always" match explicitly; everything else,
// including an unset or unrecognized value, falls back to
// DialogPreferenceDefault, the same "swallow and fall back" convention as
// DefaultTreeExpanded.
func RunDialogPreference(cfg SettingsConfig) DialogPreference {
	switch {
	case strings.EqualFold(cfg.General.RunDialog, "never"):
		return DialogPreferenceNever
	case strings.EqualFold(cfg.General.RunDialog, "always"):
		return DialogPreferenceAlways
	default:
		return DialogPreferenceDefault
	}
}

// ColorSchemePreference reads cfg.General.ColorScheme, case-insensitively -
// "light" is the only value that means light; everything else, including
// an unset or unrecognized value (a typo, "auto" - deliberately not
// supported, see General.ColorScheme's own doc comment), falls back to
// "dark", the same "swallow and fall back" convention
// DefaultTreeExpanded/RunDialogPreference already use for their own
// two/three-way string settings.
func ColorSchemePreference(cfg SettingsConfig) string {
	if strings.EqualFold(cfg.General.ColorScheme, "light") {
		return "light"
	}
	return "dark"
}

// Verb identifies which top-level command Tangsible was invoked with -
// "run" (the direct successor of the original bare-playbook invocation),
// "rerun" (see Rerun.md), "role" (see design-docs/Tangsible role.md),
// "template" (see design-docs/Tangsible template.md), "host"/"hosts"/
// "inventory" (see design-docs/HostVerb.md), "vault" (see design-docs/
// Vault.md, per-variable vault editing - no TUI at all, unlike every
// other Verb), "debug" (see design-docs/DebugMode.md, its own standalone
// TUI - never internal/session's liveSession), or "version" (print build
// stamps and exit - no TUI, no inventory).
// A Verb is now mandatory: unlike the playbook argument, there's no
// shape-based way to tell "no Verb given" from "Verb given" (every Verb
// looks like a plain word, same as a playbook path), and more verbs were
// expected to follow Rerun.md's own rationale for introducing this - so
// requiring one explicitly, as a breaking change, is simpler than trying
// to keep guessing.
type Verb string

const (
	VerbRun       Verb = "run"
	VerbRerun     Verb = "rerun"
	VerbRole      Verb = "role"
	VerbTemplate  Verb = "template"
	VerbHost      Verb = "host"
	VerbHosts     Verb = "hosts"
	VerbInventory Verb = "inventory"
	VerbRevisit   Verb = "revisit"
	VerbVault     Verb = "vault"
	VerbVersion   Verb = "version"
	VerbDebug     Verb = "debug"
)

// ParseVerb reads args[0] (os.Args[1:]) as the verb Tangsible was invoked
// with. ok is false if args is empty or its first element isn't a
// recognized verb - the caller treats that as a usage error.
func ParseVerb(args []string) (v Verb, rest []string, ok bool) {
	if len(args) == 0 {
		return "", nil, false
	}
	switch Verb(args[0]) {
	case VerbRun, VerbRerun, VerbRole, VerbTemplate, VerbHost, VerbHosts, VerbInventory, VerbRevisit, VerbVault, VerbVersion, VerbDebug:
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

// SystemConfigPath is the system-wide settings file an administrator can
// author to set a shared default for every [general] key (see
// SettingsConfig's own doc comment for the project -> user -> system
// cascade every key now goes through) for every user on a machine, below
// the project and per-user tiers.
//
// Deliberately /etc/tangsible/config.toml, not $XDG_CONFIG_DIRS/tangsible/
// config.toml (i.e. /etc/xdg/tangsible/config.toml by its own default).
// The Base Directory Specification technically covers this case, but in
// practice $XDG_CONFIG_DIRS is honored almost exclusively by desktop-
// session components (autostart entries, menus, user-dirs.defaults) - on a
// real system, checking /etc/xdg turns up exactly that and nothing else,
// while CLI/server tools that want a system config tier overwhelmingly use
// a plain /etc/<name>.conf or /etc/<name>/ instead (matching FHS's own
// stance on /etc: host-specific configuration, no XDG involvement
// mandated). This follows that convention instead, using the same
// config.toml filename and [general] shape as the other two tiers so an
// admin can hand a user roughly the file they'd have written themselves.
//
// A var, not a const, purely so tests can point it at a temp file instead
// of the real /etc/tangsible/config.toml - same reasoning as
// TangsibleConfigPath being a var.
var SystemConfigPath = filepath.Join("/etc", "tangsible", "config.toml")

// DataHome mirrors ConfigHome for the XDG Base Directory Specification's
// data-home rule: $XDG_DATA_HOME if set and non-empty, else
// $HOME/.local/share. Used by runner.ResolveCallbackPluginDir to find the
// bundled ansible callback plugin's installed location (design-docs/
// OwnCallbackPlugin.md's "Shipping the .py"). Returns "" if neither can be
// determined, same convention as ConfigHome.
func DataHome() string {
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share")
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

// firstNonEmpty returns the first non-"" string in vals, or "" if every one
// is "" - the merge rule every string-valued [general] key uses in
// MergedSettingsConfig below: the more specific tier's own value wins,
// falling through to a less specific tier only when the key is genuinely
// absent there, not when it's merely set to something falsy.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// firstNonNil is firstNonEmpty's own analogue for the *bool/*int-shaped
// [general] keys (TwoPaneLayout, Color, NotifyTaskFailedMax,
// TemplateHostsMax) - same "more specific tier wins, fall through only on
// genuine absence" rule, generic since those fields aren't all the same
// underlying type.
func firstNonNil[T any](vals ...*T) *T {
	for _, v := range vals {
		if v != nil {
			return v
		}
	}
	return nil
}

// MergedSettingsConfig reads the project-local, global, and system config
// files and merges them into one SettingsConfig, project overriding global
// overriding system, field by field - the same project -> user -> system
// cascade General.DefaultPlaybook has always used (see ResolvePlaybook),
// now applied uniformly to every other [general] key too rather than
// leaving DefaultPlaybook as the only one that behaves this way (your own
// call, design-docs/ConfigOptions.md: a key that's project-local-only
// while its neighbor cascades is confusing regardless of whether there's a
// principled reason for the difference).
//
// Deliberately not folded into ResolvePlaybook itself: that function has
// two sources this one doesn't (TANGSIBLE_PLAYBOOK, a bare ./site.yml
// fallback) and reports which tier its own answer came from, for the
// startup note main.go prints - a general merge has no single "source" to
// report once several keys might each come from a different tier.
// ResolvePlaybook keeps its own hand-rolled cascade rather than reading
// General.DefaultPlaybook off this function's result.
func MergedSettingsConfig() SettingsConfig {
	project := ReadSettingsConfig(TangsibleConfigPath)
	var global SettingsConfig
	if home := ConfigHome(); home != "" {
		global = ReadSettingsConfig(filepath.Join(home, "tangsible", "config.toml"))
	}
	system := ReadSettingsConfig(SystemConfigPath)

	var merged SettingsConfig
	merged.General.DefaultPlaybook = firstNonEmpty(project.General.DefaultPlaybook, global.General.DefaultPlaybook, system.General.DefaultPlaybook)
	merged.General.DefaultTreeState = firstNonEmpty(project.General.DefaultTreeState, global.General.DefaultTreeState, system.General.DefaultTreeState)
	merged.General.TwoPaneLayout = firstNonNil(project.General.TwoPaneLayout, global.General.TwoPaneLayout, system.General.TwoPaneLayout)
	merged.General.Color = firstNonNil(project.General.Color, global.General.Color, system.General.Color)
	merged.General.RunDialog = firstNonEmpty(project.General.RunDialog, global.General.RunDialog, system.General.RunDialog)
	merged.General.NotifyPlaybookFinished = firstNonEmpty(project.General.NotifyPlaybookFinished, global.General.NotifyPlaybookFinished, system.General.NotifyPlaybookFinished)
	merged.General.NotifyTaskFailed = firstNonEmpty(project.General.NotifyTaskFailed, global.General.NotifyTaskFailed, system.General.NotifyTaskFailed)
	merged.General.NotifyTaskFailedMax = firstNonNil(project.General.NotifyTaskFailedMax, global.General.NotifyTaskFailedMax, system.General.NotifyTaskFailedMax)
	merged.General.TemplateHostsMax = firstNonNil(project.General.TemplateHostsMax, global.General.TemplateHostsMax, system.General.TemplateHostsMax)
	merged.General.ColorScheme = firstNonEmpty(project.General.ColorScheme, global.General.ColorScheme, system.General.ColorScheme)
	merged.General.Theme = firstNonEmpty(project.General.Theme, global.General.Theme, system.General.Theme)
	merged.Colors = mergeColors(project.Colors, global.Colors, system.Colors)
	return merged
}

// ResolvePlaybook determines which playbook to run when none was given
// explicitly on the command line, trying each source in order - env var,
// project config, per-user config, system config, then a bare site.yml -
// and returning the first hit along with a short description of where it
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
	if v := ReadDefaultPlaybook(SystemConfigPath); v != "" {
		return v, SystemConfigPath
	}
	if _, err := os.Stat("site.yml"); err == nil {
		return "site.yml", "./site.yml"
	}
	return "", ""
}
