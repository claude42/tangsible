# Copyright 2026 Klaus Wissmann and The jsonl callback authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

from __future__ import annotations

DOCUMENTATION = r"""
name: tangsible_events
type: aggregate
short_description: Tangsible's playbook event stream as JSON Lines
description:
  - Emits one JSON document per playbook event, so a consumer can follow the
    run while it is still in progress. Nothing is accumulated in memory.
  - Runs alongside the user's own stdout callback instead of replacing it.
  - Events go to the file descriptor named by the C(TANGSIBLE_EVENT_FD)
    environment variable when it is set, otherwise to normal display output.
  - The document layout follows the C(ansible.posix.jsonl) stdout callback;
    the differences are listed in the notes.
options:
  json_indent:
    name: Indentation width
    description:
      - Number of spaces used to indent each JSON document.
      - When unset or not positive, every event is written on a single line.
    type: integer
    default: 0
    env:
      - name: ANSIBLE_JSON_INDENT
    ini:
      - key: json_indent
        section: defaults
  show_custom_stats:
    name: Show custom stats
    description:
      - Include statistics recorded with C(set_stats) in the final stats event.
    type: bool
    default: false
    env:
      - name: ANSIBLE_SHOW_CUSTOM_STATS
    ini:
      - key: show_custom_stats
        section: defaults
notes:
  - With strategies such as C(free), a task is announced once per host through
    C(v2_runner_on_start) rather than once through C(v2_playbook_on_task_start),
    so several events can carry the same task id. That event also names the
    host in a top-level C(host) field.
  - Under C(linear) and C(debug), C(v2_runner_on_start) is emitted too, once per
    host the task is dispatched to, as a bare document holding only C(task.id)
    and C(host).
  - Handler task-start events under C(linear) and C(debug) carry
    C(task.is_handler), true.
  - Failed results carry C(ignore_errors), a boolean.
  - A C(v2_playbook_on_notify) event is emitted the moment a task queues a
    handler, well before the handler runs.
"""

import datetime
import json
import os
from collections.abc import Mapping, Set

from ansible.plugins.callback import CallbackBase


def _now():
    return datetime.datetime.now(datetime.timezone.utc)


def _stamp(moment=None):
    moment = moment or _now()
    return moment.strftime("%Y-%m-%dT%H:%M:%S.%fZ")


def _fallback(value):
    """Convert values the json module cannot handle natively."""
    if isinstance(value, (datetime.datetime, datetime.date)):
        return value.isoformat()
    if isinstance(value, bytes):
        return value.decode("utf-8", "replace")
    if isinstance(value, Mapping):
        return dict(value)
    if isinstance(value, (Set, tuple)):
        return list(value)
    return str(value)


# Strategies that advance every host through a task together. Anything else
# (free, host_pinned, custom ones) is reported per host.
_LOCKSTEP_STRATEGIES = frozenset({"linear", "debug"})


class CallbackModule(CallbackBase):
    CALLBACK_VERSION = 2.0
    CALLBACK_TYPE = "aggregate"
    CALLBACK_NAME = "tangsible_events"
    # Loads whenever its directory is on the callback plugin path, which
    # tangsible arranges only for the ansible-playbook processes it starts -
    # no need to look up and extend the user's callbacks_enabled list.
    CALLBACK_NEEDS_ENABLED = False

    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        # Start times, so a result can report how long its task ran.
        self._task_started = {}
        self._host_started = {}
        self._lockstep = True
        self.set_options()
        self._sink = None
        fd = os.environ.get("TANGSIBLE_EVENT_FD")
        if fd:
            self._sink = os.fdopen(int(fd), "w")

    # -- helpers ---------------------------------------------------------

    def _emit(self, event, **payload):
        payload["_event"] = event
        payload["_timestamp"] = _stamp()
        indent = self.get_option("json_indent")
        document = json.dumps(
            payload,
            default=_fallback,
            sort_keys=True,
            separators=(",", ":"),
            indent=indent if indent and indent > 0 else None,
        )
        if self._sink is None:
            self._display.display(document)
            return
        self._sink.write(document + "\n")
        self._sink.flush()

    @staticmethod
    def _describe(entity, started, ended=None):
        duration = {"start": started}
        if ended is not None:
            duration["end"] = ended
        return {
            "duration": duration,
            "id": str(entity._uuid),
            "name": entity.get_name(),
            "path": entity.get_path(),
        }

    def _announce_task(self, event, task, host=None, **extra):
        started = _stamp()
        if host is None:
            self._task_started[task._uuid] = started
        else:
            self._host_started[(task._uuid, host.name)] = started
            extra["host"] = host.name
        task_doc = self._describe(task, started)
        task_doc.update(extra.pop("task_extra", {}))
        self._emit(event, hosts={}, task=task_doc, **extra)

    def _report(self, event, result, **flags):
        ended = _stamp()
        task = result._task
        host = result._host.name
        started = self._host_started.pop((task._uuid, host), None)
        if started is None:
            started = self._task_started.get(task._uuid, ended)
        # The raw result carries neither the producing action nor, always, the
        # outcome flag that matches the event; make both explicit.
        payload = dict(result._result, action=task.action, **flags)
        self._emit(
            event,
            hosts={host: payload},
            task=self._describe(task, started, ended),
        )

    # -- play and task lifecycle ----------------------------------------

    def v2_playbook_on_play_start(self, play):
        self._lockstep = play.strategy in _LOCKSTEP_STRATEGIES
        self._emit(
            "v2_playbook_on_play_start",
            play=self._describe(play, _stamp()),
            tasks=[],
        )

    def v2_playbook_on_task_start(self, task, is_conditional):
        if self._lockstep:
            self._announce_task("v2_playbook_on_task_start", task)

    def v2_playbook_on_handler_task_start(self, task):
        # Per-host strategies announce a handler through v2_runner_on_start.
        if self._lockstep:
            self._announce_task("v2_playbook_on_handler_task_start", task,
                                task_extra={"is_handler": True})

    def v2_runner_on_start(self, host, task):
        if self._lockstep:
            # The task itself was announced once already; this only says
            # that a host has been handed it.
            self._emit("v2_runner_on_start", task={"id": str(task._uuid)},
                       host=host.name)
        else:
            self._announce_task("v2_runner_on_start", task, host)

    def v2_playbook_on_notify(self, handler, host):
        self._emit(
            "v2_playbook_on_notify",
            handler={"id": str(handler._uuid), "name": handler.get_name()},
            host=host.name,
        )

    # -- results --------------------------------------------------------

    def v2_runner_on_ok(self, result, **kwargs):
        self._report("v2_runner_on_ok", result)

    def v2_runner_on_failed(self, result, ignore_errors=False, **kwargs):
        self._report("v2_runner_on_failed", result, failed=True,
                     ignore_errors=bool(ignore_errors))

    def v2_runner_on_skipped(self, result, **kwargs):
        self._report("v2_runner_on_skipped", result, skipped=True)

    def v2_runner_on_unreachable(self, result, **kwargs):
        self._report("v2_runner_on_unreachable", result, unreachable=True)

    # -- summary --------------------------------------------------------

    def v2_playbook_on_stats(self, stats):
        per_host = {
            host: dict(stats.summarize(host)) for host in sorted(stats.processed)
        }
        custom, global_custom = {}, {}
        if self.get_option("show_custom_stats"):
            custom = {h: v for h, v in stats.custom.items() if h != "_run"}
            global_custom = stats.custom.get("_run", {})
        self._emit(
            "v2_playbook_on_stats",
            stats=per_host,
            custom_stats=custom,
            global_custom_stats=global_custom,
        )
