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
    finds a task's own source (the "Task definition" section) by walking
    the playbook's own directory tree looking for
    matching YAML files, and never traces roles:/include_role references
    to find them another way. If the stub lived in a system temp
    directory, that walk would start from an empty directory and those
    sections would silently go blank for every role run. Should be added
    to .gitignore.

## Role resolution

  * The role is identified by its bare name on the command line, e.g.
    tangsible role nginx
  * Best-effort search for the role's own directory: ./roles/<role_name>
    under the current working directory, then each entry of ansible's
    effective roles path (internal/rolepath: asked of `ansible-config
    dump`, so $ANSIBLE_ROLES_PATH, ansible.cfg's roles_path, or the
    built-in ~/.ansible/roles, /usr/share/ansible/roles and
    /etc/ansible/roles). A role found outside the project tree still gets
    its Task definition sections: source.BuildTaskSourceIndex also indexes
    the directories of roles named in the playbook's (or stub's) roles:
    lists. Only those - never a whole roles directory - and not roles
    pulled in solely via include_role/import_role or meta dependencies.
    It is not a full reimplementation of ansible's own role-search
    algorithm (no collections).
  * If the role can't be found this way, the stub is still generated and
    handed to ansible-playbook to resolve however it normally would - the
    only cost is that the Task definition section stays empty for
    that role's tasks, same "omit rather than fail" behaviour the
    drill-down view already falls back to elsewhere (e.g. a task whose
    source file just isn't found).

### Open issues (postponed, 2026-10-25)

Roles outside the project are found and indexed only when named literally in
a `roles:` list (or the stub). Not yet covered:

  * **include_role / import_role.** A role pulled in by such a task has no
    Task definition section if it lives outside the project tree. Needs the
    task walk in source.go to report these (also the FQCN spelling
    `ansible.builtin.include_role`, inside blocks and in role task files) and
    feed a worklist. A templated name (`name: "{{ app }}"`) can never be
    resolved by a static scan.
  * **meta/main.yml dependencies.** Same effect for dependency roles. Read
    `dependencies:` (bare names, or mappings with `role:`/`name:`) of every
    located role - project-local ones too, since they may depend on external
    roles - with a visited set against cycles. The easiest of the three.
  * **Roles inside collections.** `namespace.collection.role` (or a short
    name with the play's `collections:` keyword) lives under
    `<collections_path>/ansible_collections/<ns>/<coll>/roles/<role>/`.
    Neither `role` nor the source index searches it: `tangsible role
    community.general.foo` warns "couldn't find role" although ansible
    resolves it, and the Task definition tab stays empty. The run itself
    works. Needs the collections path (likely `ansible-config`'s
    COLLECTIONS_PATHS, confirmed: `ansible-config dump --format json` lists
    it as a plain string list, default `~/.ansible/collections` and
    `/usr/share/ansible/collections`, checked on core 2.19), FQCN splitting,
    and `collections:` handling; reuses the same worklist. Details:
      * The project's own `./collections/ansible_collections/...` (next to
        the playbook) is searched by ansible too and is the usual layout for
        vendored collections; add it ahead of COLLECTIONS_PATHS, the way
        `roles/` precedes roles_path.
      * Split an FQCN *before* the roles-path search. `rolepath.Find` accepts
        dots, so `ns.coll.foo` would otherwise be looked up as a plain
        directory name in roles_path.
      * `uikit.RolePathPattern` and `template.RoleDir` already match a
        collection role's `.../roles/<name>/tasks/` path, but report the
        bare `<name>`, not `ns.coll.<name>`. Check what the drill-down and
        `template`'s `vars_files` handling want to show/use for those.
      * A short name resolved via the play's `collections:` keyword is the
        same lookup, but the source walk doesn't read that keyword yet.

### Other gaps (found 2026-11-13)

  * **Galaxy-installed legacy roles** (`ns.rolename` directories under
    roles_path) already work, being plain directory names. Not a collection
    role; keep it that way when FQCN splitting is added (add a test).
  * **Search order (verified 2026-11-13** against ansible-core 2.19's
    `playbook/role/definition.py`, `_load_role_path`). Collection lookup
    comes first, if the name is an FQCN or the play has a `collections:`
    list. Otherwise: `<playbook dir>/roles`, then roles_path, then (only for
    dependency roles) the depending role's own `roles/` directory, then the
    playbook directory itself (`<playbook dir>/<name>`), and finally the name
    taken as a path. `rolepath.Find` matches the first two steps and omits
    the rest. The stub lives in the cwd, so the playbook-directory step
    would make `./<name>` resolve for ansible while tangsible warns "couldn't
    find role" - a harmless mismatch, but `Find` could add it. The
    dependency step belongs to the meta/main.yml worklist, where each
    role's own directory is that base.

Deliberately not done: `tangsible role` keeps taking exactly one bare role
name. No path arguments, and no special handling for running from inside the
role's own directory.

### Suggested order

1. meta/main.yml dependencies (easiest).
2. include_role / import_role, sharing one worklist with it.
3. Collections: FQCN splitting, COLLECTIONS_PATHS plus `./collections`, the
   `collections:` keyword.

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
