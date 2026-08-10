# tangsible role

## Problem

When developing a new role this role cannot be simply executed by running
ansible-playbook. Instead one has to call this role from some playbook.
Either you add it to some existing playbook or you create a new stub
playbook.

## Idea

Add a new verb

  tangsible role <role_name>

What it does is
  * create a small, temporary stub playbook which references this role
  * then execute the playbook like every other playbook. Command line
    arguments given to role (e.g. -l) will be taken into account as usual
  * delete the stub playbook again after tangsible exits (the whole
    process, not just one generation - a mid-session re-run via the r key
    reuses the same stub without regenerating it, exactly like a normal
    playbook re-run does today)

## The stub playbook

  * hosts: all - roles are generally host-agnostic; the actual target is
    narrowed the normal way, via -l/--limit or -i on the command line
  * Written into the current working directory, not a system temp
    directory - a dot-prefixed name, e.g. .tangsible-role-<role_name>.yml.
    This matters beyond tidiness: the existing output drill-down view
    finds a task's own source (the "Task definition"/"Play definition"
    sections) by walking the playbook's own directory tree looking for
    matching YAML files, and never traces roles:/include_role references
    to find them another way. If the stub lived in a system temp
    directory, that walk would start from an empty directory and those
    sections would silently go blank for every role run. Should be added
    to .gitignore.

## Role resolution

  * The role is identified by its bare name on the command line, e.g.
    tangsible role nginx
  * Best-effort search for the role's own directory: ./roles/<role_name>
    under the current working directory, then each entry of
    $ANSIBLE_ROLES_PATH. This is purely so the stub can be placed
    somewhere the directory walk above can actually find the role - it is
    not a full reimplementation of ansible's own role-search algorithm.
  * If the role can't be found this way, the stub is still generated and
    handed to ansible-playbook to resolve however it normally would - the
    only cost is that the Task/Play definition sections stay empty for
    that role's tasks, same "omit rather than fail" behaviour the
    drill-down view already falls back to elsewhere (e.g. a task whose
    source file just isn't found).

## Rerun history

To make rerun work correctly, it has to store a call to tangsible role
appropriately. The existing [[history]] table gets a second, mutually
exclusive shape: an entry is either a playbook run or a role run, never
both.

  [[history]]
  playbook = "site.yml"
  invocations = ["-l somehost"]

  [[history]]
  role = "some_role"
  invocations = ["-l nirvana", "", "-l nirvana,moksha"]

So when tangsible rerun is called with no playbook/role given, it has to
determine whether the last invocation in this project was a run or a
role. Rather than tracking that as a separate flag, General.last_playbook
becomes a single General.last field holding whichever name was invoked
most recently - a playbook path or a role name. Resolving it just means
scanning [[history]] for an entry whose playbook or role field matches:
whichever field matches tells you which kind it was, no separate marker
needed. (A playbook and a role sharing the exact same name is vanishingly
unlikely, and not worth more machinery than picking one deterministically,
e.g. preferring a role match, if it ever happens.)

## Loose end

The TUI's top bar currently shows the resolved playbook's filename. For a
role run it should show the role name instead - the stub's own filename
isn't meaningful to the user.
