# (c) 2016, Matt Martz <matt@sivel.net>
# (c) 2017 Ansible Project
# (c) 2026 Klaus Wissmann
# GNU General Public License v3.0-or-later (see LICENSE or https://www.gnu.org/licenses/gpl-3.0.txt)
#
# This file is a derivative of ansible.posix.jsonl (GPL-3.0-or-later, see
# https://github.com/ansible-collections/ansible.posix). Changes: runs as
# an 'aggregate' callback instead of 'stdout' so it can run alongside the
# user's own stdout callback; emits a per-host v2_runner_on_start event
# under lockstep strategies (linear/debug) too, not just under free/
# host_pinned; writes to a dedicated inherited fd (TANGSIBLE_EVENT_FD)
# instead of stdout, falling back to normal display when that env var is
# absent; stamps is_handler on handler task-start events; emits
# ignore_errors on failed results; adds a v2_playbook_on_notify override
# for "handler queued, not yet run" visibility. See
# design-docs/OwnCallbackPlugin.md for the full rationale.

# Make coding more python3-ish
from __future__ import (absolute_import, division, print_function)
__metaclass__ = type

DOCUMENTATION = '''
    name: tangsible_jsonl
    short_description: Tangsible's event stream as JSONL (lines in json format)
    description:
        - This callback converts playbook events into JSON output, one per line.
        - It is a fork of ansible.posix.jsonl, modified to run as an aggregate
          callback (alongside the user's own stdout callback, e.g. C(default))
          rather than replacing it, and to emit a per-host C(v2_runner_on_start)
          event even under lockstep strategies (C(linear)/C(debug)).
        - Output goes to the fd named by the C(TANGSIBLE_EVENT_FD) environment
          variable when set, otherwise to normal display output.
    type: aggregate
    requirements:
      - Enable via callbacks_enabled / ANSIBLE_CALLBACKS_ENABLED (this is an
        aggregate callback, not a stdout callback)
    options:
      show_custom_stats:
        name: Show custom stats
        description: 'This adds the custom stats set via the set_stats plugin to the play recap'
        default: False
        env:
          - name: ANSIBLE_SHOW_CUSTOM_STATS
        ini:
          - key: show_custom_stats
            section: defaults
        type: bool
      json_indent:
        name: Use indenting for the JSON output
        description: 'If specified, use this many spaces for indenting in the JSON output. If not specified or <= 0, write to a single line.'
        default: 0
        env:
          - name: ANSIBLE_JSON_INDENT
        ini:
          - key: json_indent
            section: defaults
        type: integer
    notes:
      - When using a strategy such as free, host_pinned, or a custom strategy, host results will
        be added to new task results in ``.plays[].tasks[]``. As such, there will exist duplicate
        task objects indicated by duplicate task IDs at ``.plays[].tasks[].task.id``, each with an
        individual host result for the task.
      - Handler task-start events (``v2_playbook_on_handler_task_start``) carry an additional
        ``task.is_handler`` boolean field, true, not present on regular task-start events.
      - A failed result's per-host output includes an ``ignore_errors`` boolean field.
      - A ``v2_playbook_on_notify`` event fires the moment a task's notify option queues a
        handler, well before that handler actually runs at flush time - jsonl has no equivalent.
'''

import datetime
import json
import copy
import os

from functools import partial

from ansible.inventory.host import Host
from ansible.module_utils._text import to_text
from ansible.parsing.ajson import AnsibleJSONEncoder
from ansible.plugins.callback import CallbackBase


LOCKSTEP_CALLBACKS = frozenset(('linear', 'debug'))


def current_time():
    # Stripping tzinfo before isoformat() keeps the wire format identical to
    # the old utcnow()-based one (no "+00:00" before the trailing "Z").
    return '%sZ' % datetime.datetime.now(datetime.timezone.utc).replace(tzinfo=None).isoformat()


