package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// playbookConfig is the shape of .tangsible and
// $XDG_CONFIG_HOME/tangsible/config.toml - deliberately minimal (a single
// key), since the user's own spec for this format explicitly expects it
// to grow later.
type playbookConfig struct {
	General struct {
		DefaultPlaybook string `toml:"default_playbook"`
	} `toml:"general"`
}

// splitPlaybookArgs splits args (os.Args[1:]) into the playbook path and
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

// readDefaultPlaybook reads path as a playbookConfig TOML file and
// returns its general.default_playbook value - "" if the file doesn't
// exist (the common case for 3 of resolvePlaybook's 4 sources, not worth
// a warning), can't be parsed (a warning is printed to stderr here, since
// an existing-but-broken config shouldn't fail silently and invisibly -
// safe to print directly, since this always runs before the TUI is ever
// constructed), or simply doesn't set that key.
func readDefaultPlaybook(path string) string {
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	var cfg playbookConfig
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		fmt.Fprintf(os.Stderr, "tangsible: couldn't parse %s: %v\n", path, err)
		return ""
	}
	return cfg.General.DefaultPlaybook
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
	if v := readDefaultPlaybook(".tangsible"); v != "" {
		return v, "./.tangsible"
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
