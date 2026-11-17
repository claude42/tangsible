tangsible-ee(1) "Tangsible" "Tangsible Manual"

# NAME

tangsible-ee - draft an execution-environment.yml for ansible-builder from a playbook

# SYNOPSIS

*tangsible ee init* [_playbook.yml_] [--base _image_] [--resolve-packages]

# DESCRIPTION

*ee init* writes a draft *execution-environment.yml* (ansible-builder schema
version 3) to the current directory. The stock execution environment images
carry almost no collections, so using one for real work means building a
custom image, and most of that definition is boilerplate. The draft fills in
what can be determined; everything else is marked with a *# TODO* comment.
It is a starting point to review and extend, not a complete definition, and
*tangsible* does not build the image (use *ansible-builder*(1) for that).

If _playbook.yml_ is omitted, it is resolved the same way as
*tangsible-run*(1) (see *tangsible*(1)).

It does the following:

- Reads the playbook and the roles it uses (*roles:*, *include_role*,
  *import_role*, role dependencies), and collects the module of every task,
  including handlers, *block*/*rescue*/*always* and the *action:* and
  *local_action:* forms. Modules only: filters, lookups and tests are not
  analyzed.
- Asks the host's Ansible (*ansible-doc*(1)) which collection owns each
  module, honoring *collections:* keywords. *ansible.builtin* modules are
  left out, as ansible-core ships with them. A module the host has no
  collection for, a templated module name and a templated *include_tasks* or
  *include_role* target become *# TODO* comments.
- Takes collections and roles from a *requirements.yml* (or *collections/requirements.yml*)
  next to the playbook or in the current directory, if there is one, and adds
  the collections the playbook uses that it doesn't list. Its versions win;
  otherwise a collection is pinned to the version installed on the host
  (*ansible-galaxy collection list*), or left unpinned with a comment.
- Pulls the base image if it is missing (using *pull_policy*) and inspects it
  with *podman* for its package manager, Python and ansible-core.
- Annotates each collection with where it was found.
- Suggests the name of the built image as a commented *tags* stanza
  (the project directory's name plus *-ee:latest*) to uncomment and adjust.
- Finds the lookup plugins the playbook uses (*lookup()*, *query()*, *q()* with a
  literal name, and *with_\**-loops, in task files, variable files,
  *group_vars* and Jinja templates under the playbook's directory); their
  collections are added, with the plugin's documented requirements as a
  comment. A name that is not a literal cannot be determined and becomes a
  single *# TODO*. Filters are not analyzed.
- Offers packages that used lookups and modules need and their collections
  don't declare (for example *dnspython* for the *dig* lookup, *rsync* for
  *synchronize*), from a small built-in table, as a commented *python:* and
  *system:* block to uncomment.
- Lists the binaries that *command* and *shell* tasks running on the
  controller (and so inside the execution environment) use and the base image
  lacks, as a comment block. Only tasks detected as running on the controller
  count: a play with *hosts: localhost* (or *127.0.0.1*, *::1*) or
  *connection: local*, or a task or block with *connection: local*,
  *delegate_to: localhost*, *ansible_connection: local* in its *vars*, or
  *local_action*. Tasks on other hosts are the targets' business. A role's
  task file is looked at without its play, so only the task-level conditions
  apply to it. The binary is the first word of each command of a shell line
  (builtins, shell keywords, assignments and wrappers like *sudo* are skipped);
  one that is templated cannot be determined and becomes a single *# TODO*.

The base image is, in this order: *--base* _image_, *base_image* under
*[execution_environment]* in *config.toml*, or the built-in default
(a pinned *ghcr.io/ansible-community/community-ee-base* tag, named in the
draft's header when used). *image* under *[execution_environment]* is never
used: it is the image tangsible runs, often the result of building this very
draft.

*--resolve-packages*
	Also look up which package provides each missing binary, and offer them as
	a *system:* block to uncomment (bindep syntax, with *[platform:rpm]* or
	*[platform:dpkg]*). This starts a throw-away container of the base image
	(the image itself is never changed) with network access, as root, and
	downloads repository metadata, so it can take from half a minute to a
	couple of minutes: *dnf* is queried on Fedora/RHEL-family images (the image
	needs *dnf*; *microdnf* can't answer this) and *apt-file* on
	Debian/Ubuntu-family images. A binary with several candidate packages is
	listed with all of them, one with none as a *# TODO*. If the lookup can't be
	done (no network, no *dnf*), the binaries are only listed. The
	*network* key under *[execution_environment]* is used for the container
	(*host* fixes DNS trouble). Nothing is ever added without you uncommenting it.

If *execution-environment.yml* already exists, *ee init* fails and writes
nothing.

# EXIT STATUS

*0*
	The draft was written.

*1*
	No draft was written: the file already exists, *podman* is missing, the
	base image couldn't be pulled or inspected, or the playbook couldn't be
	read.

*2*
	Usage error.

# SEE ALSO

*tangsible*(1), *tangsible-run*(1), *ansible-builder*(1)
