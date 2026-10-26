# Template: render a Jinja2 template through Ansible for one host, edit, re-render.
#
# Render from the repository root (needs vhs, ttyd, ffmpeg, tangsible on PATH):
#
#   vhs screenrecordings/template.tape
#
# Everything runs on localhost; see demo-project/.

Output screenrecordings/output/template.gif

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

# Setup, not part of the recording
Hide
Type "export PS1='$ ' EDITOR=vim" Enter
Type "cd screenrecordings/demo-project && git checkout -q -- roles/nginx/templates && clear" Enter
Sleep 500ms
Show

# 1. Render a template for the first inventory host
Type "tangsible template roles/nginx/templates/nginx.conf.j2" Sleep 300ms Enter
Wait+Screen /server_name db01/
Sleep 4s

# 2. The source next to the result
Tab
Sleep 3s
Tab
Sleep 1s

# 3. Same template, other host
Type "h"
Sleep 800ms
Ctrl+U
Type "web01"
Sleep 1s
Enter
Wait+Screen /server_name web01/
Sleep 3s

# 4. Edit it: use a variable that does not exist
Type "e"
Sleep 2s
Type "7Go"
Type "    client_max_body_size {{ nginx_max_body_size }};"
Sleep 3s
Escape
Type ":wq"
Sleep 500ms
Enter
Wait+Screen /is undefined/
Sleep 4s

# 5. Edit again: give it a default
Type "e"
Sleep 2s
Type "/nginx_max_body_size"
Enter
Type "ea | default('10m')"
Sleep 3s
Escape
Type ":wq"
Sleep 500ms
Enter
Wait+Screen /client_max_body_size 10m/
Sleep 4s

Type "q"
Sleep 500ms

# Not part of the recording: leave the demo project as it was
Hide
Type "git checkout -q -- roles/nginx/templates" Enter
Sleep 300ms
