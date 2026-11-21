# Execution environments, part 2: build the image, push it to a registry.
#
# Render from the repository root (needs vhs, ttyd, ffmpeg, podman,
# ansible-builder, network, tangsible on PATH, and a registry on
# localhost:5000: render.sh starts a throw-away one):
#
#   vhs screenrecordings/ee-build.tape
#
# The build runs while the recording is hidden (it takes a minute or two).

# poster: 10
Output screenrecordings/output/ee-build.gif

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
Type "tangsible ee init site.yml >/dev/null && clear" Enter
Sleep 8s
Show

# 1. Name the image: the tags are where ee build puts it and ee push sends it
Type "printf '  tags:\n    - localhost:5000/demo/service-ee:1.0\n    - localhost:5000/demo/service-ee:latest\n' >> execution-environment.yml" Enter
Sleep 800ms
Type "tail -6 execution-environment.yml" Enter
Sleep 3s
Type "clear" Enter

# 2. Build it
Type "tangsible ee build" Sleep 300ms Enter
Sleep 6s
Hide
Wait+Screen /To run in it/
Show
Sleep 3s
Type "podman images | grep -E 'REPOSITORY|service-ee'" Enter
Sleep 3s
Type "clear" Enter

# 3. Push it: tangsible checks the names first and asks before pushing
Type "tangsible ee push" Sleep 300ms Enter
Sleep 4s
Type "y" Enter
Hide
Sleep 30s
Show
Sleep 4s
