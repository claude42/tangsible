# Execution environments, part 3: run the playbook inside a container image.
# The project is mounted into the container, the event stream crosses the
# container boundary, everything else looks like a normal run.
#
# Render from the repository root (needs vhs, ttyd, ffmpeg, podman and the
# image pulled, tangsible on PATH):
#
#   vhs screenrecordings/ee-run.tape
#
# Everything runs on localhost; see demo-ee-project/.

# poster: 12
Output screenrecordings/output/ee-run.gif

Require tangsible

Set Shell "bash"
Set FontSize 14
Set Width 1200
Set Height 700
Set Framerate 10
Set TypingSpeed 30ms
Set WaitTimeout 120s
Set PlaybackSpeed 1.5
Set Theme "iTerm2 Dark Background"

# Setup, not part of the recording
Hide
Type "export PS1='$ '" Enter
Type "rm -rf /tmp/tangsible-ee-demo && cd screenrecordings/demo-ee-project && rm -rf .tangsible execution-environment.yml* && clear" Enter
Sleep 1s
Show

# 1. Run it in an image: --ee names it (or [execution_environment] in config.toml)
Type "tangsible run site.yml --ee code.aw.net/claude/std-ee:latest" Sleep 300ms Enter
Wait+Screen /Playbook completed/
Sleep 1s

# 2. The run looks like any other. A debug task's output expands by itself,
# and the recap lists the messages
Sleep 5s

# 3. The raw output (o), scrolled to its first line (Home, typed as a raw
# escape sequence - VHS has no Home key): the exact engine command, with
# the project mounted at its own path and the event stream in a mounted dir
Type "o"
Sleep 500ms
Escape
Type@1ms "[H"
Sleep 7s
Escape
Sleep 500ms

# 4. Drill down into a task: it ran in the container, the paths are the host's
Up
Sleep 800ms
Enter
Sleep 3500ms
Escape
Sleep 500ms
Type "q"
Sleep 1s
