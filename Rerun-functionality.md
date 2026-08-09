# Rerun functionality

Especially developing new playbooks or roles frequently requires rerunning the same
playbook. But this can also be the case in other scenarios, e.g.

* one host not online, rerun whole playbook for this host again
* Failed task because of something unforeseen - fix this, re-run
* ...

As these use cases have quite different requirements, the implementation will
tackle different areas of the app

## Interactive re-run

* After a playbook has run (successfully or failed), the user can initiate a
  re-run by pressing the r key.
* This will open the re-run dialog Which will present the following options
  * Type of re-run
    * Re-run full playbook
    * Re-run playbook starting with currently selected task
    * Re-run the currently selected task *only*
  * Tags
    * User can input tags to limit the tasks that should be re-run
    * If tags were already specified in the previous run (e.g. by --tags ...)
      then this text field should be pre-filled with these tags
  * Hosts
    * User can limit on which hosts the tasks should be re-run
    * If hosts were already specified in the previous run (e.g. by --l ...)
      then this text field should be pre-filled with these tags
  * Re-run shall be initiated by pressing return, dialog will be closed
    (without re-runnig) by ESC or q (only if no text field is active in that
    moment
