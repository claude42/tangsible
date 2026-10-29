# tangsible_events

Tangsible's Ansible callback plugin: an `aggregate` callback that writes one
JSON document per playbook event to the file descriptor named by
`TANGSIBLE_EVENT_FD` (normal display output when unset), alongside whatever
stdout callback the user has configured. Apache-2.0 licensed (see the top-level `LICENSE`).

The document layout follows `ansible.posix.jsonl`; additions are listed in
the `notes` of `ansible-doc -t callback tangsible_events`
(`ANSIBLE_CALLBACK_PLUGINS=callback`). Rationale and design:
`design-docs/OwnCallbackPlugin.md`.

Tests: `python3 -m unittest discover -s callback-tests -v` (from the repo root;
needs `ansible-playbook`).

## Provenance

The base is a clean-room reimplementation written from the public option
documentation (`ansible-doc`) and black-box observation of
`ansible.posix.jsonl`'s output; the plugin's source was not read or copied.
Tangsible's earlier bundled plugin was a GPL-3.0 fork of it and is gone. The
additions (aggregate loading, event fd, per-host start under lockstep
strategies, `host`, `is_handler`, `ignore_errors`, `v2_playbook_on_notify`)
were written from the behavior documented in
`design-docs/OwnCallbackPlugin.md` and `internal/playbook/events.go`, not
transcribed from that fork. `ansible.posix.jsonl` is still executed (never
read) by the parity tests.
