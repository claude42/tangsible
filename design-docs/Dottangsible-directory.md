# Idea: split up the current .tangsible

## Current situation

.tangsible contains both configuration directives (like default_tree_state)
as well as the invocation history. Combining this has a few negative side
effects as what can be considered the configuration file is beeing written
over every run and so are all configuration parameters - even if at their
default value. Comments and other artefacts might get lost as well. OTOH a
user can screw up the history while editing a configuration file. All not
good.

## Proposed solution

Important: this is a breaking change. But as currently I'm still the only
user, that shouldn't be much of a problem.

Idea is to split this into two parts and put it into a separate directory,
i.e.

.tangsible/config - should contain all settings the user should be able to
modify

.tangsible/state - should contain all state information (including history as
well as the last=xxx setting), that the application saves itself.

That's basically it. No migration code is necessary, I will discard the
existing .ansible file myself when updating.
