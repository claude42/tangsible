# Tangsible

**An interactive TUI workbench for Ansible.**

Why did that task fail? Where did that variable come from? What is Ansible about to do on this host? Why does this template not render as expected? And what's needed in this execution environment to make this playbook run?

Working with Ansible often means juggling commands, scrolling through output, adding temporary debug tasks and writing scratch playbooks. Tangsible brings these workflows together in one interactive terminal workbench for Ansible.

## See it in action

<table>
  <tr>
    <td width="33.33%" valign="top">
      <a href="https://gettangsible.aw.net/run.gif"><img src="https://gettangsible.aw.net/run.png" width="100%" alt="Run playbooks"></a>
      <b>Run playbooks</b>
    </td>
    <td width="33.33%" valign="top">
      <a href="https://gettangsible.aw.net/debug.gif"><img src="https://gettangsible.aw.net/debug.png" width="100%" alt="Interactive debuggger"></a>
      <b>Interactive debugger</b>
    </td>
    <td width="33.33%" valign="top">
      <a href="https://gettangsible.aw.net/stats.gif"><img src="https://gettangsible.aw.net/stats.png" width="100%" alt="Show statistics"></a>
      <b>Show statistics</b>
    </td>
  </tr>
  <tr>
    <td width="33.33%" valign="top">
      <a href="https://gettangsible.aw.net/ee.gif"><img src="https://gettangsible.aw.net/ee.png" width="100%" alt="Execution environments"></a>
      <b>Execution environments</b>
    </td>
    <td width="33.33%" valign="top">
      <a href="https://gettangsible.aw.net/template.gif"><img src="https://gettangsible.aw.net/template.png" width="100%" alt="Render and debug Jinja2 templates"></a>
      <b>Render and debug Jinja2 templates</b>
    </td>
    <td width="33.33%" valign="top">
      <a href="https://gettangsible.aw.net/hosts.gif"><img src="https://gettangsible.aw.net/hosts.png" width="100%" alt="Explore host facts and variables"></a>
      <b>Explore host facts and variables</b>
    </td>
  </tr>
</table>


## What can you do with Tangsible?

### Run and debug playbooks

- **Run and inspect playbooks interactively.** Follow execution in a navigable tree of plays, tasks and hosts, with live updates as the playbook runs.
- **Drill into task results.** Inspect task source, stdout/stderr, diffs and resolved variables without losing your place.
- **Debug execution.** Set breakpoints, step through tasks and hosts, inspect variables, add expressions to a watch list and change variable values while execution is paused.
- **Re-run and compare.** Re-run a complete playbook or a selected part of it, revisit previous runs and compare their results.

### Explore hosts and inventories

- **Inspect hosts.** Browse facts, variables (including where they are defined), group membership and the plays that target a host.
- **Explore inventories.** Navigate groups and hosts in a collapsible tree, or open a host directly for a detailed view.

### Develop and troubleshoot

- **Test roles in isolation** without having to create a temporary playbook.
- **Render and debug Jinja2 templates** using Ansible's own templating system and the variables of an actual inventory host. Edit the template and render it again interactively.
- **Manage encrypted variables.** Add, edit and remove individual Ansible Vault-encrypted variables without manually handling encrypted strings.

### Work with execution environments
(not yet released)

- **Run playbooks and roles in an EE** to use the same Ansible Core and collection versions as another environment, such as CI or AWX.
- **Generate an EE definition from a playbook.** `tangsible ee init` analyzes modules, collections, lookups and controller-side dependencies to create a starting `execution-environment.yml`.
- **Verify the definition.** Build a temporary image and check it against the playbook before committing to a final build.
- **Build and push images.** Build the configured image tags and publish them to a container registry.

## Installation

### One-line installer

Interactive:

```sh
curl -fsSL https://gettangsible.aw.net/ | sh
```

Unattended:

```sh
curl -fsSL https://gettangsible.aw.net/ | sh -s -- --yes
```

The installer uses the user's home directory (`~/.local/bin` and `~/.local/share`) by default and also supports `--prefix`.

### Binary archive