class CallbackModule(CallbackBase):
    CALLBACK_VERSION = 2.0
    CALLBACK_TYPE = 'aggregate'
    CALLBACK_NAME = 'tangsible_jsonl'
    CALLBACK_NEEDS_ENABLED = True

    def __init__(self, display=None):
        super(CallbackModule, self).__init__(display)
        self.results = []
        self._task_map = {}
        self._is_lockstep = False

        self.set_options()

        self._json_indent = self.get_option('json_indent')
        if self._json_indent <= 0:
            self._json_indent = None

        self._event_fd = None
        fd_env = os.environ.get('TANGSIBLE_EVENT_FD')
        if fd_env:
            self._event_fd = os.fdopen(int(fd_env), 'w')

    def _new_play(self, play):
        self._is_lockstep = play.strategy in LOCKSTEP_CALLBACKS
        return {
            'play': {
                'name': play.get_name(),
                'id': to_text(play._uuid),
                'path': to_text(play.get_path()),
                'duration': {
                    'start': current_time()
                }
            },
            'tasks': []
        }

    def _new_task(self, task):
        return {
            'task': {
                'name': task.get_name(),
                'id': to_text(task._uuid),
                'path': to_text(task.get_path()),
                'duration': {
                    'start': current_time()
                }
            },
            'hosts': {}
        }

    def _find_result_task(self, host, task):
        key = (host.get_name(), task._uuid)
        return self._task_map.get(
            key,
            self.results[-1]['tasks'][-1]
        )

    def v2_playbook_on_play_start(self, play):
        play_result = self._new_play(play)
        self.results.append(play_result)
        self._write_event('v2_playbook_on_play_start', play_result)

    def v2_runner_on_start(self, host, task):
        if self._is_lockstep:
            # v2_playbook_on_task_start already recorded the task itself
            # (results / _task_map) under lockstep - this is purely an
            # additional per-host "dispatched" signal, so it must not
            # touch either.
            self._write_event('v2_runner_on_start', {
                'task': {'id': to_text(task._uuid)},
                'host': host.get_name(),
            })
            return
        key = (host.get_name(), task._uuid)
        task_result = self._new_task(task)
        task_result['host'] = host.get_name()
        self._task_map[key] = task_result
        self.results[-1]['tasks'].append(task_result)
        self._write_event('v2_runner_on_start', task_result)

    def v2_playbook_on_task_start(self, task, is_conditional):
        if not self._is_lockstep:
            return
        task_result = self._new_task(task)
        self.results[-1]['tasks'].append(task_result)
        self._write_event('v2_playbook_on_task_start', task_result)

    def v2_playbook_on_handler_task_start(self, task):
        if not self._is_lockstep:
            return
        task_result = self._new_task(task)
        task_result['task']['is_handler'] = True
        self.results[-1]['tasks'].append(task_result)
        self._write_event('v2_playbook_on_handler_task_start', task_result)

    def v2_playbook_on_notify(self, handler, host):
        self._write_event('v2_playbook_on_notify', {
            'handler': {
                'name': handler.get_name(),
                'id': to_text(handler._uuid),
            },
            'host': host.get_name(),
        })

    def _convert_host_to_name(self, key):
        if isinstance(key, (Host,)):
            return key.get_name()
        return key

    def v2_playbook_on_stats(self, stats):
        """Display info about playbook statistics"""

        hosts = sorted(stats.processed.keys())

        summary = {}
        for h in hosts:
            s = stats.summarize(h)
            summary[h] = s

        custom_stats = {}
        global_custom_stats = {}

        if self.get_option('show_custom_stats') and stats.custom:
            custom_stats.update(dict((self._convert_host_to_name(k), v) for k, v in stats.custom.items()))
            global_custom_stats.update(custom_stats.pop('_run', {}))

        output = {
            'stats': summary,
            'custom_stats': custom_stats,
            'global_custom_stats': global_custom_stats,
        }

        self._write_event('v2_playbook_on_stats', output)

    def _write_event(self, event_name, output):
        output['_event'] = event_name
        output['_timestamp'] = current_time()
        line = json.dumps(output, cls=AnsibleJSONEncoder, indent=self._json_indent, separators=',:', sort_keys=True)
        if self._event_fd is not None:
            self._event_fd.write(line + '\n')
            self._event_fd.flush()
        else:
            self._display.display(line)

    def _record_task_result(self, event_name, on_info, result, **kwargs):
        """This function is used as a partial to add failed/skipped info in a single method"""
        host = result._host
        task = result._task

        result_copy = result._result.copy()
        result_copy.update(on_info)
        result_copy['action'] = task.action
        if 'ignore_errors' in kwargs:
            result_copy['ignore_errors'] = kwargs['ignore_errors']

        task_result = self._find_result_task(host, task)

        end_time = current_time()
        task_result['task']['duration']['end'] = end_time
        self.results[-1]['play']['duration']['end'] = end_time

        task_result_copy = copy.deepcopy(task_result)
        task_result_copy['hosts'][host.name] = result_copy

        if not self._is_lockstep:
            key = (host.get_name(), task._uuid)
            del self._task_map[key]

        self._write_event(event_name, task_result_copy)

    def __getattribute__(self, name):
        """Return ``_record_task_result`` partial with a dict containing skipped/failed if necessary"""
        if name not in ('v2_runner_on_ok', 'v2_runner_on_failed', 'v2_runner_on_unreachable', 'v2_runner_on_skipped'):
            return object.__getattribute__(self, name)

        on = name.rsplit('_', 1)[1]

        on_info = {}
        if on in ('failed', 'skipped'):
            on_info[on] = True

        return partial(self._record_task_result, name, on_info)
