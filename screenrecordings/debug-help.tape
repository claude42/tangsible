# Context help in the debugger: a click on a key of the task as written
# explains it (a module, one of its options, a task keyword), a click on a
# variable shows its value and where it is defined.
#
# Render from the repository root (needs vhs, ttyd, ffmpeg, tangsible on PATH):
#
#   vhs screenrecordings/debug-help.tape
#
# Everything runs on localhost under /tmp/tangsible-demo; see demo-project/.
#
# VHS has no mouse and no function keys, so each is typed as the raw terminal
# sequence after an ESC: F5 = ESC [15~, F9 = ESC [20~, and a click is an xterm
# mouse report, press then release: ESC [ < 0 ; column ; row M / m. The columns
# and rows belong to the 1200x700 terminal below (117x35) and the tab layout
# of this playbook.

# poster: 12
Output screenrecordings/output/debug-help.gif

Require tangsible

Set Shell "bash"
Set FontSize 14
Set Width 1200
Set Height 700
Set Framerate 10
Set TypingSpeed 30ms
Set WaitTimeout 60s
Set PlaybackSpeed 1.5
Set Theme "iTerm2 Dark Background"

# Setup, not part of the recording: clean state, warm up so the host is
# idempotent (no Diff tab to count)
Hide
Type "export PS1='$ '" Enter
Type "rm -rf /tmp/tangsible-demo && cd screenrecordings/demo-project && rm -rf .tangsible && clear" Enter
Type "ansible-playbook site.yml >/dev/null; ansible-playbook site.yml >/dev/null; clear; echo war''med" Enter
Wait+Screen /warmed/
Type "clear" Enter
Sleep 500ms
Show

# 1. Break before "Configure nginx" and run to it
Type "tangsible debug site.yml" Sleep 300ms Enter
Wait+Screen /Schedule nightly backup/
Sleep 1500ms
Down@300ms 6
Sleep 500ms
Escape
Type@1ms "[20~"
Sleep 1s
Escape
Type@1ms "[15~"
Wait+Screen /Status:/
Sleep 3s

# 2. The task as written
Tab@600ms 3
Sleep 2500ms

# 3. A module option: dest
Escape
Type@1ms "[<0;45;6M"
Escape
Type@1ms "[<0;45;6m"
Sleep 4s
Escape
Sleep 700ms

# 4. A task keyword: notify
Escape
Type@1ms "[<0;43;8M"
Escape
Type@1ms "[<0;43;8m"
Sleep 4s
Escape
Sleep 700ms

# 5. The module itself: template
Escape
Type@1ms "[<0;43;4M"
Escape
Type@1ms "[<0;43;4m"
Sleep 3500ms
Escape
Sleep 700ms

# 6. A variable: its value now and where it is defined
Escape
Type@1ms "[<0;54;6M"
Escape
Type@1ms "[<0;54;6m"
Sleep 4s
Escape
Sleep 700ms
Escape
Type@1ms "[<0;72;6M"
Escape
Type@1ms "[<0;72;6m"
Sleep 4s
Escape
Sleep 1s
Escape
Sleep 500ms
Type "q"
Sleep 1s
