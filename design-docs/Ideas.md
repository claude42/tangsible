# Random findings / ideas from user testing

Noted here so these won't be forgotten but not something that must be
immediately implemented.

* Debug mode (to improve Resolved variables and od our own debugger)
  * strategy plugin is deprecated
  * use hostvars[inventory_hostname] gives us at least something
  * Use virtual terminal to run ansible-playbook - to support things like
    --step, the debuger or ansible.builtin.pause





* tangsible template seems to be run from /tmp/ - shows in some variables


* Configurable colors

* Work gracefully on white on black and black on white themes

* "What differs?" functionality for a specific host


* More Rerun Options: Failed, Current, Start with Current, Select Tasks, Failed Hosts, All
  hosts

* tangsible config

* export previous runs, rename previous runs

* Easily decrypt individual variables, overwrite existing variables, not sure
  what's still open?
  https://claude.ai/share/a7d53130-a437-403f-9e31-c18cba4ec47e

* Double coding (instead of just colors)

* Sign checksums, let install.sh verify

* Interactive --step, --pause, debugger

* Variable resolution explorer - show expansion of a variable on every host

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

* Go to first failed task keyboard shortcut

* edit file

* hide encrypted values in drill down

* show template for all hosts

* show undefined variables in template



* system wide install + /etc/tangsible/config.toml

* allow tangsible run -l host playbook.yml (in addition to tangsible run
  playbook.yml -l host)

------

* Knoten im Tree öffnen, der auf letzter Zeile ist --> sollte runter
  scrollen, damit Inhalt sichtbar ist
