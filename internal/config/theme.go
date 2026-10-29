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

// Implements design-docs/ColorConfig.md's own addendum: named themes, one
// TOML file per theme, holding the exact same shape ColorsConfig itself
// has (flat color keys plus optional [dark]/[light] sub-tables) - just not
// nested under a [colors] key, since the whole file already is one.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// ThemeFile is one loaded theme's own content, split into its
// scheme-independent flat keys and its two optional scheme-specific
// sub-tables - a plain map[string]string in all three cases, not 34 more
// named *string fields the way ColorsConfig has, since a theme file is a
// read-only, load-once concern (never round-tripped back to TOML the way
// SettingsConfig's own fields sometimes conceptually could be) and this
// avoids a second 34-name list to keep in sync with ColorsConfig's own.
// Keys are the exact same snake_case names ColorsConfig's own toml tags
// use - see ResolveEffectiveColors, the one place that reads a ThemeFile
// by those names.
type ThemeFile struct {
	Flat  map[string]string
	Dark  map[string]string
	Light map[string]string
}

// ResolveThemePath finds name's own theme file, in two tiers for a
// bundled-but-user-extensible asset (the pattern the callback plugin's own
// lookup used before it was embedded, design-docs/OwnCallbackPlugin.md):
// <prefix>/share/tangsible/themes/<name>.toml (anything shipped with the
// install), then $XDG_DATA_HOME/tangsible/themes/<name>.toml (a user's
// own, writable regardless of how Tangsible itself was installed) -
// DataHome, not ConfigHome, since a theme is a shareable asset someone
// else might have authored, the same distinction ConfigHome/DataHome
// already draw for an unrelated reason. No $TANGSIBLE_CALLBACK_DIR-style
// dev override - not documented as needed, and easy to add later if it is.
func ResolveThemePath(name string) (path string, ok bool) {
	if exe, err := os.Executable(); err == nil {
		candidate := prefixRelativeThemePath(exe, name)
		if fileExists(candidate) {
			return candidate, true
		}
	}
	if dataHome := DataHome(); dataHome != "" {
		candidate := dataHomeThemePath(dataHome, name)
		if fileExists(candidate) {
			return candidate, true
		}
	}
	return "", false
}

// prefixRelativeThemePath/dataHomeThemePath are ResolveThemePath's own two
// candidate paths, split out as pure functions of an already-known
// exe/dataHome so they're directly testable without a real
// os.Executable() call or a real $XDG_DATA_HOME - the same reasoning
// runner.prefixRelativeShareDir already documents for the identical
// split on the callback-plugin side of this same precedent.
func prefixRelativeThemePath(exe, name string) string {
	prefix := filepath.Dir(filepath.Dir(exe))
	return filepath.Join(prefix, "share", "tangsible", "themes", name+".toml")
}

func dataHomeThemePath(dataHome, name string) string {
	return filepath.Join(dataHome, "tangsible", "themes", name+".toml")
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// LoadTheme resolves and parses name's own theme file - ok is false, with
// nothing warned, if name is "" (no theme configured, the common case);
// also false, this time with a warning collected into pendingWarnings
// (see its own doc comment for why not printed immediately), if name is
// set but no file was found at either ResolveThemePath tier or the file
// it found couldn't be parsed as TOML. Either failure mode returns a
// zero-value ThemeFile - harmless on its own (every map lookup against a
// nil map just misses, exactly like an empty one), so a bad theme
// reference costs only "no theme applied," never a crash or a
// half-applied one.
func LoadTheme(name string) (ThemeFile, bool) {
	if name == "" {
		return ThemeFile{}, false
	}
	path, ok := ResolveThemePath(name)
	if !ok {
		pendingWarnings = append(pendingWarnings, fmt.Sprintf("tangsible: couldn't find theme %q (tried <prefix>/share/tangsible/themes and $XDG_DATA_HOME/tangsible/themes) - no theme applied", name))
		return ThemeFile{}, false
	}

	var raw map[string]interface{}
	if _, err := toml.DecodeFile(path, &raw); err != nil {
		pendingWarnings = append(pendingWarnings, fmt.Sprintf("tangsible: couldn't parse theme file %s: %v - no theme applied", path, err))
		return ThemeFile{}, false
	}

	theme := ThemeFile{
		Flat:  map[string]string{},
		Dark:  map[string]string{},
		Light: map[string]string{},
	}
	for key, value := range raw {
		switch key {
		case "dark":
			copyStringTable(value, theme.Dark)
		case "light":
			copyStringTable(value, theme.Light)
		default:
			if s, ok := value.(string); ok {
				theme.Flat[key] = s
			}
		}
	}
	return theme, true
}

// pendingWarnings collects LoadTheme's own config-validation messages
// instead of printing them immediately - the same reasoning uikit.
// pendingColorWarnings' own doc comment gives for [colors]' own
// per-key validation: LoadTheme runs as part of ResolveEffectiveColors,
// called before any TUI-showing Verb constructs its own tview.
// Application, so an immediate print would sit on the terminal's normal
// screen for well under a second before tview's alternate-screen swap
// visually covers it, and isn't reliably restored on quit either. This
// package can't import internal/uikit (the reverse dependency already
// exists - uikit.ApplyColorConfig takes a config.ColorsConfig), so this
// is its own small mirror of that mechanism rather than a shared one.
var pendingWarnings []string

// PrintPendingWarnings prints (to stderr) and clears every warning
// LoadTheme has collected since the last call - called by session.Main
// right after each Verb's own function returns (i.e. once its TUI, if
// any, has actually closed), the same point uikit.PrintColorWarnings is
// also called from. A no-op when nothing's pending (the common case).
func PrintPendingWarnings() {
	for _, w := range pendingWarnings {
		fmt.Fprintln(os.Stderr, w)
	}
	pendingWarnings = nil
}

// copyStringTable copies raw's own string-valued entries into dst - raw
// is whatever toml.Decode produced for a [dark]/[light] sub-table
// (map[string]interface{} for a real table, something else entirely for
// a malformed file that put a scalar there instead) and a non-string
// value for what should be a color (a stray TOML integer/bool/array) is
// silently skipped rather than propagated - the same "ignore what
// doesn't fit rather than fail the whole file" tolerance LoadTheme's own
// caller-facing behavior already has, just applied one level deeper.
func copyStringTable(raw interface{}, dst map[string]string) {
	table, ok := raw.(map[string]interface{})
	if !ok {
		return
	}
	for k, v := range table {
		if s, ok := v.(string); ok {
			dst[k] = s
		}
	}
}
