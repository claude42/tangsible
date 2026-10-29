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

"""Behavioural tests for the tangsible_events callback.

They run real ``ansible-playbook`` processes and check the event stream the
plugin writes to its event fd. ``ParityTests`` additionally compares against
``ansible.posix.jsonl`` used purely as a black box (it is executed, never
read); the plugin's documented additions are stripped before comparing. They
are skipped when that collection is unavailable.

Run with:  python3 -m unittest discover -s callback-tests -v
"""

import datetime
import json
import os
import re
import shutil
import subprocess
import tempfile
import textwrap
import unittest

def is_addition(event):
    """Events the reference jsonl has no counterpart for."""
    return event["_event"] == "v2_playbook_on_notify" or (
        event["_event"] == "v2_runner_on_start" and "name" not in event["task"])


def strip_additions(event):
    """Drop the extra fields this plugin adds to events jsonl also emits."""
    event = json.loads(json.dumps(event))
    event.pop("host", None)
    event.get("task", {}).pop("is_handler", None)
    for result in event.get("hosts", {}).values():
        result.pop("ignore_errors", None)
    return event


HERE = os.path.dirname(os.path.abspath(__file__))
PLUGIN_DIR = os.path.join(os.path.dirname(HERE), "callback")
REFERENCE = "ansible.posix.jsonl"
TS_RE = re.compile(r"^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d\.\d{6}Z$")


@unittest.skipUnless(shutil.which("ansible-playbook"), "ansible not installed")
class Base(unittest.TestCase):
    # None: the plugin under test, writing to its event fd. Any other value
    # is run as the stdout callback and its stdout is the event stream.
    callback = None

    def setUp(self):
        self.tmp = tempfile.mkdtemp(prefix="events-test-")
        self.addCleanup(shutil.rmtree, self.tmp, ignore_errors=True)
        self.write("inventory", "localhost ansible_connection=local\n")

    def write(self, name, text):
        path = os.path.join(self.tmp, name)
        with open(path, "w") as fh:
            fh.write(textwrap.dedent(text))
        return path

    def run_play(self, playbook, env=None, inventory="inventory", args=(),
                 callback=None):
        path = self.write("play.yml", playbook)
        full_env = dict(os.environ)
        for key in list(full_env):
            if key.startswith("ANSIBLE_"):
                del full_env[key]
        callback = callback or self.callback
        full_env.update(
            ANSIBLE_LOCALHOST_WARNING="false",
            ANSIBLE_INVENTORY_UNPARSED_WARNING="false",
            ANSIBLE_HOME=os.path.join(self.tmp, "home"),
            ANSIBLE_NOCOLOR="1",
        )
        sink = None
        if callback:
            # Another plugin as the stream source: ours must not also load
            # (it needs no enabling) and write to the same stdout.
            full_env["ANSIBLE_STDOUT_CALLBACK"] = callback
        else:
            full_env["ANSIBLE_CALLBACK_PLUGINS"] = PLUGIN_DIR
            sink = open(os.path.join(self.tmp, "events.out"), "w+")
            full_env["TANGSIBLE_EVENT_FD"] = str(sink.fileno())
        full_env.update(env or {})
        proc = subprocess.run(
            ["ansible-playbook", "-i", os.path.join(self.tmp, inventory),
             *args, path],
            capture_output=True, text=True, env=full_env, cwd=self.tmp,
            pass_fds=(sink.fileno(),) if sink else (),
        )
        self.proc = proc
        # The event stream: the event fd's contents, or stdout for a plain
        # stdout callback.
        if sink:
            sink.seek(0)
            self.raw = sink.read()
            sink.close()
        else:
            self.raw = proc.stdout
        return proc

    def events(self, playbook, everything=False, **kw):
        """The parsed event stream. The plugin's own additions - the per-host
        start under lockstep strategies and the notify event - are left out
        unless ``everything``, so tests of the shared format see the same
        stream a plain jsonl callback would produce."""
        proc = self.run_play(playbook, **kw)
        events = []
        for doc in filter(None, re.split(r"(?m)^(?=\{)", self.raw)):
            try:
                events.append(json.loads(doc))
            except ValueError:
                self.fail("non-JSON in event stream: %r\nstderr: %s"
                          % (doc, proc.stderr))
        if everything:
            return events
        return [e for e in events if not is_addition(e)]

    @staticmethod
    def of(events, kind):
        return [e for e in events if e["_event"] == "v2_" + kind]


