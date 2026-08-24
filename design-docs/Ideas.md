# Random findings / ideas from user testing

Noted here so these won't be forgotten but not something that must be
immediately implemented.

* A summary page (comparable to what ansible-playbook does) would be nice

* Need concept on how / when to display global stderr output

* Search in drill down window

* Summary line sometimes not visible - beyond the screen (still?)

* Errors before / during execution

* OK/Changed/Skipped sumamry instead of individual hosts when space is
  constrained or if --color=no

* Show full log (won't be possible)

* show file

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

* Filter for "interesting" things, i.e. failed, unreachable, changed, stderr,
  warning

* Show corresponding file in drilldown view

* Rerun Failed, Current, Start with Current, Select Tasks, Failed Hosts, All
  hosts, Current host, select

* auto-complete e.g. for entering hosts or tags in rerun dialog

* --check - at least visualization

* Show global stderr after summary page

* tangsible config

* tangsible export
