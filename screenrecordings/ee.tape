# Execution environments: draft an execution-environment.yml from a playbook,
# verify an image built from it, build and push it, run the playbook in it.
#
# Render from the repository root (needs vhs, ttyd, ffmpeg, podman,
# ansible-builder, network, tangsible on PATH and a podman login to
# code.aw.net, where the image is pushed):
#
#   screenrecordings/render.sh ee
#
# render.sh sets CONTAINERS_CONF (the container's DNS can't reach the network
# when the host resolver is on 127.0.0.1).
#
# The builds, the push and the image start run while the recording is hidden
# (about two minutes); the frames in between are simply left out.
#
# Everything runs on localhost; see demo-ee-project/.

# poster: 12
Output screenrecordings/output/ee.gif

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
Type "rm -rf /tmp/tangsible-ee-demo && cd screenrecordings/demo-ee-project && rm -rf .tangsible execution-environment.yml* && clear" Enter
Sleep 1s
Show

# 1. The playbook needs a collection the stock images lack
Type "grep -n 'community' site.yml" Enter
Sleep 4s

# 2. Draft the definition from the playbook; --resolve-packages also finds the
# package that provides jq, which the base image lacks (the lookup downloads
# repository metadata, about 20 s, and is cut)
Type "tangsible ee init site.yml --resolve-packages" Sleep 300ms Enter
Sleep 3s
Hide
Wait+Screen /collection\(s\)/
Show
Sleep 7s
Type "clear" Enter

# 3. Read the draft (the one file used from here to the end): what was found,
# what is left to decide
Type "less execution-environment.yml" Enter
Sleep 10s
Type@1ms " "
Sleep 6s
Type "q"
Sleep 500ms
Type "clear" Enter

# 4. Build an image from it and check the playbook against it (the minute or
# two of building is cut)
Type "tangsible ee verify site.yml" Sleep 300ms Enter
Sleep 5s
Hide
Wait+Screen /Binaries:/
Show
Sleep 6s
Type "clear" Enter

# 5. Name the image (not recorded): the tags are where ee build puts it and
# ee push sends it
Hide
Type "printf '  tags:\n    - code.aw.net/claude/tangsible-demo-ee:1.0\n    - code.aw.net/claude/tangsible-demo-ee:latest\n' >> execution-environment.yml" Enter
Sleep 800ms
Type "clear" Enter
Show

# 6. Build it (cut again once the cache has made it quick to read)
Type "tangsible ee build" Sleep 300ms Enter
Sleep 5s
Hide
Wait+Screen /To run in it/
Show
Sleep 4s
Type "podman images | grep -E 'REPOSITORY|tangsible-demo-ee'" Enter
Sleep 4s
Type "clear" Enter

# 7. Push it: tangsible checks the names first and asks before pushing
Type "tangsible ee push" Sleep 300ms Enter
Sleep 5s
Type "y" Enter
Hide
Wait /\$$/
Show
Sleep 4s
Type "clear" Enter

# 8. Run the playbook in the image just built: --ee names it (or
# [execution_environment] in config.toml)
Type "tangsible run site.yml --ee code.aw.net/claude/tangsible-demo-ee:latest" Sleep 300ms Enter
Hide
Wait+Screen /Playbook completed/
Show
Sleep 6s

# 9. The raw output (o), scrolled to its first line (Home, typed as a raw
# escape sequence - VHS has no Home key): the exact engine command, with
# the project mounted at its own path and the event stream in a mounted dir
Type "o"
Sleep 500ms
Escape
Type@1ms "[H"
Sleep 8s
Escape
Sleep 500ms

# 10. Drill down into a task: it ran in the container, the paths are the host's
Up
Sleep 800ms
Enter
Sleep 4s
Escape
Sleep 500ms
Type "q"
Sleep 1s
