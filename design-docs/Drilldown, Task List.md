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


# New

## ansible.builtin.shell

Similar behavior as ansible.builtin.command, but use

Command: <cmd>

## ansible.builtin.apt_repository

Same as above, use

Filename: <sources_added>

## ansible.builtin.assemble, ansible.builtin.git

Same behavior as e.g. ansible.builtin.copy.

Filename: <dest>

## ansible.builtin.user

Print the following on the drilldown page

User: <name>
SSH public key: <ssh_public_key>

In the host line add (User: <name>, SSH public key: <ssh_public_key>)

## Warnings in general

If the results contain a <warnings> field. Add another section to the
drilldown page (between Output and Error) and print the contents of the
warnigns field there.








More to come but let's start with these to see how it looks
