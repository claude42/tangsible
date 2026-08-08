# Random findings / ideas from user testing

Noted here so these won't be forgotten but not something that must be
immediately implemented.

* Sometimes a host gets green first and only later turns yellow. That doesn't
  sound right. First OK, then changed?!
* A summary page (comparable to what ansible-playbook does) would be nice

* Hide cursor when autoscrolling is active

* Need concept on how / when to display stderr output

* All lines are currently selectable

* Plays with no tasks are not displayed

# Further Feature ideas

* Filter
  * All / OK / Skipped / Changed / Failed
  * Full text

* Re-run
  * All, Selected tasks, failed tasks
  * With different hosts

* Progress bar (by analyzing playbook)

* View Play, Task

* --step, --check, --diff

* Interactive prompts (passwords)

* Visualize loops

* When everything is expanded while playbook is still running, show new
  entries also expanded.
