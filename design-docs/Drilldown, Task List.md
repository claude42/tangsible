# Drilldown + Task List

I have documented a couple of special cases for specific commands that should
be handled a bit differently.

## ansible.builtin.debug

I'm actually surprised that nothing's printed yet because the whole goal of
this action is to print something. But here we go. Please print the msg part
unter the Output headline. And if it's just one line, add it also to the host
line in the treeview

## ansible.builtin.copy, ansible.builtin.file, ansible.builtin.stat, ansible.builtin.template

In addition to anything that might have gone to stdout already, please also
print either

Filename: <dest>

or

Filename: <path>

depending on which field exists in the results. Also add this to the host
line in the treeview.

## ansible.builtin.command

Similar behavior as the actions above, but use

Command: <cmd>



More to come but let's start with these to see how it looks
