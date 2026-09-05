tangsible-revisit(1) "Tangsible" "Tangsible Manual"

# NAME

tangsible-revisit - browse and reopen previous runs

# SYNOPSIS

*tangsible revisit* [_playbook.yml or role name_] [--tags | -t _tags_] [--limit | -l _hosts_]

# DESCRIPTION

*revisit* lists every past invocation matching the specified arguments.
Selecting one replays its saved *.jsonl* event stream into a fresh tree —
the exact same live tree UI *tangsible-run*(1) uses, already frozen with
that run's own final outcome, tags, and hosts.

With no arguments, every recorded run across every playbook and role is
shown. An optional _playbook.yml_ or _role_ argument narrows the list to
just that target; *--tags*/*-t* and *--limit*/*-l* narrow it further, to
runs whose own recorded tags/hosts overlap the given value.

Once a run is open, every normal live-tree key applies (see
*tangsible-run*(1)), including *r* to rerun it (starting a real, live
invocation from that point) and *d* to diff it against another recorded
run. 

# EXIT STATUS

*0*
	The list was shown (and browsed, however briefly) and closed
	normally with *q*/*Ctrl-C*.

*1*
	No recorded run matched the given filters, and the list was never
	shown at all.

# SEE ALSO

*tangsible*(1), *tangsible-run*(1), *tangsible-rerun*(1)
