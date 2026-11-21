# Statistics: where did the time go? Hosts, plays and tasks, as bars and as
# timelines, once a run has ended.
#
# Render from the repository root (needs vhs, ttyd, ffmpeg, tangsible on PATH):
#
#   vhs screenrecordings/stats.tape
#
# Everything runs on localhost under /tmp/tangsible-demo; see demo-project/.

# poster: 12
Output screenrecordings/output/stats.gif

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
Type "export PS1='$ '" Enter
Type "rm -rf /tmp/tangsible-demo && cd screenrecordings/demo-project && rm -rf .tangsible && clear" Enter
Sleep 1s
Show

# 1. A run, with a failing host: only its end is recorded
Hide
Type "tangsible run site.yml" Sleep 300ms Enter
Wait+Screen /Playbook failed/
Show
Sleep 3s

# 2. Once it has ended, s opens the statistics: first the per-host overview
Type "s"
Sleep 3500ms

# ... then the time each host spent
Tab
Sleep 4s

# 3. Slowest plays, bars with the name inside
Tab
Sleep 3500ms

# 4. The plays in the order they ran, on a timeline
Tab
Sleep 3500ms

# 5. Every task, longest first
Tab
Sleep 4s

# 6. One play's tasks on a timeline; pick the play with the arrow keys...
Tab
Sleep 3s
Right
Sleep 3s
Right
Sleep 3s

# ... or from a list
Type "p"
Sleep 1s
Type "common"
Sleep 1500ms
Enter
Sleep 3s

# 7. The help lists what each tab shows
Type "?"
Sleep 4s
Escape
Sleep 500ms
Escape
Sleep 1s
Type "q"
Sleep 1s
