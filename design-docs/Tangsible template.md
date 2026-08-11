# tangsible template

## Problem

Debugging Jinja 2 can be challenging sometimes. Using tools outside Ansible
can lead to different results. Within ansible requires crafting suitable
playbooks and roles just to test templates.

## Idea

Add a new verb

  tangsible template <template>

What it does is
  * create a small, temporary stub playbook with an ansible.builtin.template
    action referencing the template
  * run the playbook using ansible-playbook and print the resulting file to
    sdtout (the user can decide for himself wether they want to pipe it to
    a file)
    * ansible-playbook will then automatically pick up any potentially
      available host or group vars
    * TBD: in case a template is part of a role it might make sense to also
      create play under <rolename>. Rationale: template might contain
      variables defined in this role
    * of course the user can use -e on the command line to specify extra
      variables
  * clean up afterwards