class OutputFormatTests(Base):
    PLAY = """
        - hosts: all
          gather_facts: false
          tasks:
            - name: hello
              debug: msg="héllo ✓"
    """

    def test_every_line_is_a_json_object_with_event_and_timestamp(self):
        events = self.events(self.PLAY)
        self.assertTrue(events)
        for event in events:
            self.assertIsInstance(event, dict)
            self.assertTrue(event["_event"].startswith("v2_"))
            self.assertRegex(event["_timestamp"], TS_RE)

    def test_timestamps_are_utc_and_recent(self):
        before = datetime.datetime.now(datetime.timezone.utc)
        events = self.events(self.PLAY)
        for event in events:
            moment = datetime.datetime.strptime(
                event["_timestamp"], "%Y-%m-%dT%H:%M:%S.%fZ"
            ).replace(tzinfo=datetime.timezone.utc)
            self.assertGreaterEqual(moment, before - datetime.timedelta(seconds=1))

    def test_compact_single_line_sorted_keys_by_default(self):
        self.run_play(self.PLAY)
        for line in self.raw.splitlines():
            self.assertNotRegex(line, r'[,:] ')
            top = json.loads(line, object_pairs_hook=lambda pairs: [k for k, _ in pairs])
            self.assertEqual(top, sorted(top))

    def test_non_ascii_is_escaped(self):
        self.run_play(self.PLAY)
        self.assertIn("h\\u00e9llo \\u2713", self.raw)
        self.assertTrue(self.raw.isascii())

    def test_indent_env_spreads_output_over_lines(self):
        self.run_play(self.PLAY, env={"ANSIBLE_JSON_INDENT": "3"})
        lines = self.raw.splitlines()
        self.assertGreater(len(lines), 10)
        self.assertIn('   "_event":"v2_playbook_on_play_start",', lines)
        docs = re.split(r"(?m)^(?=\{)", self.raw)
        for doc in filter(None, docs):
            json.loads(doc)

    def test_indent_zero_and_negative_stay_on_one_line(self):
        for value in ("0", "-2"):
            self.run_play(self.PLAY, env={"ANSIBLE_JSON_INDENT": value})
            for line in self.raw.splitlines():
                json.loads(line)

    def test_indent_from_ini_file(self):
        cfg = self.write("ansible.cfg", "[defaults]\njson_indent = 2\n")
        self.run_play(self.PLAY, env={"ANSIBLE_CONFIG": cfg})
        self.assertIn('  "_event":"v2_playbook_on_play_start",', self.raw)


