package main

import (
	"os"

	"github.com/BurntSushi/toml"
)

// maxHistoryPerPlaybook bounds how many past invocations are kept per
// playbook in .tangsible's [[history]] table - arbitrary, per-project
// discussion (there's no principled number here, just "enough to be
// useful without growing the file forever").
const maxHistoryPerPlaybook = 20

// playbookHistory is one [[history]] entry in .tangsible: a playbook path
// and its past invocations, oldest first. Each entry in Invocations is the
// single space-joined string form of that run's passthrough args (see
// argsToHistoryString/historyStringToArgs in rerunargs.go) - the same
// shape the user would have typed after the playbook name on the command
// line, including an empty string for "no extra args at all".
type playbookHistory struct {
	Playbook    string   `toml:"playbook"`
	Invocations []string `toml:"invocations"`
}

// writeTangsibleConfig writes cfg to path as TOML, overwriting whatever
// was there. This is a full rewrite, not an in-place edit - any comments
// or formatting a user hand-added to .tangsible are lost once it starts
// accumulating history, same tradeoff this project already accepts
// elsewhere for "good enough, not chased further" machinery.
func writeTangsibleConfig(path string, cfg playbookConfig) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(cfg)
}

// appendInvocation records one new invocation (argsString, as produced by
// argsToHistoryString) under playbook's own [[history]] entry in path,
// creating that entry if it doesn't exist yet, and capping it at
// maxHistoryPerPlaybook (dropping the oldest once that's exceeded). Reads
// the file fresh and writes the whole thing back - .tangsible isn't
// expected to be edited concurrently by more than one tangsible process at
// this project's target scale, so no locking is attempted.
func appendInvocation(path, playbook, argsString string) error {
	cfg := readTangsibleConfig(path)

	found := false
	for i := range cfg.History {
		if cfg.History[i].Playbook == playbook {
			cfg.History[i].Invocations = appendCapped(cfg.History[i].Invocations, argsString, maxHistoryPerPlaybook)
			found = true
			break
		}
	}
	if !found {
		cfg.History = append(cfg.History, playbookHistory{
			Playbook:    playbook,
			Invocations: []string{argsString},
		})
	}

	return writeTangsibleConfig(path, cfg)
}

// appendCapped appends next to existing, dropping from the front (oldest
// first) until the result is at most max entries long.
func appendCapped(existing []string, next string, max int) []string {
	out := append(existing, next)
	if len(out) > max {
		out = out[len(out)-max:]
	}
	return out
}

// lastInvocation returns the most recently recorded invocation string for
// playbook out of cfg, and false if there's no history entry for it (or
// that entry has no invocations recorded).
func lastInvocation(cfg playbookConfig, playbook string) (string, bool) {
	for _, h := range cfg.History {
		if h.Playbook != playbook {
			continue
		}
		if len(h.Invocations) == 0 {
			return "", false
		}
		return h.Invocations[len(h.Invocations)-1], true
	}
	return "", false
}
