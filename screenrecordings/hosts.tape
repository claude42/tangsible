# Hosts: browse the inventory as a tree, open a host, then the flat host list
# and the single-host view - all three share one detail view.
#
# Render from the repository root (needs vhs, ttyd, ffmpeg, tangsible on PATH):
#
#   vhs screenrecordings/hosts.tape
#
# Everything runs on localhost; the facts shown come from the canned fact
# cache in demo-project/.fact-cache.

Output screenrecordings/output/hosts.gif

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

# Setup, not part of the recording: a finished run, so the hosts have history
Hide
Type "export PS1='$ '" Enter
Type "rm -rf /tmp/tangsible-demo && cd screenrecordings/demo-project && rm -rf .tangsible && clear" Enter
Type "tangsible run site.yml" Enter
Wait+Screen /r: re-run/
Sleep 3s
Type "q"
Sleep 1s
Type "q"
Sleep 1s
Type "clear" Enter
Sleep 500ms
Show

# 1. The inventory as a tree: groups are nodes, hosts are leaves
Type "tangsible inventory" Sleep 300ms Enter
Sleep 3s
Enter
Down
Down
Down
Enter
Sleep 3s

# 2. Open a host: summary, groups, plays, variables, everything known
Down
Enter
Sleep 5s
Tab
Sleep 3s
Tab
Sleep 3s
Tab
Sleep 3s
Tab
Sleep 3s
Escape
Sleep 1s
Type "q"
Sleep 500ms

# 3. The same detail view from a flat list of hosts
Type "clear" Enter
Type "tangsible hosts" Sleep 300ms Enter
Sleep 2s
Down
Enter
Sleep 4s
Right
Sleep 3s
Escape
Sleep 500ms
Type "q"
Sleep 500ms

# 4. Or straight to one host
Type "clear" Enter
Type "tangsible host db01" Sleep 300ms Enter
Sleep 5s
Type "q"
Sleep 500ms
