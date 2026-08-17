# Tangsible

## Idea

A TUI interface for ansible aimed at user who use ansible interactively, e.g.
during development but other use cases are also possible.

## Problem

Running ansible-playbook presents a couple of challenges for interactive use

* The output scrolls by very fast. One either has to scroll back in the shell
  or check the log file to really see what happened.
* Output of the commands that ran is limited
* So it's really hard to reconstruct which commands ran fine, which failed and on
  which host and for what reasons.

ansible-playbook provides a few means to improve the situation (e.g. --step)
but it's still very cumbersome.

## Vision

* Console app with a TUI interface
* The app is run instead of ansible-playbook (but will run ansible-playbook
  internally)
* Can run playbooks (for all hosts, specific hosts, full playbook or only
  individual plays)
* Playbooks, tags, hosts, etc. don't have to be specified always on the
  command line but could also come from config file, env variable or selected
  in the TUI app.
* Displays an overview of what happened (plays, hosts, succeded, skipped,
  changed, failed, ...) on a high level
* User can scroll up/down in the list - while the whole playbook is still
  running and generating more output
* User can also filter (only failed, full-text search, ...)
* User can press enter to get additional information, what failed, command
  output, etc.
* User can re-run specific task (individually selected or failed tasks, still
  TBD)
* plus probably a lot more ideas

## Platform

* Plan is to use Go as I'm (somewhat) familiar with it and it provides good
  support for TUI applications
* Initial idea is that my app will shell out to ansible-playbook and process
  its json output. Rationale
  * My app should not just read / parse / visualize an existing output. Idea
    is that it can be used interactively and I can already have a look at the
    ouput of initial plays while the whole playbook is still running. This is
    important, so my app has to process new lines from ansible-playbook live.
  * This implies that something like the `ansible.posix.jsonl` callback
    plugin should be used (verified: it writes one JSON object per line to
    stdout in real time, as opposed to the built-in `json` callback which
    only writes a single buffered blob at the end of the run). Requires the
    `ansible.posix` collection.
  * Implementing a Python callback plugin that interfaces with my app sounds
    like more complexity than necessary (but if the benefits would outweigh
    the simple shell command solution I would be open to it)
* In the beginning only expose a very limited subset of ansible-playbooks
  features / configs. Only add more as necessary.

## Questinos

Reproduced here so they won't be forgotten. Some have answers some don't have
answers yet

* History — not in v1. Single-session only for now; revisit once there's
  something to play around with and it's clearer whether it's actually
  needed.
* Interactive prompts (become password, vault password, `pause` tasks) —
  not in v1, maybe later.
* Ctrl-C — same behavior as hitting Ctrl-C while running ansible-playbook
  directly.
* Scale — should it handle comfortably 10 hosts or 1000?
  * For now I would assume rather in the ballpark of 10 hosts.
  * 1000 hosts does not make sense for such an app IMHO. 
  * You would use this app to test the whole thing but once it's in
    production for 1000 hosts, other tools will be better suited.