class EventStreamTests(Base):
    def test_basic_sequence_and_shapes(self):
        events = self.events("""
            - name: Play one
              hosts: all
              gather_facts: false
              tasks:
                - name: say it
                  debug: msg=hi
        """)
        self.assertEqual(
            [e["_event"] for e in events],
            ["v2_playbook_on_play_start", "v2_playbook_on_task_start",
             "v2_runner_on_ok", "v2_playbook_on_stats"],
        )
        play, start, ok, stats = events

        self.assertEqual(set(play), {"_event", "_timestamp", "play", "tasks"})
        self.assertEqual(play["tasks"], [])
        self.assertEqual(play["play"]["name"], "Play one")
        self.assertEqual(set(play["play"]), {"duration", "id", "name", "path"})
        self.assertEqual(set(play["play"]["duration"]), {"start"})
        self.assertRegex(play["play"]["path"], r"play\.yml:2$")
        self.assertRegex(play["play"]["duration"]["start"], TS_RE)

        self.assertEqual(set(start), {"_event", "_timestamp", "hosts", "task"})
        self.assertEqual(start["hosts"], {})
        self.assertEqual(start["task"]["name"], "say it")
        self.assertEqual(set(start["task"]), {"duration", "id", "name", "path"})
        self.assertEqual(set(start["task"]["duration"]), {"start"})
        self.assertRegex(start["task"]["path"], r"play\.yml:6$")

        self.assertEqual(set(ok), {"_event", "_timestamp", "hosts", "task"})
        self.assertEqual(list(ok["hosts"]), ["localhost"])
        self.assertEqual(ok["hosts"]["localhost"]["msg"], "hi")
        self.assertEqual(ok["task"]["id"], start["task"]["id"])
        self.assertEqual(ok["task"]["duration"]["start"],
                         start["task"]["duration"]["start"])
        self.assertGreaterEqual(ok["task"]["duration"]["end"],
                                ok["task"]["duration"]["start"])
        self.assertNotEqual(play["play"]["id"], start["task"]["id"])

    def test_no_playbook_start_or_item_events(self):
        events = self.events("""
            - hosts: all
              gather_facts: false
              tasks:
                - debug: msg={{ item }}
                  loop: [1, 2, 3]
        """)
        kinds = {e["_event"] for e in events}
        self.assertEqual(kinds, {"v2_playbook_on_play_start",
                                 "v2_playbook_on_task_start",
                                 "v2_runner_on_ok", "v2_playbook_on_stats"})
        ok = self.of(events, "runner_on_ok")
        self.assertEqual(len(ok), 1)
        self.assertEqual(len(ok[0]["hosts"]["localhost"]["results"]), 3)

    def test_unnamed_task_is_named_after_its_action(self):
        events = self.events("""
            - hosts: all
              gather_facts: false
              tasks:
                - command: /bin/true
        """)
        self.assertEqual(self.of(events, "playbook_on_task_start")[0]["task"]["name"],
                         "command")

    def test_one_play_start_per_play_even_without_hosts(self):
        events = self.events("""
            - name: A
              hosts: all
              gather_facts: false
              tasks: []
            - name: B
              hosts: nomatch
              tasks: []
        """)
        plays = self.of(events, "playbook_on_play_start")
        self.assertEqual([p["play"]["name"] for p in plays], ["A", "B"])
        self.assertEqual(len(self.of(events, "playbook_on_stats")), 1)

    def test_changed_failed_skipped_and_ignored(self):
        events = self.events("""
            - hosts: all
              gather_facts: false
              tasks:
                - name: changed
                  command: /bin/true
                - name: skipped
                  debug: msg=x
                  when: false
                - name: ignored
                  command: /bin/false
                  ignore_errors: true
                - name: fatal
                  fail: msg=boom
        """)
        by_name = {e["task"]["name"]: e for e in events
                   if e["_event"].startswith("v2_runner_on_")}
        self.assertEqual(by_name["changed"]["_event"], "v2_runner_on_ok")
        self.assertTrue(by_name["changed"]["hosts"]["localhost"]["changed"])
        self.assertEqual(by_name["skipped"]["_event"], "v2_runner_on_skipped")
        self.assertTrue(by_name["skipped"]["hosts"]["localhost"]["skipped"])
        self.assertEqual(by_name["ignored"]["_event"], "v2_runner_on_failed")
        self.assertEqual(by_name["ignored"]["hosts"]["localhost"]["rc"], 1)
        self.assertEqual(by_name["fatal"]["_event"], "v2_runner_on_failed")
        self.assertEqual(by_name["fatal"]["hosts"]["localhost"]["msg"], "boom")
        self.assertEqual(self.proc.returncode, 2)

    def test_unreachable(self):
        self.write("inv2", "ghost ansible_host=127.0.0.1 ansible_port=1 "
                           "ansible_connection=ssh\n")
        events = self.events("""
            - hosts: all
              gather_facts: false
              tasks:
                - ping:
        """, inventory="inv2", env={"ANSIBLE_SSH_RETRIES": "0"})
        bad = self.of(events, "runner_on_unreachable")
        self.assertEqual(len(bad), 1)
        self.assertTrue(bad[0]["hosts"]["ghost"]["unreachable"])
        stats = self.of(events, "playbook_on_stats")[0]["stats"]["ghost"]
        self.assertEqual(stats["unreachable"], 1)
        self.assertEqual(self.proc.returncode, 4)

    def test_handlers_are_announced_with_their_own_event(self):
        events = self.events("""
            - hosts: all
              gather_facts: false
              tasks:
                - command: /bin/true
                  notify: h1
              handlers:
                - name: h1
                  debug: msg=handled
        """)
        handler = self.of(events, "playbook_on_handler_task_start")
        self.assertEqual(len(handler), 1)
        self.assertEqual(handler[0]["task"]["name"], "h1")
        self.assertEqual(handler[0]["hosts"], {})
        idx = events.index(handler[0])
        self.assertEqual(events[idx + 1]["_event"], "v2_runner_on_ok")
        self.assertEqual(events[idx + 1]["task"]["id"], handler[0]["task"]["id"])

    def test_rescue_block_events(self):
        events = self.events("""
            - hosts: all
              gather_facts: false
              tasks:
                - block:
                    - name: bad
                      fail: msg=x
                  rescue:
                    - name: recover
                      debug: msg=ok
        """)
        names = [(e["_event"][3:], e["task"]["name"]) for e in events if "task" in e]
        self.assertIn(("runner_on_failed", "bad"), names)
        self.assertIn(("runner_on_ok", "recover"), names)
        self.assertEqual(self.proc.returncode, 0)

    def test_no_log_result_is_censored(self):
        events = self.events("""
            - hosts: all
              gather_facts: false
              tasks:
                - debug: msg=topsecret
                  no_log: true
        """)
        self.assertNotIn("topsecret", self.raw)
        result = self.of(events, "runner_on_ok")[0]["hosts"]["localhost"]
        self.assertIn("censored", result)

    def test_diff_is_embedded_in_the_result(self):
        target = os.path.join(self.tmp, "target.txt")
        events = self.events("""
            - hosts: all
              gather_facts: false
              tasks:
                - copy: {content: "new\\n", dest: %s}
                  diff: true
        """ % target)
        result = self.of(events, "runner_on_ok")[0]["hosts"]["localhost"]
        self.assertEqual(result["diff"][0]["after"], "new\n")

    def test_include_tasks_events(self):
        self.write("inc.yml", "- name: inner\n  debug: msg=in\n")
        events = self.events("""
            - hosts: all
              gather_facts: false
              tasks:
                - include_tasks: inc.yml
        """)
        names = [e["task"]["name"] for e in self.of(events, "runner_on_ok")]
        self.assertEqual(names, ["include_tasks", "inner"])

    def test_event_stream_is_only_json(self):
        events = self.events("""
            - hosts: all
              tasks:
                - debug: msg=x
        """)
        self.assertTrue(events)
        self.assertEqual(self.of(events, "playbook_on_task_start")[0]["task"]["name"],
                         "Gathering Facts")


