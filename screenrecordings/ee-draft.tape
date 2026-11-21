# Execution environments, part 1: draft an execution-environment.yml from a
# playbook, then verify that an image built from it can run the playbook.
#
# Render from the repository root (needs vhs, ttyd, ffmpeg, podman,
# ansible-builder, network, tangsible on PATH):
#
#   vhs screenrecordings/ee-draft.tape
#
# The build runs while the recording is hidden (it takes a minute or two).
# If the container's DNS can't reach the network (a host resolver on
# 127.0.0.1), run vhs with CONTAINERS_CONF pointing at a file with
# [containers] netns="host"; render.sh does that.

# poster: 6
Output screenrecordings/output/ee-draft.gif

Require tangsible

Set Shell "bash"
Set FontSize 14
Set Width 1200
Set Height 700
Set Framerate 10
Set TypingSpeed 30ms
Set WaitTimeout 600s
Set PlaybackSpeed 1.5
Set Theme "iTerm2 Dark Background"

# Setup, not part of the recording
Hide
Type "export PS1='$ '" Enter
Type "cd screenrecordings/demo-ee-project && rm -rf .tangsible execution-environment.yml* && clear" Enter
Sleep 1s
Show

# 1. The playbook needs a collection the stock images lack
Type "grep -n 'community' site.yml" Enter
Sleep 3s

# 2. Draft the definition from the playbook
Type "tangsible ee init site.yml" Sleep 300ms Enter
Wait+Screen /collection\(s\)/
Sleep 3500ms
Type "clear" Enter

# 3. Read the draft: what was found, what is left to decide
Type "less execution-environment.yml" Enter
Sleep 3s
Type@1ms " "
Sleep 3500ms
Type "q"
Sleep 500ms
Type "clear" Enter

# 4. Build an image from it and check the playbook against it
Type "tangsible ee verify site.yml" Sleep 300ms Enter
Sleep 6s
Hide
Wait+Screen /Binaries:/
Show
Sleep 5s
