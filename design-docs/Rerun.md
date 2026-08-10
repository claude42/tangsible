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

## Re-run from the command line

Prerequisite: we have to change the way tangsible is invoked to a command
verb syntax, i.e instead of calling

   tangsible site.yml -l somehost --tags sometag

this should in the future be

   tangsible run site.yml -l somehost --tags sometag

Rationale: for this feature (and some more I have in mind) we'll need
different verbs.

* Tangsible shall save the history of its previous invocations in the local
  .tangsible file
* When tangsible is started with the verb "rerun" instead of "run"
  * it shall bring up the same Re-run dialog as describe above
  * where sensible the elements of the dialog shall be filled with known
    information from the previous run (read from the .tangsible file) (i.e.
    hosts, tags)
  * if -l or --tags is specified on the command line, these should have
    precedence over the data from the last run
* The user has to confirm the dialog by pressing enter before the playbook is
  run
* So tangsible could be called like
  * tangsible rerun
    -> would run the same playbook with the same arguments as last time
  * tangsible rerun someplaybook.yml
    -> would run someplaybook.yml with the same arguments as the last time
    tangsible was run for someplaybook.yml (or no arguments if never run for
    this playbook
  * tangsible rerun -l somehost
    -> would run tangsible with the same playbook and arguments as in the
    last invocation but only for host somehost