class StrategyTests(Base):
    def setUp(self):
        super().setUp()
        self.write("inv2", "a ansible_connection=local\nb ansible_connection=local\n")

    def test_free_strategy_uses_per_host_start_events(self):
        events = self.events("""
            - hosts: all
              gather_facts: false
              strategy: free
              tasks:
                - command: sleep {{ 2 if inventory_hostname == 'a' else 0 }}
                - debug: msg=second
        """, inventory="inv2")
        self.assertEqual(self.of(events, "playbook_on_task_start"), [])
        starts = self.of(events, "runner_on_start")
        self.assertEqual(len(starts), 4)
        for start in starts:
            self.assertEqual(start["hosts"], {})
            self.assertEqual(set(start["task"]["duration"]), {"start"})
        first = [s for s in starts if s["task"]["name"] == "command"]
        self.assertEqual(len({s["task"]["id"] for s in first}), 1)
        results = [e for e in self.of(events, "runner_on_ok")
                   if e["task"]["name"] == "command"]
        self.assertEqual(sorted(next(iter(e["hosts"])) for e in results), ["a", "b"])
        # Each host's result reports the start time of *its own* start event;
        # the hosts started at different instants so those must be distinct.
        starts_seen = {e["task"]["duration"]["start"] for e in results}
        self.assertEqual(len(starts_seen), 2)
        self.assertEqual(starts_seen, {s["task"]["duration"]["start"] for s in first})

    def test_linear_strategy_shares_one_task_start(self):
        events = self.events("""
            - hosts: all
              gather_facts: false
              tasks:
                - debug: msg=x
        """, inventory="inv2")
        self.assertEqual(len(self.of(events, "playbook_on_task_start")), 1)
        results = self.of(events, "runner_on_ok")
        self.assertEqual(len(results), 2)
        self.assertEqual({e["task"]["duration"]["start"] for e in results},
                         {self.of(events, "playbook_on_task_start")[0]
                          ["task"]["duration"]["start"]})


