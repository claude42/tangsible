# Tabbed UI proposal

## Current situation

The drill down page contains a lot of subsections which we are currently just
rendering on one big page. This could be visualized more nicely if we put the
different sections into different tabs.

The template page right now just shows the output of the template. But my
idea to toggle between template source and rendered version. To implement
this, tabs would also be a very nice solution.

## Rough sketch

This is a rough sketch for how the drill down view could look like.
Unfortunately text in this document doesn't allow colors or inverse
characters so this will be a bit lacking. I'll give a few more explanations
below

+-------------------------------------------------------------------------------+
| hades - claude : Create ssh aliases for all hosts                             |
|                                                                               |
|  Task   Output   Task definition   Resolved   Play definition   Details       |
| ----------------------------------------------------------------------------- |
| - name: Create ssh aliases for all hosts                                      |
|   ansible.builtin.copy:                                                       |
|     content: |                                                                |
|       function {{ item }}                                                     |
|           moshtmux-fish {{ item }}                                            |
|       end                                                                     |
|     dest: "/home/claude/.config/fish/functions/{{ item }}.fish"               |
|     owner: claude                                                             |
|     group: claude                                                             |
|     mode: '0644'                                                              |
|   loop: "{{ all_hosts }}"                                                     |
| [/home/claude/ansible/roles/claude/tasks/main.yml, line 102]                  |
|                                                                               |
|                                                                               |
|                                                                               |
|                                                                               |
| ↑/↓ or j/k/h/l scroll  g/home top  G/end bottom  ←/→: prev/next host          |
+-------------------------------------------------------------------------------+

So,
* title and status line as always
* below that a line with the different tabs (in this case the tabs are
  "Task", "Output", "Task definition", and so on).
* the active tab shall be rendered in a different color and in inverted
  characters
* tab can be changed using tab key or shift-tab key or via a mouse click
* below the tabs there's a separator line
* below that, there's the content.

## The widget

tview has no built-in tabs component (checked directly against its own
source - same as when a filterable-list widget was looked into earlier).
Unlike `treeList` (`treelist.go`), though, this doesn't need a from-scratch
`Primitive` - `tview.Pages` already *is* the "switch between named content
panels" building block, and it's already used throughout this app
(`"main"`/`"output"`/`"filter"`/`"search"`/`"rerun"`). What's actually
missing is just the tab-bar header itself (a `TextView` using the same
inline `[color]...[-]` tag convention already used everywhere else for the
tree's own coloring - no new rendering mechanism needed) plus the glue to
switch pages and re-highlight the active tab, and the mouse-click-to-tab
hit-testing (summing each tab label's own rendered width to map an x
coordinate to a tab index).

Built once as a single, shared component (a new `tabs.go`, in the spirit
of `treelist.go`) that both the drill-down view and the template page use,
rather than two separate implementations.

## Drill-down: content mapping

Six tabs, matching the sketch above exactly - each shown only when that
particular task actually has something for it (matching
`formatHostOutput`'s own existing "omit rather than show empty" rule
throughout, just applied to tabs instead of stacked sections):

1. **Task** - the existing summary block (Name/Action/Role/Host/Status).
   Always present.
2. **Output** - merges the current Output/Items/Warnings/Error sections
   into one tab; present whenever *any* of those four has content, absent
   only if all four are empty. Their own individual headers/underlines/
   colors (`sectionLabel`) are kept exactly as they render today within
   this tab's own content - they still don't become one big undifferentiated
   blob, just live under one tab instead of one always-visible page.
3. **Task definition** - present when the source lookup succeeds.
4. **Resolved** - conditional, and not just on whether the lookup
   succeeds: absent while the background resolve is still running (no
   "Resolving..." placeholder - a follow-up revision of this feature,
   design-docs/Drilldown, Resolved Values.md's own "Triggering, caching,
   and display" section covers the reasoning), absent on success if the
   resolved text turns out byte-for-byte identical to "Task definition"'s
   own raw source (nothing this tab would add), present once resolved to
   something genuinely different, and present on a genuine resolve error
   (real information the other tabs don't carry).
5. **Play definition** - present when `task.Play != nil` and its own
   source lookup succeeds (same condition as today).
6. **Details** - the full JSON. Always present.

## Template page: content mapping

Two tabs: **Rendered**, **Source**. A Jinja failure replaces Rendered's
own content with the error text, exactly like today's single-view
behavior - just relocated into a tab rather than swapping the whole view.

## Mechanics

* **Tab/Shift-Tab's existing side effect** - closing the output view, via
  `tview.TextView`'s own default "done key" handling (`Tab`/`Backtab` are
  both in its fixed set) - becomes a deliberate override once tabs exist:
  `Escape`/`Enter` remain the only way to close/back out, with Tab/
  Shift-Tab intercepted first and repurposed as next/previous tab.
* **Scroll position resets to top on every tab switch** - matches the
  existing `ScrollToBeginning()` convention already used when opening a
  new host's output - rather than remembering a separate scroll offset per
  tab. Switching tabs is usually "I want to see the top of this new
  thing" anyway.
* **Mouse click-to-select is part of this, not a later add-on** - the
  sketch itself lists it as one of the three ways to switch tabs.

