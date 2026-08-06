# Random findings / ideas from user testing

Noted here so these won't be forgotten but not something that must be
immediately implemented.

* Unfortunately I think my initial visualization idea (OK, Chgd, Skip, Fail)
  is not ideal. 
  * New idea would be to just print the host names in the respective color
  * Let me further think about this first

* Pressing q once seems to only stop ansible-playbook. Tangsible itself only
  stops I press q a second time.

* Once I had saw the behavior that ansible-playbook finished (apparently
  without problems) but after exiting with q I saw an error message

* A summary page (comparable to what ansible-playbook does) would be nice

* Hide cursor when autoscrolling is active

* Skip PLAY / TASK prefixes, render them in different colors instead

* Need some kind of indicator that more command output is available

# Further Feature ideas

* Filter
  * All / OK / Skipped / Changed / Failed
  * Full text

* Re-run
  * All, Selected tasks, failed tasks
  * With different hosts

* Progress bar (by analyzing playbook)

* View Play, Task

* --step