Download an archive from [Releases](https://code.aw.net/claude/tangsible/releases) and install it using the included `install.sh`.

### Debian / Ubuntu

Releases are published to an APT repository for Debian 12+ and Ubuntu 22.04+ (amd64 and arm64). `ansible-core` is installed as a package dependency.

```sh
sudo curl -fsSL https://code.aw.net/api/packages/claude/debian/repository.key \
  -o /etc/apt/keyrings/tangsible.asc

echo "deb [signed-by=/etc/apt/keyrings/tangsible.asc] https://code.aw.net/api/packages/claude/debian stable main" \
  | sudo tee /etc/apt/sources.list.d/tangsible.list

sudo apt update
sudo apt install tangsible
```

Each release also includes `.deb` and `.rpm` packages for manual installation.

### Go install

```sh
go install code.aw.net/claude/tangsible@latest
```

This installs the binary into `$GOPATH/bin` (usually `~/go/bin`).

### Build from source

```sh
git clone https://code.aw.net/claude/tangsible
cd tangsible
go build ./...
```

This produces a `tangsible` binary in the current directory. The included `install.sh` can be used to install it.

For system-wide installation, `install.sh --prefix /usr/local` installs Tangsible for all users when run as root. For distro packaging, see [`PACKAGING.md`](PACKAGING.md).

## Requirements

- Ansible Core
- Podman - only required for running playbooks inside execution environments.
- Ansible Builder - only required for creating new execution environments.
- A real terminal (TTY). Tangsible opens a full-screen interface and cannot run in a piped or headless shell.
- Go 1.24+ to build from source.

Tangsible bundles its Ansible callback plugins, so no additional collection installation is required.

## Quick start

Run a playbook on the host:

```sh
tangsible run -i inventory.ini site.yml
```

The `run` verb is optional: `tangsible -i inventory.ini site.yml` does the same, and a bare `tangsible` opens the run dialog for `./site.yml` (or your `default_playbook`) and starts when you confirm it.

Pass Ansible arguments as you normally would:

```sh
tangsible run -i inventory.ini --limit webservers --tags deploy  site.yml
```

Run it in an execution environment instead (not released yet):

```sh
tangsible run -i inventory.ini --ee ghcr.io/ansible-community/community-ee-base:latest site.yml
```

Tangsible passes Ansible arguments through to `ansible-playbook`, so normal inventory, tag, limit and verbosity options continue to work.

If you don't specify a playbook, Tangsible can resolve one from its configuration. See [Configuration](#configuration).

## Commands and workflows

| Command | What it does |
|---|---|
| `tangsible run` | Run a playbook and interactively inspect its execution |
| `tangsible debug` | Debug playbook execution with breakpoints, stepping and variable inspection |
| `tangsible rerun` | Repeat the last run after adjusting hosts, tags, … |
| `tangsible revisit` | Browse and compare previous runs |
| `tangsible role` | Run a role in isolation |
| `tangsible template` | Render and debug a Jinja2 template |
| `tangsible host` / `hosts` | Inspect one host or browse available hosts |
| `tangsible inventory` | Explore the inventory as a tree |
| `tangsible vault` | Edit individual encrypted variables |
| `tangsible ee init` | Generate an execution-environment definition from a playbook |
| `tangsible ee verify` | Build and check an EE definition against a playbook |
| `tangsible ee build` | Build an execution-environment image |
| `tangsible ee push` | Push configured image tags to their registries |

## Run and debug playbooks

### Inspect a live run

```sh
tangsible run playbook.yml
```

Tangsible presents plays, tasks and hosts in a navigable tree. Outcomes such as OK, Changed, Skipped, Failed and Unreachable are visible at a glance. Drill into a result to inspect the task source, output, stderr, diff and full result. Show ansible-playbook's original output and show statistics.

Filter by outcome or search task names, source and output. Keyboard and mouse navigation are supported, as are desktop notifications, terminal-title progress and clipboard integration.

### Debug execution

```sh
tangsible debug [playbook.yml]
```

Set breakpoints with `F9` or the mouse, then use:

- `F5` to run or continue to the next breakpoint.
- `F10` to move to the next task.
- `F11` to step to the next pause, including the next host within a task.
- `F6` to stop while it runs: the current task finishes and you are in the debugger right there, as at a breakpoint (`q` does the same while running; Ctrl-C ends the run). It can only stop between tasks, never inside one.
- `F12` to redo the current task/host.
- `r` to start over: opens a small dialog (tags, skip tags, limit hosts); Enter takes the session back to the idle state with nothing run, rebuilt from the playbook and roles as they are on disk now (so edits show), breakpoints kept. A paused run is ended by it, which the dialog says. Works in the tree and in the drill-down.
- `v` to open the variable watch explorer.

While execution is paused, inspect variables, add expressions to the watch list and change variable values. Click on variables to see their value and origin. A **Resolved** tab shows the complete task with all variables replaced by their values.

### Re-run, revisit and compare

After a run, press `r` to open the rerun dialog. You can run the playbook again, start at a selected task, change the target hosts or adjust the tasks to execute.

The same workflow is available from the command line:

```sh
tangsible rerun [playbook.yml] 
```

Previous executions can be opened with:

```sh
tangsible revisit
```

Revisited runs can be inspected in the same way as a live run. Compare a run with an earlier execution, analyze the differences, or rerun it.

Previous runs can be renamed, deleted or exported. Exported files can be re-imported with:

```sh
tangsible revisit exported-run.txt
```

## Explore hosts and inventories

Inspect one host:

```sh
tangsible host <hostname> [playbook.yml]
```

Browse all hosts:

```sh
tangsible hosts [playbook.yml]
```

The host view brings together information that is otherwise spread across inventory files and Ansible commands:

- Host data: FQDN, IP addresses, operating system and architecture.
- Collected facts.
- Variables, including where they are defined.
- Direct and inherited group membership.
- Plays that will run for the host.

Explore the inventory as a tree:

```sh
tangsible inventory [playbook.yml]
```

Groups are shown as collapsible nodes and hosts as leaves. Opening a host displays the same detailed view as `tangsible host`.

## Develop and troubleshoot

### Run a role in isolation

```sh
tangsible role <role-name>
```

Tangsible creates the temporary playbook wrapper needed to run the role against `hosts: all`. Normal Ansible options such as `-i` and `-l` can be used to select the target hosts.

Roles can be found in the project's `roles` directory or on Ansible's configured roles path. Roles in collections can be specified as `namespace.collection.role`.

### Render and debug a Jinja2 template

```sh
tangsible template <path-to-template> [<hostspec>] [-e ...]
```

Tangsible renders the template through Ansible's own templating system, using the variable and inventory context of an actual host. Extra variables can be supplied with `-e`. If rendering fails, the Jinja error is shown.

For templates in a conventional role template directory, the role's variables are made available automatically. Press `e` to edit the template in `$VISUAL` or `$EDITOR`; Tangsible renders it again after you save.

### Edit individual Vault-encrypted variables

```sh
tangsible vault <filename>
```

This provides an editor-based workflow for adding, editing and removing individually encrypted variables. Unlike encrypting an entire file, changes can remain limited to the variables that were edited, keeping diffs manageable.

The password comes from `--vault-password-file`, `--ask-vault-pass`, `$ANSIBLE_VAULT_PASSWORD_FILE`, your `ansible.cfg`, or a prompt. Several vault ids work as in Ansible: pass `--vault-id dev@dev.pw --vault-id prod@prompt` (or set `vault_identity_list` in `ansible.cfg`), and `--encrypt-vault-id` for the id new values get. Each value keeps its vault id when saved. A value none of your passwords opens shows as a `<vaulted: prod>` placeholder and is saved unchanged.

## Execution environments
(not yet released)

Execution environments let you run Ansible with a defined Ansible Core version and set of collections, rather than relying on whatever happens to be installed on the host. This is useful when checking that a playbook or role works with the environment used by CI or AWX.

Tangsible uses Podman on Linux to run EEs. Docker and macOS are not supported at this point. It does not require `ansible-runner`.

### Run in an existing EE

Pass an image with `--ee`:

```sh
tangsible run -i hosts --ee ghcr.io/ansible-community/community-ee-base:latest [playbook.yml]
```

The same EE support is available for `tangsible role`. It is not currently available for `tangsible debug`.

### Generate an EE definition

Stock EE images generally contain few collections. `tangsible ee init` creates a starting `execution-environment.yml` based on the modules and dependencies used by a playbook and its roles:

```sh
tangsible ee init [--base <image>] [--resolve-packages] [--offline] [--latest] [-t <tag> ...] [playbook.yml]
```

Without `--base` (or `base_image` in the config), `ee init` looks up the community base image whose ansible-core matches your host's (the newest release of the same series if there is no exact match) and pins collections to the versions installed on your host, asking Galaxy only for those you lack. `--latest` takes the newest base image and the newest compatible collection versions instead. If the registry or Galaxy cannot be reached, the built-in default image and unpinned collections are used; `--offline` skips the lookups.

The generated file is a draft, not a guarantee that every dependency can be inferred. Anything that cannot be determined statically is left as a `# TODO` for you to review.

### Verify an EE definition

After reviewing and editing the generated definition, verify it against the playbook:

```sh
tangsible ee verify [playbook.yml]
```

It reports missing dependencies and exits with status 1 if checks fail. Verification catches many dependency and syntax problems before a real run, but it cannot detect failures that only occur when a task actually executes.

### Build the image

Once the definition is ready, build the image with:

```sh
tangsible ee build
```

### Push the image

```sh
tangsible ee push [-f execution-environment.yml] [-y | --yes]
```

Tangsible pushes the image names listed in `options.tags` to their registries.

Before pushing, Tangsible checks the image names and shows what it is about to upload. Confirm with `Proceed?`, or use `--yes` to skip the confirmation.

Authentication is handled by Podman, not by Tangsible. Log in beforehand:

```sh
podman login registry.example.com
```

## Configuration

When no playbook is given on the command line, Tangsible looks for one in this order:

1. `$TANGSIBLE_PLAYBOOK`
2. `.tangsible/config.toml` in the current directory.
3. `$XDG_CONFIG_HOME/tangsible/config.toml` (default: `~/.config/tangsible/config.toml`).
4. `/etc/tangsible/config.toml`.
5. `./site.yml`, if present.

Project, user and system configuration files use the same TOML format. Settings cascade from project to user to system configuration; a more specific setting takes precedence when it is set.

For example:

```toml
[general]
default_playbook = "myplaybook.yml"
default_tree_state = "collapsed"

[execution_environment]
image = "registry.example.com/team/my-ee:latest"
base_image = "ghcr.io/ansible-community/community-ee-minimal:latest"
network = "host"
```

`image` is the default image used to run playbooks and roles. `base_image` is used by `tangsible ee init` when generating a definition; it is not the image used to execute playbooks. Check this [example config](https://code.aw.net/claude/tangsible/raw/branch/main/examples/config.toml) for a detailed description of all configuration settings.

## Current limitations

Tangsible focuses on interactive workflows rather than complete `ansible-playbook` feature parity.

- **Execution environments:** Supported on Linux with Podman for `run`, `rerun` and `role`. Docker, macOS and `debug` are not supported for EE execution. `ansible-runner` is not required or supported.
- **Interactive input:** Interactive input after the TUI starts is not supported in `run`, `rerun` or `role`. For example, a bare `pause` task will not wait for input, and `--step` will not work. Use `tangsible debug` for the interactive debugging workflow.\
  Timed pauses work normally. `--ask-become-pass`, `--ask-vault-pass` and `--ask-pass` are supported: Tangsible prompts before opening the TUI and supplies Ansible with temporary password files, which are removed on exit.
- **Scale:** Tangsible is developed and tested primarily with relatively small development inventories. It is not intended as a fleet-management interface.
- **CLI parity:** Only a limited subset of `ansible-playbook` options is interpreted specially by Tangsible; other arguments are passed through to Ansible.

## Development

Tangsible is written in Go.

```sh
go build ./...
go vet ./...
go test ./...
```

The callback plugin is embedded at build time, so `go run .` works without installing it separately. To try a modified callback plugin without rebuilding, point `$TANGSIBLE_CALLBACK_DIR` at a directory containing it:

```sh
TANGSIBLE_CALLBACK_DIR="$PWD/callback" go run . run site.yml
```

End-to-end smoke tests use a real binary in a `tmux` pane and are excluded from the normal test run:

```sh
go test -tags e2e ./...
```

The callback plugin has its own Python test suite in `callback-tests/`:

```sh
python3 -m unittest discover -s callback-tests -v
```

## License

Tangsible and its bundled Ansible callback plugin are licensed under Apache-2.0. See [LICENSE](LICENSE).

## Contact

`tangsible@aw.net`
