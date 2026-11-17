# execution-environment.yml templates

## Situation

The standard ee images usually don't contain any additional collections, not
even community.general. So in most cases one has to build a custom image.
This involves generating an execution-environment.yml file as input for
ansible-builder.

Getting this right will take a while and there's quite a bit of boilerplate
code that's required. E.g. determining all the collections which need to go
into the image is something that could be easily done automatically.

## Idea

Create a tool (read: tangsible functionality) that creates a basic
execution-environment.yml as a basis for all the additional stuff that the
user might want to add to it. Idea is not to make this fully automatic
because the different environments will very to much.

### How it could work

(first rough idea, not a final decision yet)

* tangsible reads playbook and all role it uses and extracts all used modules
* examine the base image, extract e.g. python version
* then create first draft execution-environment.yml with the known data
  already filled in

### Answers to questions

1. new verb, maybe tangsible ee init
  * do not fold into run, it's standalone
* its only job is to create a draft execution-environment.yml
2. building is not part of this discussion, let's assume for now,
  ansible-builder will do the building step
* The --verify idea (see below) is only meant to do basic verification, not a
  final build.
  * Let's keep the --verify idea out of v1
3. so at this point, ansible-builder will not be needed yet
4. output location current directory, if file already exists, don't overwrite

5. Analysis: I actually don't know what the best way  to do it is.
  * Not sure if I understand you correctly. I'd use the host's collection as
    the source of truth. If one is present, then add a # TODO for now (maybe
    we do something more sophisticated later on
6. Yeah, let's do modules-only for v1
7. Yes, if there's already a requirements.yml, then use it
8. I'm all for code re-use

9. Fail if podman is not available
10. --base <image>, then base_image in config, then image as a last resort
11. Use builder schema v3 only

12. Comments are good, but should be terse, references to files sound good.
    Do it and then we can determine if it's getting too much
13. Do the pinning thing. We're creating a draft for a reason, user can
    decide whether to modify it. Maybe add a comment saying so
14. Not in v1
15. Batch - at least for now

16. Not sure if I understand the question. But completeless in the sence of a
    full functional execution-environments.yml is not the goal. We target
    podman-only, Docker is not a target.
17. Galaxy auth, private automation hub and ansible.cfg server settings
    definitely out of scope for now



### Thoughts from another discussion

The next paragraph is copy/pasted from another discussion we had. These were
your initial thoughts on that topic. I put them here as input for our further discussion.

Yes, I think it makes sense. As far as I know, nothing generates an EE definition from a playbook. The existing tools analyze content or build from a definition, but none connects the two. A draft generator that is honest about what it doesn't know is useful, because it removes the tedious 80% and leaves you reviewing instead of writing from scratch.

**Playbook and role analysis**
- Walk plays, tasks, handlers, `block`/`rescue`/`always`, `include_role`/`import_role`, `roles:` entries, and `meta/main.yml` dependencies. Don't forget the legacy `action:` and `local_action:` forms.
- **Resolve short names, not just FQCNs.** Plays and roles can set `collections:`, so `docker_container` may mean `community.docker.docker_container`. Many unqualified names also resolve through ansible-core's own redirect table (`ansible_builtin_runtime.yml`), so use that rather than guessing.
- Filters and lookups are the hard part. Jinja filter names live in templates and vars, so you'd regex them out and map them to collections. That works best when you can run `ansible-doc -t filter --list` in an environment where the collections are installed.

**Base image inspection**
- Running `podman run --rm <image> ...` is the most reliable approach: read `/etc/os-release` to pick the package manager, then check `python3 --version`, whether pip exists, and whether ansible-core is already present.
- Without a container runtime, a small lookup table for common bases (Fedora, Debian, ubi) is a decent fallback.
- This feeds `package_manager_path`, `python_interpreter`, and the choice of bindep selectors.

**Where the draft gets its value**
- **Implied-dependency rules**: `json_query` → `jmespath`, `password_hash` → `passlib`, `synchronize` → `rsync`, `sshpass` for password SSH. It's a small, editable rule table.
- **Compatibility check**: compare each collection's `requires_ansible` (in `meta/runtime.yml`) with the ansible-core version you pin, and warn on conflicts.
- **Review hints**: binaries found as the first word of `command`/`shell` tasks should be listed as commented suggestions, not added automatically. Static analysis can't tell whether they're needed in the EE or on the target host.
- **Unresolvable items**: dynamic module names and variable `include_tasks` paths should appear as `# TODO` comments in the output, so nobody mistakes the draft for complete.

**Scope advice**
- Keep the promise at "draft with annotated uncertainty." Trying to guarantee completeness is where tools like this become fragile and over-engineered.
- An optional `--verify` step that builds the image and runs `ansible-playbook --syntax-check` inside it would catch a lot of the remaining gaps cheaply.
- Pin versions from the currently installed environment if there is one, and otherwise leave them unpinned with a comment.

