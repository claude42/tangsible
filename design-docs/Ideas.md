# Random findings / ideas from user testing

Noted here so these won't be forgotten but not something that must be
immediately implemented.

* Jump to first / next fail

* Roles (see "Tangsible role.md")
  * support for other role paths in
    * include_role, import_role and meta/main.yml dependencies
  * support for roles in collection
* tangsible host
  * uptime
  * q ends, doesn't go back to list
  * show when which role ran last

* Debugger
  * remove timer
  * configurable keyboard shortcuts
  * mouse control for variable explorer
  * UX improvement for varible explorer
  * Some more things in DebugMode.md
  * show undefined variables in template
  * don't show passwords etc.
  * change variable - not just for the current step
  * restart from beginning / cursor / redo role (accepting to lose context)
  * Use Data Tagging

* Details tab shows less info than before (really?)

* Treeformatting broken when two-pane mode is active

* Different tangsible vault approach
  * show vars in tui, edit with enter

* tangsible host inventory
  * add host to group


* tangsible template seems to be run from /tmp/ - shows in some variables


* Configurable colors

* Work gracefully on white on black and black on white themes

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

* Execution environments

* Directed graph visualizing dependencies between tasks - probably not

* Mistral's ideas

* Copy-as-ansible-playbook-invocation
  One key that puts the exact ansible-playbook ... command (including the effective limit/tags/extra-vars that were used) onto the clipboard, ready to paste into a ticket or a CI job.

* --check -> visualize items which do not support --check

* edit file

* hide encrypted values in drill down


* allow tangsible run -l host playbook.yml (in addition to tangsible run
  playbook.yml -l host)

* tangsible rerun --only-failed

* Drop tangsible role?

------

Bugs

* tangsible revisit -> total runtime is 00:00

* Knoten im Tree öffnen, der auf letzter Zeile ist --> sollte runter
  scrollen, damit Inhalt sichtbar ist

* Select a line in the summary -> cursor left/right won't work as expected

Promote

* https://search.brave.com/ask?q=How+to+promote+an+open+source+project&source=llmSuggest&conversation=09b901abb579898038d8f714856c761d67ff
* esp. ProductHunt
