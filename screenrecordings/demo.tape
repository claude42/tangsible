# Run/rerun walkthrough against the self-contained demo-project/.
#
# Render from the repository root (needs vhs, ttyd, ffmpeg, tangsible on PATH):
#
#   vhs screenrecordings/demo.tape
#
# Everything runs on localhost under /tmp/tangsible-demo; see demo-project/.

Output screenrecordings/output/demo.gif

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

# Setup, not part of the recording: clean state, short prompt
Hide
Type "export PS1='$ ' TANGSIBLE_CALLBACK_DIR=$PWD/callback" Enter
Type "rm -rf /tmp/tangsible-demo && cd screenrecordings/demo-project && rm -rf .tangsible && clear" Enter
Sleep 1s
Show

# 1. Live run: tasks appear as they happen, web02 fails at "Start nginx"
Type "tangsible run site.yml" Sleep 300ms Enter
Wait+Screen /Playbook failed/
Sleep 1500ms

# 2. The cursor already sits on the failed host: open its drill-down
Enter
Sleep 1500ms
Tab
Sleep 2s

# ... and step back to a task that has Diff and File tabs as well
Type@400ms "NNNN"
Sleep 1s
Tab
Sleep 2500ms
Tab
Sleep 3s
Tab
Sleep 2500ms
Escape
Sleep 500ms

# 3. Re-run only the failed host
Type "r"
Sleep 800ms
Tab 5
Sleep 300ms
Space
Sleep 1500ms
Enter
# ...no need to watch it to the end
Wait+Screen /Configure nginx +web02/
Sleep 1s
Type "q"
Sleep 1500ms
Type "q"
Sleep 800ms
