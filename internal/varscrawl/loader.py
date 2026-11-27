# Lists every place a host's variables are defined, in ansible's own order of
# precedence, without running anything (design-docs/Variables.md). Run by
# tangsible as `python3 -c <this file> <callback helper> <host> <ansible-
# playbook-or-- args...>`; prints JSON on stdout, errors go to stderr with a
# non-zero exit status. Built on ansible-core internals (the same calls
# VariableManager.get_vars makes, one source at a time): see
# design-docs/AnsibleInternals.md.
import importlib.util
import json
import os
import re
import sys

from ansible import constants as C
from ansible.cli.playbook import PlaybookCLI
from ansible.inventory.group import Group
from ansible.playbook import Playbook
from ansible.module_utils.common.text.converters import to_text
from ansible.plugins.loader import init_plugin_loader
from ansible.vars.plugins import get_vars_from_path
from ansible.vars.manager import VariableManager  # noqa: F401  (import check)


def load_helper(path):
    spec = importlib.util.spec_from_file_location("tangsible_events_helper", path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def sort_groups(groups):
    # ansible.vars.manager.sort_groups
    return sorted(groups, key=lambda g: (g.depth, g.priority, g.name))


def top_level_line(path, name):
    """The line a top-level key of a YAML file is written on, 0 if not found."""
    pattern = re.compile(r"^%s\s*:" % re.escape(name))
    try:
        with open(path, encoding="utf-8") as handle:
            for number, line in enumerate(handle, 1):
                if pattern.match(line):
                    return number
    except OSError:
        pass
    return 0


def describe(helper, variables, files=()):
    """{name: {value, path, line | desc}} for a layer, vaulted values masked.

    A value of a type ansible cannot tag (a boolean, null) has no origin of its
    own; for those the layer's files are searched for the key, last file
    first (the later file wins, as in ansible).
    """
    if not variables:
        return {}
    result = helper.tangsible_variables(dict(variables))
    out = {}
    for name, value in result["variables"].items():
        entry = {"value": value}
        entry.update(result["origins"].get(name, {}))
        out[name] = entry
    for name, value in variables.items():
        name = str(name)
        if name in out or not (value is None or isinstance(value, bool)):
            continue
        entry = {"value": value}
        for path in reversed(list(files)):
            line = top_level_line(path, name)
            if line:
                entry.update({"path": path, "line": line})
                break
        out[name] = entry
    return out


def inventory_dirs(inventory, loader):
    dirs = []
    for source in inventory._sources:
        if not source or "," in source:
            continue
        dirs.append(os.path.dirname(source) or loader.get_basedir())
    return dirs


def main():
    helper_path, hostname, playbook_arg = sys.argv[1], sys.argv[2], sys.argv[3]
    # "-" means no playbook: only the inventory-side sources are listed.
    # ansible-playbook insists on a positional argument, so it gets a dummy.
    cli_args = ["ansible-playbook"] + sys.argv[4:] + [playbook_arg if playbook_arg != "-" else "-"]
    init_plugin_loader()
    cli = PlaybookCLI(cli_args)
    cli.parse()
    loader, inventory, variable_manager = cli._play_prereqs()
    helper = load_helper(helper_path)

    host = inventory.get_host(hostname)
    if host is None:
        raise SystemExit("host %r is not in the inventory" % hostname)

    from ansible import context
    playbooks = [] if playbook_arg == "-" else list(context.CLIARGS.get("args") or [])
    plays = []
    notes = []
    playbook_dir = loader.get_basedir()
    if playbooks:
        pb = Playbook.load(playbooks[0], variable_manager=variable_manager, loader=loader)
        playbook_dir = pb._basedir  # what ansible-playbook sets the loader's basedir to
        loader.set_basedir(playbook_dir)
        for play in pb.get_plays():
            pattern = play.hosts
            if isinstance(pattern, (list, tuple)):
                pattern = ",".join(str(p) for p in pattern)
            try:
                matched = [h.name for h in inventory.get_hosts(str(pattern))]
            except Exception as exc:  # noqa: BLE001 - a templated pattern
                notes.append("play %r: host pattern %r not evaluated (%s)" % (play.get_name(), str(pattern), exc))
                continue
            if hostname in matched:
                plays.append(play)

    all_group = inventory.groups.get("all")
    host_groups = sort_groups([g for g in host.get_groups() if g.name != "all"])
    layers = []

    def add(level, entity, variables, files=(), **extra):
        described = describe(helper, variables, files)
        if described:
            layer = {"level": level, "entity": entity, "variables": described}
            layer.update(extra)
            layers.append(layer)

    def role_files(role, subdir):
        folder = os.path.join(role._role_path, subdir)
        if not os.path.isdir(folder):
            return []
        return [to_text(f) for f in loader.find_vars_files(folder, "main")]

    for play in plays:
        for role in play.roles:
            if role.public:
                add("role defaults", role.get_name(), role.get_default_vars(),
                    role_files(role, "defaults"), play=play.get_name())

    # ansible reads group_vars/host_vars next to each inventory source and next
    # to the playbook. When those are one directory (a project with the
    # inventory beside the playbook) the same files are read twice; the
    # playbook's level wins, so that is the one listed. Without a playbook there
    # is no playbook level at all.
    pdirs = [playbook_dir] if playbooks else []
    seen = {os.path.realpath(d) for d in pdirs}
    idirs = []
    for d in inventory_dirs(inventory, loader):
        real = os.path.realpath(d)
        if real not in seen:
            seen.add(real)
            idirs.append(d)

    def plugin_layers(level, entities, dirs, stage="task"):
        for entity in entities:
            for path in dirs:
                add(level, entity.name, get_vars_from_path(loader, path, [entity], stage),
                    vars_files_of(path, entity))

    def vars_files_of(path, entity):
        subdir = "host_vars" if entity.base_type.name == "HOST" else "group_vars"
        folder = os.path.join(os.path.realpath(path), subdir)
        if not os.path.isdir(folder):
            return []
        return [to_text(f) for f in loader.find_vars_files(folder, entity.name)]

    def all_inventory():
        add("inventory file: group", "all", all_group.get_vars())

    def groups_inventory():
        for group in host_groups:
            add("inventory file: group", group.name, group.get_vars())

    def all_plugins_inventory():
        plugin_layers("inventory group_vars", [all_group], idirs)

    def all_plugins_play():
        plugin_layers("playbook group_vars", [all_group], pdirs)

    def groups_plugins_inventory():
        plugin_layers("inventory group_vars", host_groups, idirs)

    def groups_plugins_play():
        plugin_layers("playbook group_vars", host_groups, pdirs)

    table = {f.__name__: f for f in (all_inventory, groups_inventory, all_plugins_inventory,
                                     all_plugins_play, groups_plugins_inventory, groups_plugins_play)}
    for entry in C.VARIABLE_PRECEDENCE:
        if entry in table:
            table[entry]()
        else:
            notes.append("unknown variable precedence entry %r ignored" % entry)

    add("inventory file: host", host.name, host.get_vars())
    plugin_layers("inventory host_vars", [host], idirs)
    plugin_layers("playbook host_vars", [host], pdirs)

    for play in plays:
        add("play vars", play.get_name(), play.get_vars())
        for item in play.get_vars_files():
            notes.append("play %r: vars_files %r not read (play-level variables come later)" % (play.get_name(), item))
        for role in play.roles:
            if role.public:
                add("role vars", role.get_name(),
                    role.get_vars(include_params=False, only_exports=False),
                    role_files(role, "vars"), play=play.get_name())

    extra = variable_manager.extra_vars
    add("extra vars", "-e", extra)

    # ansible's own answer for the same context, to compare the order with
    winners = {}
    if len(plays) <= 1:
        merged = variable_manager.get_vars(play=plays[0] if plays else None, host=host,
                                           include_hostvars=False)
        winners = describe(helper, {k: merged[k] for k in merged if any(k in l["variables"] for l in layers)})
    json.dump({
        "host": host.name,
        "groups": [g.name for g in host_groups],
        "layers": layers,
        "winners": winners,
        "plays": [p.get_name() for p in plays],
        "notes": notes,
    }, sys.stdout)


try:
    main()
except SystemExit:
    raise
except Exception as exc:  # noqa: BLE001 - reported, then non-zero exit
    sys.stderr.write("%s\n" % exc)
    sys.exit(1)
