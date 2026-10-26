# Debug: breakpoints, variable watch, change a variable and redo a task.
#
# Render from the repository root (needs vhs, ttyd, ffmpeg, tangsible on PATH):
#
#   vhs screenrecordings/debug.tape
#
# Everything runs on localhost under /tmp/tangsible-demo; see demo-project/.
#
# VHS has no function keys, so each F-key is typed as the raw terminal escape
# sequence (F5 = ESC [15~, F9 = ESC [20~, F10 = ESC [21~, F11 = ESC [23~,
# F12 = ESC [24~).

Output screenrecordings/output/debug.gif

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

# Setup, not part of the recording: clean state, warm up so the recorded run
# starts from an idempotent host (and the redo is the only change)
Hide
Type "export PS1='$ '" Enter
Type "rm -rf /tmp/tangsible-demo && cd screenrecordings/demo-project && rm -rf .tangsible && clear" Enter
Type "ansible-playbook site.yml >/dev/null; ansible-playbook site.yml >/dev/null; clear; echo war''med" Enter
Wait+Screen /warmed/
Type "clear" Enter
Sleep 500ms
Show

# 1. The whole predicted task tree is there before anything runs
Type "tangsible debug site.yml --all-vars" Sleep 300ms Enter
Wait+Screen /Schedule nightly backup/
Sleep 2s

# 2. Arm breakpoints (F9) on "Configure nginx" and "Start nginx"
Down@300ms 6
Sleep 500ms
Escape
Type@1ms "[20~"
Sleep 1s
Down@300ms 4
Sleep 500ms
Escape
Type@1ms "[20~"
Sleep 2s

# 3. Run (F5) - stops at the first breakpoint
Escape
Type@1ms "[15~"
Wait+Screen /Status:/
Sleep 3s

# 4. The task as written, then with all variables resolved
Tab@600ms 3
Sleep 5s
Tab
Sleep 6s

# 5. Watch a variable, then change it
Escape
Sleep 500ms
Type "v"
Sleep 500ms
Type "domain"
Sleep 1500ms
Enter
Sleep 2s
Type 'domain = "staging.example.org"'
Sleep 1s
Enter
Sleep 2500ms
Escape
Sleep 1s

# 6. Redo the task (F12) with the new value
Escape
Type@1ms "[24~"
Wait+Screen /Status: +Changed/
Sleep 2s
Tab@600ms 2
Sleep 5s

# 7. Run on (F5): the breakpoint pauses on each host, then the playbook ends
Escape
Sleep 300ms
Escape
Type@1ms "[15~"
Sleep 4s
Escape
Type@1ms "[15~"
Sleep 3s
Escape
Type@1ms "[15~"
Sleep 6s
Type "q"
Sleep 1s
