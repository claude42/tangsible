# Loads a playbook with ansible's own parser, without running anything, and
# prints its plays and tasks as JSON on stdout (design-docs/View.md). Run by
# tangsible as `python3 -c <this file> <playbook>`; errors go to stderr with a
# non-zero exit status. Uses ansible-core's internal playbook classes, which
# can change between releases: any failure is reported, never papered over.
import json
import sys

from ansible.parsing.dataloader import DataLoader
from ansible.playbook import Playbook
from ansible.playbook.block import Block
from ansible.playbook.task import Task
from ansible.plugins.loader import init_plugin_loader
from ansible.vars.manager import VariableManager


def flatten(items, out):
    for item in items:
        if isinstance(item, Block):
            for part in (item.block, item.rescue, item.always):
                flatten(part, out)
        elif isinstance(item, Task):
            if item.action == "meta":
                # implicit ones (flush_handlers, role dependencies) are
                # not listed by --list-tasks either
                continue
            path, _, line = (item.get_path() or "").rpartition(":")
            out.append({
                "name": item.get_name(),
                "action": item.action,
                "path": path,
                "line": int(line) if line.isdigit() else 0,
                "role": item._role.get_name() if item._role else "",
                "tags": [str(t) for t in (item.tags or [])],
                "when": [str(w) for w in (item.when or [])],
            })


def main():
    init_plugin_loader()
    loader = DataLoader()
    variables = VariableManager(loader=loader)
    playbook = Playbook.load(sys.argv[1], variable_manager=variables, loader=loader)
    plays = []
    for play in playbook.get_plays():
        tasks = []
        for block in play.compile():
            flatten([block], tasks)
        hosts = play.hosts
        if isinstance(hosts, (list, tuple)):
            hosts = ",".join(str(h) for h in hosts)
        plays.append({"name": play.get_name(), "hosts": str(hosts), "tasks": tasks})
    json.dump({"plays": plays}, sys.stdout)


try:
    main()
except Exception as exc:  # noqa: BLE001 - reported, then non-zero exit
    sys.stderr.write("%s\n" % exc)
    sys.exit(1)
