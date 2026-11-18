tangsible-ee(1) "Tangsible" "Tangsible Manual"

# NAME

tangsible-ee - draft an execution-environment.yml from a playbook, check it, or build it

# SYNOPSIS

*tangsible ee* <init | verify | build> [_playbook.yml_] [_options_ ...]

*tangsible ee init* [_playbook.yml_] [--base _image_] [--resolve-packages]++
*tangsible ee verify* [_playbook.yml_] [-f _file_] [--keep] [--no-cache] [--show-output] [_ansible-playbook args_ ...]++
*tangsible ee build* [-f _file_] [-t _tag_ ...] [--no-cache] [--show-output]

# DESCRIPTION

*ee init* drafts an execution-environment.yml; *ee verify* builds one into a
throw-away image and checks it (see *VERIFY* below); *ee build* builds it into
the image it names and keeps it (see *BUILD* below).

## INIT

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

# VERIFY

*ee verify* builds an *execution-environment.yml* (_file_, default
*./execution-environment.yml*, normally the draft after you have edited it) into
a throw-away image and checks that image against what the playbook uses. It
needs *podman* and *ansible-builder* (*tangsible version* shows both). If
_playbook.yml_ is omitted it is resolved like *tangsible-run*(1); other
arguments, such as *-i* or *-e*, are passed to *ansible-playbook* for the
syntax check. *--ask-pass*, *--ask-become-pass* and *--ask-vault-pass* are not
supported: a syntax check needs no secrets, use a vault password file.

- It builds the image with *ansible-builder build* under a private tag
  (*localhost/tangsible-ee-verify:...*), which overrides the *tags* of the
  definition, so an image you named is never created or overwritten. The
  *network* key under *[execution_environment]* is used for the build too
  (*host* fixes DNS trouble). A build takes minutes; its output goes to
  *.tangsible/ee-verify.log*, and a failed build shows the end of it.
- In the built image it runs *ansible-playbook --syntax-check*, the way a
  real *tangsible run* would run it (the same mounts and environment).
- It reports the image's ansible-core version next to the host's, as
  information only (a run that behaves differently in the container often
  differs in the version).
- In one more run it asks the image's *ansible-doc* about *every* module and
  lookup the playbook uses (so all missing ones are listed, not only the first
  one the syntax check stops at) and checks that the binaries used by
  *command*/*shell* tasks that run on the controller exist.
- It removes the image and the build context, unless *--keep* is given, which
  leaves the image and prints its name. podman's build cache stays (see
  *--no-cache*). A build that fails, or is interrupted
  with Ctrl-C, can leave working containers and a temporary image behind; those
  are removed too (only what the build created: nothing that was there before is
  touched, so do not run another podman build at the same time).

This catches missing collections, a broken build, missing ansible-core or
ansible-runner and missing binaries. It does not catch Python libraries that a
plugin imports only when it runs, or anything a syntax check does not execute.

*-f* _file_, *--file* _file_
	The definition to build, instead of *execution-environment.yml*.

*--keep*
	Keep the built image and print its name.

*--show-output*
	Stream the build's complete output to the terminal (stderr) instead of
	the status line. The status line, shown while the build runs when stderr is
	a terminal, has the elapsed time, podman's latest *[stage/stages] STEP
	step/steps* marker and the latest line of output; it is erased when the
	build ends. There is no percentage: podman does not tell how many steps a
	stage has until it gets there. The full output always goes to the log.
	(*-v* and *--verbose* are not used for this: *ee verify* passes them to
	*ansible-playbook*.)

*--no-cache*
	Build without podman's build cache. By default the cache is used and kept
	(untagged stage images, which make a second build faster but take disk
	space; *podman image prune* removes them), so a layer that installs
	unpinned packages is not re-run. Use this to confirm the definition builds
	from scratch; nothing is left behind.

# BUILD

*ee build* builds an *execution-environment.yml* (_file_, default
*./execution-environment.yml*) with *ansible-builder build* and keeps the image.
It needs *podman* and *ansible-builder*, takes no playbook and runs no checks
(*ee verify* does). The image name is *-t* _tag_, else the definition's
*options.tags*; with neither it stops before building (*ansible-builder*
would otherwise create *ansible-execution-env:latest*; *ee init*'s draft has a
commented *tags* stanza). The build uses a private temporary context, so no
*context/* directory appears in the project; the *network* key under
*[execution_environment]* is used for it (*host* fixes DNS trouble); its output
goes to *.tangsible/ee-build.log*, and a failed build shows the end of it.

On success it prints each tag with the image id and size, notes a tag that
moved off an older image (*podman image prune* removes the untagged one) and
prints the *image = "..."* line to use it, which is never written to
*config.toml*. A build that fails or is interrupted with Ctrl-C leaves
nothing behind except podman's build cache; an existing tag is untouched.

*-f* _file_, *--file* _file_
	The definition to build, instead of *execution-environment.yml*.

*-t* _tag_, *--tag* _tag_
	The image name; repeatable (one build, several names). Replaces the
	definition's *options.tags*.

*--show-output*
	Stream the build's complete output instead of the status line, as for
	*ee verify*.

*--no-cache*
	Build without podman's build cache, as for *ee verify*.

# EXIT STATUS

*0*
	*ee init*: the draft was written. *ee verify*: the build and every check
	passed. *ee build*: the image was built.

*1*
	*ee init*: no draft was written (the file already exists, *podman* is
	missing, the base image couldn't be pulled or inspected, or the playbook
	couldn't be read). *ee verify*: the build or a check failed, or it
	couldn't start (a missing tool or file).
	*ee build*: the build failed, or it couldn't start (a missing tool or file, or
	no image name).

*2*
	Usage error.

*130*
	*ee verify* or *ee build* was interrupted.

# SEE ALSO

*tangsible*(1), *tangsible-run*(1), *ansible-builder*(1), *podman*(1)
