# Revisit and compare: run a playbook, reopen its results after tangsible
# has exited, diff them against an older run, then re-run from there.
#
# Render from the repository root (needs vhs, ttyd, ffmpeg, tangsible on PATH):
#
#   vhs screenrecordings/revisit.tape
#
# Everything runs on localhost under /tmp/tangsible-demo; see demo-project/.

# poster: 26
Output screenrecordings/output/revisit.gif

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

# Setup, not part of the recording: two earlier runs - the first one fails
# (web02 can't start nginx), the second is a retry against the web servers
Hide
Type "export PS1='$ '" Enter
Type "rm -rf /tmp/tangsible-demo && cd screenrecordings/demo-project && rm -rf .tangsible && clear" Enter
Type "tangsible run site.yml" Enter
Wait+Screen /Playbook (completed|failed)/
Sleep 3s
Type "q"
Sleep 1s
Type "clear" Enter
Type "tangsible run site.yml --limit webservers" Enter
Wait+Screen /Playbook (completed|failed)/
Sleep 3s
Type "q"
Sleep 1s
Type "clear" Enter
Show

# 1. A full run, now that the problem is gone
Type "tangsible run site.yml" Sleep 300ms Enter
Wait+Screen /Playbook (completed|failed)/
Sleep 2s
Type "q"
Sleep 500ms

# 2. Reopen it from the list of all runs, newest first
Type "tangsible revisit" Sleep 300ms Enter
Sleep 3s
Enter
Sleep 2s

# 3. Look around: drill into a task's result on one host
Up 30
Sleep 500ms
Down 7
Sleep 1s
Enter
Sleep 1s
Down
Sleep 500ms
Enter
Sleep 2s
Tab
Sleep 2s
Escape
Sleep 1s

# 4. Diff against another run: pick the oldest, the failed one
Type "d"
Sleep 2s
Down
Sleep 800ms
Enter
Sleep 3s

# 5. Only what differs is shown; drill into the task that now works
Down 10
Sleep 1s
Enter
Sleep 1s
Down
Sleep 500ms
Enter
Sleep 3s
Tab
Sleep 3s
Escape
Sleep 500ms
Escape
Sleep 1s
Escape
Sleep 1500ms

# 6. Back in the latest run: re-run it, the dialog is pre-filled from that run
Type "r"
Sleep 3s
Enter
Sleep 4s

# 7. Stop after a few lines (the interrupted run joins the list)
Type "q"
Sleep 2s
Type "q"
Sleep 2s
Type "q"
Sleep 500ms
