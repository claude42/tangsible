# Random findings / ideas from user testing

Noted here so these won't be forgotten but not something that must be
immediately implemented.

* tangsible template seems to be run from /tmp/ - shows in some variables

* Strategy free

* Per-host task timing - see [PerHostTaskTiming.md](PerHostTaskTiming.md);
  discussed and investigated, leaning toward dropping (queueing vs. actual
  duration is ambiguous under linear strategy when forks < host count)

* use
  ansible.builtin.debug:
    var: hostvars[inventory_hostname]

  to print out all variables

* Configurable colors

* Work gracefully on white on black and black on white themes

* "What differs?" functionality for a specific host

* Show corresponding file in drilldown view

* More Rerun Options: Failed, Current, Start with Current, Select Tasks, Failed Hosts, All
  hosts

* Show global stderr after summary page

* tangsible config

* export previous runs, rename previous runs

* Easily decrypt individual variables, overwrite existing variables, not sure
  what's still open?
  https://claude.ai/share/a7d53130-a437-403f-9e31-c18cba4ec47e

* Do our own plugin?

* tangsible info (version information all around)

* Show task times somewhere (where?)

* Double coding (instead of just colors)

* tangsible run --dialog, tangsible role --dialog

* CHANGELOG

* Sign checksums, let install.sh verify
