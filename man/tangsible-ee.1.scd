tangsible-ee(1) "Tangsible" "Tangsible Manual"

# NAME

tangsible-ee - draft an execution-environment.yml from a playbook, check it, build it, or push it

# SYNOPSIS

*tangsible ee* <init | verify | build | push> [_options_ ...] [_playbook.yml_]

*tangsible ee init* [--base _image_] [--resolve-packages] [--no-backup] [--no-galaxy] [--latest] [-t _tag_ ...] [_playbook.yml_]++
*tangsible ee verify* [-f _file_] [--keep] [--no-cache] [--show-output] [_ansible-playbook options_ ...] [_playbook.yml_]++
*tangsible ee build* [-f _file_] [-t _tag_ ...] [--no-cache] [--show-output]++
*tangsible ee push* [-f _file_] [-y | --yes]

# DESCRIPTION

*ee init* drafts an execution-environment.yml; *ee verify* builds one into a
throw-away image and checks it (see *VERIFY* below); *ee build* builds it into
the image it names and keeps it (see *BUILD* below); *ee push* uploads the
image names a definition lists to their registry (see *PUSH* below).

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
  (*ansible-galaxy collection list*); one the host lacks gets the newest
  version on Galaxy that is not a pre-release and whose *requires_ansible*
  admits the image's ansible-core, marked *from Galaxy* in the draft, or is left
  unpinned with a comment saying why (Galaxy unreachable, not found, no version
  admitted). A collection Galaxy flags deprecated is marked. Galaxy is not asked
  when the host has *GALAXY_SERVER_LIST* configured, and never fails the
  command.
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
  *synchronize*), from a small built-in table, and adds them to
  *dependencies.python* and *dependencies.system*; delete one you don't want.
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
	Also look up which package provides each missing binary. A binary with exactly
	one providing package is added to *dependencies.system* (bindep syntax, with *[platform:rpm]* or
	*[platform:dpkg]*). This starts a throw-away container of the base image
	(the image itself is never changed) with network access, as root, and
	downloads repository metadata, so it can take from half a minute to a
	couple of minutes: *dnf* is queried on Fedora/RHEL-family images (the image
	needs *dnf*; *microdnf* can't answer this) and *apt-file* on
	Debian/Ubuntu-family images. A binary with several candidate packages is
	left as a *# TODO* naming them all, one with none as a *# TODO* too; delete
	an added entry you don't want. If the lookup can't be
	done (no network, no *dnf*), the binaries are only listed. The
	*network* key under *[execution_environment]* is used for the container
	(*host* fixes DNS trouble). Nothing else is added without you uncommenting it.

If *execution-environment.yml* already exists it is replaced. When the new draft
differs, the old file is first kept as *execution-environment.yml.*_N_, where _N_
is the highest existing number plus one (1 for the first); only the five
highest-numbered backups are kept, the lowest are removed. When the content is
identical nothing is written and no backup is made. The new draft is generated
from scratch: your edits are in the backup, not merged. The file is replaced
atomically (a temporary file is written, the backup is a hard link, then a
rename), so an interrupted run never leaves a missing or half-written file. A
symlink or a directory at that name is an error.

*--no-backup*
	Replace an existing file without keeping its old content; existing
	backups are left alone and no backup is made or removed.

*--no-galaxy*
	Do not ask Galaxy for collection versions: a collection the host lacks is
	left unpinned, as before. (The image pull and *--resolve-packages* still
	use the network.)

*--latest*
	Ask Galaxy for every collection and use its newest compatible version
	instead of the host's installed one. A *requirements.yml* version still
	wins. Contradicts *--no-galaxy*.

*-t*, *--tag* _tag_
	Write _tag_ into the definition's *options.tags* (repeatable) instead of the
	commented suggestion, so *ee build* needs no *-t*.

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

# PUSH

*ee push* uploads the image names listed in the definition's *options.tags*
(_file_, default *./execution-environment.yml*) with *podman push*: one push per
name, in order. It needs *podman* only. An image is pushed by name, and the
registry is in the name (*registry.example.com/team/my-ee:1.0*); one image can
carry several names (*:1.0*, *:latest*), the layers go up once. A name with no
registry (*std-ee:latest*) is local-only: podman keeps it as *localhost/...*,
which cannot be pushed, so *ee push* leaves it out and lists it as not pushed.
It is an error when no name has a registry.

Before pushing anything it checks the other names (not *localhost/*, the image
present here) and lists all problems at once. It then prints what it will push,
grouped by registry with each image's id and size, and the local-only names it
leaves out, and asks *Proceed? [y/N]*;
only *y* or *yes* goes on. Without a terminal it stops unless *--yes* is given.
It stops at the first failed push and says which names were already pushed.

Credentials are podman's: log in once with *podman login* _registry_ and
*podman push* finds the login (*REGISTRY_AUTH_FILE*, *auth.json*, credential
helpers); nothing is read or stored by *tangsible*, and nothing goes into
*config.toml*. A CI job can set *REGISTRY_AUTH_FILE* or pipe the secret into
*podman login --password-stdin*. By default *podman login* keeps its token
under *$XDG_RUNTIME_DIR*, which is cleared at reboot; giving it *--authfile*
and a file such as *~/.config/containers/auth.json* keeps it. When a push fails
for lack of a login, *tangsible* adds the line
*not logged in? run: podman login* _registry_.

*-f* _file_, *--file* _file_
	The definition whose names are pushed, instead of
	*execution-environment.yml*.

*-y*, *--yes*
	Do not ask for confirmation.

# EXIT STATUS

*0*
	*ee init*: the draft was written. *ee verify*: the build and every check
	passed. *ee build*: the image was built. *ee push*: every name was pushed.

*1*
	*ee init*: no draft was written (the file is a symlink or a directory, *podman* is
	missing, the base image couldn't be pulled or inspected, or the playbook
	couldn't be read). *ee verify*: the build or a check failed, or it
	couldn't start (a missing tool or file).
	*ee build*: the build failed, or it couldn't start (a missing tool or file, or
	no image name). *ee push*: a name or image is unfit to push, the answer was no,
	there was no terminal to ask on and no *--yes*, or a push failed.

*2*
	Usage error.

*130*
	*ee verify*, *ee build* or *ee push* was interrupted.

# SEE ALSO

*tangsible*(1), *tangsible-run*(1), *ansible-builder*(1), *podman*(1), *podman-push*(1), *podman-login*(1)
