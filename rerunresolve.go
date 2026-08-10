package main

// rerunResolution is what the "rerun" verb (Rerun.md) needs before it can
// construct anything: which playbook to run - or, when the most recent
// invocation in this project was "tangsible role" rather than "tangsible
// run" (design-docs/Tangsible role.md), which role to regenerate a stub
// for instead - and what to pre-fill the startup re-run dialog's Tags/Skip
// tags/Hosts fields with, plus the passthrough args every generation
// carries forward unedited (the dialog's Task/Tags/Skip tags/Hosts fields
// are the only ones it ever exposes for editing). Exactly one of
// Playbook/Role is ever set, mirroring playbookHistory's own shape - an
// explicit playbook positional argument to "rerun" always resolves to
// Playbook (there's no equivalent "tangsible rerun <role>" form; a role
// rerun is only ever reached via the bare, no-argument case falling back
// to history), so Role is only ever set when Playbook wasn't explicitly
// given and the last invocation on record happens to have been a role.
type rerunResolution struct {
	Playbook string
	Role     string
	Tags     string
	SkipTags string
	Hosts    string
	Rest     []string
}

// resolveRerun computes a rerunResolution from the "rerun" verb's own args
// (everything after the verb - possibly an explicit playbook, possibly
// -l/--tags/other passthrough args) and cfg, the already-loaded .tangsible
// config (see readTangsibleConfig). ok is false only if no playbook or
// role could be determined at all - no explicit playbook given, and
// nothing on record via General.Last (nothing has ever been invoked in
// this project) - which main.go treats as a usage error, the same shape as
// run's own "no playbook given, and none could be determined" case.
//
// Kept as a pure function, no I/O of its own, so it's directly testable -
// main.go is the only caller, and it's the one that actually reads
// .tangsible, regenerates a role's stub playbook when needed, and prints
// the resolution note when the playbook/role came from history rather
// than the command line.
//
// Precedence, per Rerun.md: an explicit playbook on the command line
// always wins over cfg's history (and always means a playbook rerun, never
// a role one - see rerunResolution's own doc comment); --tags/--skip-tags/
// -l given on the command line always win over history's own remembered
// Tags/SkipTags/Hosts for that playbook (or role); any OTHER passthrough
// arg given on the command line (e.g. a custom -i) replaces history's own
// remembered Rest outright, rather than merging with it - "whatever you
// actually typed wins for its own category, whatever you didn't type
// falls back to history." A playbook with no history at all (never run
// before) resolves to every field at its zero value - Rerun.md's own "or
// no arguments if never run for this playbook" - which falls out for free
// here, no special-casing needed: historyStringToArgs("") is nil, and
// parsePassthroughArgs(nil) is the zero parsedPassthroughArgs.
func resolveRerun(args []string, cfg playbookConfig) (res rerunResolution, ok bool) {
	playbook, cliRest, explicit := splitPlaybookArgs(args)
	cliParsed := parsePassthroughArgs(cliRest)

	var histParsed parsedPassthroughArgs
	if explicit {
		res.Playbook = playbook
		hist, _ := lastInvocation(cfg, playbook)
		histParsed = parsePassthroughArgs(historyStringToArgs(hist))
	} else {
		entry, has := lastTarget(cfg)
		if !has {
			return rerunResolution{}, false
		}
		res.Playbook = entry.Playbook
		res.Role = entry.Role
		var hist string
		if len(entry.Invocations) > 0 {
			hist = entry.Invocations[len(entry.Invocations)-1]
		}
		histParsed = parsePassthroughArgs(historyStringToArgs(hist))
	}

	res.Tags = histParsed.Tags
	if cliParsed.Tags != "" {
		res.Tags = cliParsed.Tags
	}

	res.SkipTags = histParsed.SkipTags
	if cliParsed.SkipTags != "" {
		res.SkipTags = cliParsed.SkipTags
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
