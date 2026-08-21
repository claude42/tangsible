# Concept for more hosts

## Problem

The way results are displayed reight now tangsiable is explicitely geared
toward a small number of hosts. If that number grows at some point there just
won't benough horizontal screen real estate - depending on the terminal size
sooner rather than later.

A unrelated problem - which might share the same solution: Currently success,
change, failure is only coded via a color (at least before opening the host
lines in the treeview). So color-blind users or users with monchrome
terminals will have a problem.

## Proposed solution

Conditions:
* if the horizontal space is not enough to sensibly display all hosts - i.e.
  if at least one of the host names would have to be shortened to two
  characters or less,
* or if the currently used terminal is unable to display colors
* or if the user has explicitely set: color=false in .tangsible

then

The list of colored hostnames that visualize the state of the current task
shall be replaced by the following string

OK:xx/Chagd:xx/Skip:xx/Fail:xx/Unrch:xx

If the terminal can do colors and the user has not set color=false, then the
elements of this strings shall be colored in the corresponding colors,
otherwise it shall be simply an uncolored string.

claude --resume d5719640-c051-4d0e-8bc2-6fdb0bfc2063
