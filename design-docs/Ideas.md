# Random findings / ideas from user testing

Noted here so these won't be forgotten but not something that must be
immediately implemented.

* fallback_project - i.e. what gets used, when cwd does not contain an
  ansible project

* 'e' works in drill down but doesn't update tab, probably doesn't reread when
  re-run

* Statstics
  * host timings
  * task timings
  * last run, all runs

* Jump to first / next fail

* tangsible host
  * uptime

* Debugger
  * configurable keyboard shortcuts
  * mouse control for variable explorer
  * UX improvement for varible explorer
  * Some more things in DebugMode.md
  * show undefined variables in template
  * change variable - not just for the current step
  * restart from beginning / cursor / redo role (accepting to lose context)
    * re-read files!
  * click on future tasks which have no host lines yet

* Bring resolved tab for run verb back again with Action Plugins
  (see Drilldown, Resolved Values.md)

* How to handle no_log

* Details tab shows less info than before (really?)

* Different tangsible vault approach
  * show vars in tui, edit with enter

* tangsible host inventory
  * add host to group

* Debugger
  * restart from beginning
  * configurable keyboard shortcuts
  * a few more in DebugMode.md

* tangsible template seems to be run from /tmp/ - shows in some variables


* "What differs?" functionality for a specific host


* More Rerun Options: Failed, Current, Start with Current, Select Tasks, Failed Hosts, All
  hosts

* tangsible config

* Easily decrypt individual variables, overwrite existing variables, not sure
  what's still open?
  https://claude.ai/share/a7d53130-a437-403f-9e31-c18cba4ec47e

* Double coding (instead of just colors)

* Sign checksums, let install.sh verify

* Desktop notifications with OSC 9 / OSC 777, notify send, terminal-notifier

* git-aware rerun suggestions (git diff --name-only)

* Watch mode (check for changes to playbook etc and then run automatically) -
  probably not

* Directed graph visualizing dependencies between tasks - probably not

* Mistral's ideas

* Copy-as-ansible-playbook-invocation
  One key that puts the exact ansible-playbook ... command (including the effective limit/tags/extra-vars that were used) onto the clipboard, ready to paste into a ticket or a CI job.

* --check -> visualize items which do not support --check

* edit file


* tangsible rerun --only-failed

* Drop tangsible role?

* systemd integration

* Host - use tree in play tab

------

Wild stuff

* Get all origins of a variable
* tangsible ee inspect
* Debugging on EEs
* Presets
* no verb
* Loops

not so wild
* variable explorer vs. facts


------

Bugs

* tangsible revisit -> total runtime is 00:00

* Knoten im Tree öffnen, der auf letzter Zeile ist --> sollte runter
  scrollen, damit Inhalt sichtbar ist

* Select a line in the summary -> cursor left/right won't work as expected

Promote

* https://search.brave.com/ask?q=How+to+promote+an+open+source+project&source=llmSuggest&conversation=09b901abb579898038d8f714856c761d67ff
* esp. ProductHunt
* https://dev.to/wasp/how-i-promoted-my-open-source-repo-to-6k-stars-in-6-months-3li9
* https://saascity.io/where-to-promote-open-source-projects