class StatsTests(Base):
    PLAY = """
        - hosts: all
          gather_facts: false
          tasks:
            - command: /bin/true
            - debug: msg=x
            - debug: msg=y
              when: false
            - command: /bin/false
              ignore_errors: true
            - set_stats: {data: {global_key: 1}}
            - set_stats: {data: {host_key: 2}, per_host: true}
    """

    def test_summary_counts(self):
        stats = self.of(self.events(self.PLAY), "playbook_on_stats")[0]
        self.assertEqual(stats["stats"]["localhost"], {
            "ok": 5, "changed": 2, "failures": 0, "skipped": 1,
            "ignored": 1, "rescued": 0, "unreachable": 0,
        })

    def test_failure_counts(self):
        events = self.events("""
            - hosts: all
              gather_facts: false
              tasks:
                - fail:
        """)
        stats = self.of(events, "playbook_on_stats")[0]["stats"]["localhost"]
        self.assertEqual(stats["failures"], 1)

    def test_custom_stats_hidden_by_default(self):
        stats = self.of(self.events(self.PLAY), "playbook_on_stats")[0]
        self.assertEqual(stats["custom_stats"], {})
        self.assertEqual(stats["global_custom_stats"], {})

    def test_custom_stats_shown_when_enabled(self):
        stats = self.of(self.events(self.PLAY, env={"ANSIBLE_SHOW_CUSTOM_STATS": "1"}),
                        "playbook_on_stats")[0]
        self.assertEqual(stats["custom_stats"], {"localhost": {"host_key": 2}})
        self.assertEqual(stats["global_custom_stats"], {"global_key": 1})

    def test_custom_stats_enabled_from_ini(self):
        cfg = self.write("ansible.cfg", "[defaults]\nshow_custom_stats = true\n")
        stats = self.of(self.events(self.PLAY, env={"ANSIBLE_CONFIG": cfg}),
                        "playbook_on_stats")[0]
        self.assertEqual(stats["global_custom_stats"], {"global_key": 1})

    def test_stats_is_last_event(self):
        events = self.events(self.PLAY)
        self.assertEqual(events[-1]["_event"], "v2_playbook_on_stats")


