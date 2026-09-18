# Notification

## Situation

Playbooks regularly take a rather long time so the user might not watch all
the time. Still they might want to be informed about certain events - mainly
end of the playbook or when some problem occurs.

## Idea

Send a notification on certain events:
* Playbook has finished
* Task failed
* potentially more in the future

Type of notification could be either

* desktop notification via OSC 9 / OSC 777 / or Kitty's OSC 99
* a simple BEL

## Configuration

Notification behavior should be configurable. Therefore we introduce two new
configuration directives under [general]:

### notify_playbook_finished, notify_task_failed

The possible values for notify_playbook_finished and notify_task_failed are

* off
* osc9
* osc777
* osc99
* bell - tangsible will send BEL to the terminal

Defaults should be off for both.

### notify_task_failed_max

Maximum number of task failed notifications per playbook run. Default is 5.
Failure counter will reset at each rerun. When the limit is reached, one
final notification is sent stating how many further task failures were
suppressed for the rest of the run; no more notify_task_failed notifications
follow after that.


## Notification content

OSC 9 only carries a body string; OSC 777 and OSC 99 support a separate
title + body; BEL carries no text at all, so content only applies to the
osc9/osc777/osc99 types.

* notify_playbook_finished - title "Tangsible", body names the playbook and
  its outcome: "<playbook> finished successfully", "<playbook> finished
  with failures", or "<playbook> finished (unreachable hosts)" for the
  benign-unreachable case.
* notify_task_failed - title "Tangsible", body names the failed task and
  host: "<task> failed on <host>".
* notify_task_failed_max suppression notice - title "Tangsible", body:
  "<n> further task failures suppressed for this run".

## More details

* notify_task_failed should limit the number of notifications fired per run.
  See configuration option notify_task_failed_max. Default should be 5
* ignore_errors: true cannot actually be excluded - ansible.posix.jsonl
  never includes that field in the events it emits at all (confirmed by
  reading its source directly). Decision: drop this exclusion for v1;
  notify_task_failed fires for every recorded failure, ignore_errors or
  not. Revisit once design-docs/OwnCallbackPlugin.md's own callback fork
  ships and can carry that field.
* Notify_playbook_finished should not be fired when it's a direct, immediate
  result of a user interaction (e.g. ctrl-c or pre-flight-gate failures)
* benign-unreachable should count as "finished"
* Each rerun can trigger notify_playbook_finished
* Don't consider terminal focus for now
* no cli override
