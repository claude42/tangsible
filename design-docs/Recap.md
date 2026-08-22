# Recap

## Current status

ansible-playbook prints a recap when a playbook has finished. For each host
it prints a line like this:

hostname : ok=159  changed=94   unreachable=0    failed=2    skipped=25   rescued=0    ignored=0

tangsible does not print any information

## Thoughts

The information ansible-navigator prints out is nice but it could be more
helpful. When the user sees e.g. failed=5, they want to get a quick overview
of what actually went wrong instead of having to scroll / search through the
information printed while the playbook was running

## Idea

First line after the playbook is finished is the overall status as is right
now, i.e. "Playbook completed successfully"

We then re-use the mechanisms of the tree view to display the recap but the
structure will be different to what's been printed during the run. Structure
shall be as follows.


hostname : ok=159  changed=94   unreachable=0    failed=2    skipped=25   rescued=0    ignored=0
  ok (159)
  changed (94)
  unreachable (0)
  failed (2)
    role : task name (failure message)
    ...
  skipped (25)
  rescued (0)
  ignored (0)
  
## Behavior
* Initially only the top level (i.e. the summary lines for each host) shall
  be visible
* When the cursor is over one of the summary lines and the user presses
  return then then it shall be expanded (as usual) and the lines with ok /
  changed / unreachable / ... shall appear.
* When the user expands one of these second level lines, this shall be
  expanded and it will contain a line for each task in this category (e.g.
  each failed task). The line shall be "role : task name" and then the
  corresponding message / stdout (same content as in the brackets of the host
  lines in the normal tree)
* When user presses return on one of these lines, the corresponding drilldown
  view shall be opened

## Additional idea

Between the "Summary" heading and the host lines of the summary, I would like
to add a free text summary like this:

    Completed 17 tasks on 5 reachable hosts in xx:xx minutes. 1 host failed
    before the end of the playbook, 2 were not reachable.

Keep in mind: these are additional lines in the tree view that the cursor
hast to jump over when navigating up or down.