class AdditionTests(Base):
    """What this plugin does beyond the jsonl format (see its notes)."""

    def setUp(self):
        super().setUp()
        self.write("inv2", "a ansible_connection=local\nb ansible_connection=local\n")

    def test_runs_beside_the_users_stdout_callback(self):
        self.run_play("""
            - hosts: all
              gather_facts: false
              tasks:
                - debug: msg=visible
        """)
        self.assertIn("PLAY RECAP", self.proc.stdout)
        self.assertIn("visible", self.proc.stdout)
        self.assertNotIn("v2_playbook_on", self.proc.stdout)
        self.assertIn("v2_playbook_on_stats", self.raw)

    def test_loads_without_being_enabled(self):
        self.run_play("""
            - hosts: all
              gather_facts: false
              tasks: [{debug: msg=x}]
        """, env={"ANSIBLE_CALLBACKS_ENABLED": "timer"})
        self.assertIn("v2_playbook_on_stats", self.raw)

    def test_without_an_event_fd_events_go_to_normal_output(self):
        play = self.write("play.yml", """
            - hosts: all
              gather_facts: false
              tasks: [{debug: msg=x}]
        """)
        env = {k: v for k, v in os.environ.items() if not k.startswith("ANSIBLE_")}
        env.update(ANSIBLE_CALLBACK_PLUGINS=PLUGIN_DIR,
                   ANSIBLE_STDOUT_CALLBACK="minimal",
                   ANSIBLE_HOME=os.path.join(self.tmp, "home"))
        out = subprocess.run(
            ["ansible-playbook", "-i", os.path.join(self.tmp, "inventory"), play],
            capture_output=True, text=True, env=env, cwd=self.tmp).stdout
        kinds = [json.loads(l)["_event"] for l in out.splitlines() if l.startswith("{")]
        self.assertEqual(kinds[0], "v2_playbook_on_play_start")
        self.assertEqual(kinds[-1], "v2_playbook_on_stats")

    def test_lockstep_strategy_announces_each_host_start(self):
        events = self.events("""
            - hosts: all
              gather_facts: false
              tasks:
                - debug: msg=x
        """, inventory="inv2", everything=True)
        task_start = self.of(events, "playbook_on_task_start")[0]
        starts = self.of(events, "runner_on_start")
        self.assertEqual(sorted(s["host"] for s in starts), ["a", "b"])
        for start in starts:
            self.assertEqual(set(start), {"_event", "_timestamp", "task", "host"})
            self.assertEqual(start["task"], {"id": task_start["task"]["id"]})
            self.assertRegex(start["_timestamp"], TS_RE)
        order = [e["_event"] for e in events]
        self.assertLess(order.index("v2_playbook_on_task_start"),
                        order.index("v2_runner_on_start"))
        self.assertLess(order.index("v2_runner_on_start"),
                        order.index("v2_runner_on_ok"))

    def test_free_strategy_start_names_its_host(self):
        events = self.events("""
            - hosts: all
              gather_facts: false
              strategy: free
              tasks:
                - debug: msg=x
        """, inventory="inv2", everything=True)
        starts = self.of(events, "runner_on_start")
        self.assertEqual(sorted(s["host"] for s in starts), ["a", "b"])
        self.assertTrue(all(s["task"]["name"] == "debug" for s in starts))

    def test_free_strategy_announces_handlers_per_host_only(self):
        events = self.events("""
            - hosts: all
              gather_facts: false
              strategy: free
              tasks:
                - command: /bin/true
                  changed_when: true
                  notify: h
              handlers:
                - name: h
                  debug: msg=handled
        """, inventory="inv2", everything=True)
        self.assertEqual(self.of(events, "playbook_on_handler_task_start"), [])
        self.assertEqual(sorted(s["host"] for s in self.of(events, "runner_on_start")
                                if s["task"]["name"] == "h"), ["a", "b"])

    def test_handler_start_is_flagged_only_under_lockstep(self):
        events = self.events("""
            - hosts: all
              gather_facts: false
              tasks:
                - command: /bin/true
                  changed_when: true
                  notify: h
              handlers:
                - name: h
                  debug: msg=handled
        """)
        handler = self.of(events, "playbook_on_handler_task_start")[0]
        self.assertIs(handler["task"]["is_handler"], True)
        self.assertNotIn("is_handler", self.of(events, "playbook_on_task_start")[0]["task"])

    def test_failed_results_say_whether_errors_are_ignored(self):
        events = self.events("""
            - hosts: all
              gather_facts: false
              tasks:
                - name: ignored
                  command: /bin/false
                  ignore_errors: true
                - name: fatal
                  fail: msg=boom
        """)
        failed = {e["task"]["name"]: e["hosts"]["localhost"]
                  for e in self.of(events, "runner_on_failed")}
        self.assertIs(failed["ignored"]["ignore_errors"], True)
        self.assertIs(failed["fatal"]["ignore_errors"], False)
        for event in self.of(events, "runner_on_ok"):
            self.assertNotIn("ignore_errors", event["hosts"]["localhost"])

    def test_notify_event_precedes_the_handler(self):
        events = self.events("""
            - hosts: all
              gather_facts: false
              tasks:
                - command: /bin/true
                  changed_when: true
                  notify: h
                - debug: msg=later
              handlers:
                - name: h
                  debug: msg=handled
        """, everything=True)
        notify = self.of(events, "playbook_on_notify")
        self.assertEqual(len(notify), 1)
        self.assertEqual(set(notify[0]), {"_event", "_timestamp", "handler", "host"})
        self.assertEqual(notify[0]["host"], "localhost")
        self.assertEqual(notify[0]["handler"]["name"], "h")
        handler = self.of(events, "playbook_on_handler_task_start")[0]
        self.assertEqual(notify[0]["handler"]["id"], handler["task"]["id"])
        self.assertLess(events.index(notify[0]), events.index(handler))


