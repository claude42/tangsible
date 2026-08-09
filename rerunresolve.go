package main

// rerunResolution is what the "rerun" verb (Rerun.md) needs before it can
// construct anything: which playbook to run, and what to pre-fill the
// startup re-run dialog's Tags/Hosts fields with, plus the passthrough
// args every generation carries forward unedited (the dialog's Task/Tags/
// Hosts fields are the only ones it ever exposes for editing).
type rerunResolution struct {
	Playbook string
	Tags     string
	Hosts    string
	Rest     []string
}

// resolveRerun computes a rerunResolution from the "rerun" verb's own args
// (everything after the verb - possibly an explicit playbook, possibly
// -l/--tags/other passthrough args) and cfg, the already-loaded .tangsible
// config (see readTangsibleConfig). ok is false only if no playbook could
// be determined at all - no explicit one given, and no LastPlaybook on
// record (nothing has ever been invoked in this project) - which main.go
// treats as a usage error, the same shape as run's own "no playbook given,
// and none could be determined" case.
//
// Kept as a pure function, no I/O of its own, so it's directly testable -
// main.go is the only caller, and it's the one that actually reads
// .tangsible and prints the resolution note when the playbook came from
// history rather than the command line.
//
// Precedence, per Rerun.md: an explicit playbook on the command line
// always wins over cfg's LastPlaybook; --tags/-l given on the command line
// always win over history's own remembered Tags/Hosts for that playbook;
// any OTHER passthrough arg given on the command line (e.g. a custom -i)
// replaces history's own remembered Rest outright, rather than merging
// with it - "whatever you actually typed wins for its own category,
// whatever you didn't type falls back to history." A playbook with no
// history at all (never run before) resolves to every field at its zero
// value - Rerun.md's own "or no arguments if never run for this playbook"
// - which falls out for free here, no special-casing needed:
// historyStringToArgs("") is nil, and parsePassthroughArgs(nil) is the
// zero parsedPassthroughArgs.
func resolveRerun(args []string, cfg playbookConfig) (res rerunResolution, ok bool) {
	playbook, cliRest, explicit := splitPlaybookArgs(args)
	if !explicit {
		pb, has := lastPlaybook(cfg)
		if !has {
			return rerunResolution{}, false
		}
		playbook = pb
	}

	cliParsed := parsePassthroughArgs(cliRest)

	hist, _ := lastInvocation(cfg, playbook)
	histParsed := parsePassthroughArgs(historyStringToArgs(hist))

	res.Playbook = playbook

	res.Tags = histParsed.Tags
	if cliParsed.Tags != "" {
		res.Tags = cliParsed.Tags
	}

	res.Hosts = histParsed.Hosts
	if cliParsed.Hosts != "" {
		res.Hosts = cliParsed.Hosts
	}

	res.Rest = histParsed.Rest
	if len(cliParsed.Rest) > 0 {
		res.Rest = cliParsed.Rest
	}

	return res, true
}
