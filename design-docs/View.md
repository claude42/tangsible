# View.md

## Situation

Sometimes one doesn't want to run or debug a playbook but one simply wants to
view the playbook, a role or some other file. Of course one could traverse
the directory tree and view the files with less or similar. But that's
somewhat cumbersome.

## Idea

Provide the user with a similar view than run and debug provide but just as a
read-only view. Introduce a new verb: tangsible view

How it should look like compared to run or debug:

* Initially it should render the tree in a similar way to the debug view -
  i.e. only plays and tasks but no host lines. Of course in this static view
  only the tasks known at that point can be rendered. Included tasks or
  similar can not be rendered.
* In contrast to the other trees pressing return / clicking on the task lines
  shall open the drill down view
* The drill down view shall only contain the Task, Task definition and Docs
  pages
* The Task page shows
  * "Task:", "Action:" as in the other views
  * In addition it shall display "Hosts:" with the corresponding definition
    (e.g. group1,!db1)


* the tree keyboard shortcuts "f", "o", "s" make no sense in this view, "r"
  should trigger the re-run dialog as usual


## Later: a Variables tab (2026-11-26)

Not part of the first cut. The drill-down could get a Variables tab for a
chosen host: the variables in effect at the selected task, including play
level (`vars:`, `vars_files:`, role vars, block and task vars). This is how
play-level variables reach `tangsible variables` (`Variables.md`, "view and
variables"). `view` is built first because both share the static project
layer (plays, roles, vars files).