class DocumentationTests(Base):
    def test_ansible_doc_lists_options(self):
        env = dict(os.environ, ANSIBLE_CALLBACK_PLUGINS=PLUGIN_DIR)
        out = subprocess.run(
            ["ansible-doc", "-t", "callback", "tangsible_events", "--json"],
            capture_output=True, text=True, env=env)
        self.assertEqual(out.returncode, 0, out.stderr)
        doc = json.loads(out.stdout)["tangsible_events"]["doc"]
        self.assertEqual(doc["type"], "aggregate")
        self.assertEqual(set(doc["options"]), {"json_indent", "show_custom_stats"})
        self.assertEqual(doc["options"]["json_indent"]["default"], 0)
        self.assertFalse(doc["options"]["show_custom_stats"]["default"])


def _normalise(value, key=None):
    """Blank out values that legitimately differ between two runs."""
    volatile = {"_timestamp", "start", "end", "delta", "id", "path", "attempts",
                "ansible_job_id", "results_file", "job_id"}
    if isinstance(value, dict):
        return {k: ("<v>" if k in volatile else _normalise(v, k))
                for k, v in value.items()}
    if isinstance(value, list):
        return [_normalise(v, key) for v in value]
    return value


@unittest.skipUnless(
    shutil.which("ansible-doc")
    and subprocess.run(["ansible-doc", "-t", "callback", REFERENCE],
                       capture_output=True).returncode == 0,
    "%s not available" % REFERENCE)
