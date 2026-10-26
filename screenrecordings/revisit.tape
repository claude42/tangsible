# Revisit: reopen the results of earlier runs after tangsible has exited.
#
# Render from the repository root (needs vhs, ttyd, ffmpeg, tangsible on PATH):
#
#   vhs screenrecordings/revisit.tape
#
# Everything runs on localhost under /tmp/tangsible-demo; see demo-project/.

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

# Setup, not part of the recording: three earlier runs - the first one fails
# (web02 can't start nginx), a retry against the web servers, a full run
Hide
Type "export PS1='$ '" Enter
Type "rm -rf /tmp/tangsible-demo && cd screenrecordings/demo-project && rm -rf .tangsible && clear" Enter
Type "tangsible run site.yml" Enter
Wait+Screen /Playbook (completed|failed)/
Sleep 3s
Type "q"
Sleep 1s
Type "q"
Sleep 1s
Type "clear" Enter
Type "tangsible run site.yml --limit webservers" Enter
Wait+Screen /Playbook (completed|failed)/
Sleep 3s
Type "q"
Sleep 1s
Type "q"
Sleep 1s
Type "clear" Enter
Type "tangsible run site.yml" Enter
Wait+Screen /Playbook (completed|failed)/
Sleep 3s
Type "q"
Sleep 1s
Type "q"
Sleep 1s
Type "clear" Enter
Show

# 1. All previous runs, newest first
Type "tangsible revisit" Sleep 300ms Enter
Sleep 4s

# 2. Open the oldest one: the failed run, exactly as it looked back then
Down
Sleep 500ms
Down
Sleep 1s
Enter
Sleep 4s

# 3. Drill into the failure
Enter
Sleep 3s
Tab
Sleep 4s
Escape
Sleep 1s

# 4. Back to the list, open the latest run
Escape
Sleep 2s
Up
Sleep 500ms
Up
Sleep 1s
Enter
Sleep 4s

Escape
Sleep 1s
Type "q"
Sleep 500ms