class ParityTests(Base):
    """Compare event streams with the reference plugin run as a black box."""

    PLAYBOOKS = {
        "mixed": """
            - name: Play one
              hosts: all
              gather_facts: false
              tasks:
                - name: ok
                  debug: msg=hi
                - name: changed
                  command: /bin/true
                - name: skipped
                  debug: msg=x
                  when: false
                - name: ignored
                  command: /bin/false
                  ignore_errors: true
                - name: loop
                  debug: msg={{ item }}
                  loop: [1, 2]
                - name: loop skipped
                  debug: msg={{ item }}
                  loop: [1]
                  when: false
                - set_stats: {data: {foo: 1}}
                - set_stats: {data: {bar: 2}, per_host: true}
                - name: unicode
                  debug: msg="héllo ✓"
                - name: nolog
                  debug: msg=secret
                  no_log: true
                - name: fatal
                  fail: msg=boom
            - name: Play two
              hosts: nomatch
              tasks: []
        """,
        "handlers_and_rescue": """
            - hosts: all
              gather_facts: false
              tasks:
                - command: /bin/true
                  notify: h1
                - block:
                    - fail: msg=x
                  rescue:
                    - debug: msg=rescued
                  always:
                    - debug: msg=always
              handlers:
                - name: h1
                  debug: msg=handler
        """,
        "retries": """
            - hosts: all
              gather_facts: false
              tasks:
                - command: /bin/false
                  register: r
                  retries: 2
                  delay: 0
                  until: r is succeeded
                  ignore_errors: true
        """,
        "include_and_meta": """
            - hosts: all
              gather_facts: false
              tasks:
                - include_tasks: inc.yml
                - meta: flush_handlers
                - assert: {that: true}
        """,
        "check_mode": """
            - hosts: all
              gather_facts: false
              tasks:
                - copy: {content: hi, dest: "{tmp}/x"}
                  diff: true
        """,
    }

    def capture(self, name, callback, env=None, inventory="inventory", args=()):
        self.write("inc.yml", "- debug: msg=included\n")
        play = self.PLAYBOOKS[name].replace("{tmp}", self.tmp)
        events = self.events(play, callback=callback, env=env,
                             inventory=inventory, args=args)
        return events, self.proc.returncode

    def assert_same(self, name, env=None, args=()):
        ours, rc_ours = self.capture(name, None, env, args=args)
        theirs, rc_theirs = self.capture(name, REFERENCE, env, args=args)
        self.assertEqual(rc_ours, rc_theirs)
        self.assertEqual([e["_event"] for e in ours], [e["_event"] for e in theirs])
        for mine, ref in zip(ours, theirs):
            mine = strip_additions(mine)
            self.assertEqual(_normalise(mine), _normalise(ref), mine["_event"])
            self.assertEqual(set(mine), set(ref))

    def test_parity_per_playbook(self):
        for name in self.PLAYBOOKS:
            with self.subTest(playbook=name):
                self.assert_same(name, args=("-C",) if name == "check_mode" else ())

    def test_parity_with_custom_stats(self):
        self.assert_same("mixed", env={"ANSIBLE_SHOW_CUSTOM_STATS": "1"})

    def test_parity_byte_for_byte_formatting(self):
        """Same separators, key order and escaping once values are masked."""
        def shape(stream):
            # Drop what the reference has no counterpart for first: whole
            # documents (extra events) and the ignore_errors/host/is_handler
            # fields, which are written in the same compact or indented style.
            docs = [d for d in re.split(r"(?m)^(?=\{)", stream)
                    if d and not is_addition(json.loads(d))]
            out = "".join(docs)
            out = re.sub(r'"(?:ignore_errors|is_handler)":\s*(?:true|false|null),\s*', "", out)
            out = re.sub(r'"host":\s*"[^"]*",\s*', "", out)
            out = re.sub(r'"(?:_timestamp|start|end)":"[^"]*"', '"T":"x"', out)
            out = re.sub(r'"id":"[^"]*"', '"id":"x"', out)
            out = re.sub(r'"path":"[^"]*"', '"path":"x"', out)
            out = re.sub(r'"delta":"[^"]*"', '"delta":"x"', out)
            return out
        for indent in ("0", "4"):
            env = {"ANSIBLE_JSON_INDENT": indent}
            self.write("inc.yml", "- debug: msg=included\n")
            play = self.PLAYBOOKS["mixed"]
            self.run_play(play, env=env)
            mine = shape(self.raw)
            self.run_play(play, env=env, callback=REFERENCE)
            theirs = shape(self.raw)
            self.assertEqual(mine, theirs, "indent=%s" % indent)

    def test_parity_event_kinds_for_per_host_strategies(self):
        self.write("inv2", "a ansible_connection=local\nb ansible_connection=local\n")
        for strategy in ("free", "host_pinned", "linear"):
            play = """
                - hosts: all
                  gather_facts: false
                  strategy: %s
                  tasks:
                    - debug: msg=1
                      notify: h
                    - debug: msg=2
                  handlers:
                    - name: h
                      debug: msg=handled
            """ % strategy
            kinds = []
            for cb in (None, REFERENCE):
                events = self.events(play, callback=cb, inventory="inv2")
                kinds.append(sorted(
                    (e["_event"], e.get("task", {}).get("name", ""),
                     ",".join(e.get("hosts", {}))) for e in events))
            self.assertEqual(kinds[0], kinds[1], strategy)

    def test_parity_unreachable_host(self):
        self.write("inv2", "ghost ansible_host=127.0.0.1 ansible_port=1 "
                           "ansible_connection=ssh\n")
        play = """
            - hosts: all
              gather_facts: false
              tasks:
                - ping:
        """
        env = {"ANSIBLE_SSH_RETRIES": "0"}
        results = []
        for cb in (None, REFERENCE):
            events = self.events(play, callback=cb, inventory="inv2", env=env)
            results.append([_normalise(strip_additions(e)) for e in events])
        mine, ref = results
        self.assertEqual(len(mine), len(ref))
        for a, b in zip(mine, ref):
            a, b = json.dumps(a, sort_keys=True), json.dumps(b, sort_keys=True)
            # The error text embeds timing and tool paths; compare structure.
            self.assertEqual(re.sub(r'"msg": "[^"]*"', "", a),
                             re.sub(r'"msg": "[^"]*"', "", b))


if __name__ == "__main__":
    unittest.main()
