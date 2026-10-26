# Diff mode: compare two runs of the same playbook after editing a template.
#
# Render from the repository root (needs vhs, ttyd, ffmpeg, tangsible on PATH):
#
#   vhs screenrecordings/diff.tape
#
# Everything runs on localhost under /tmp/tangsible-demo; see demo-project/.

# poster: 21
Output screenrecordings/output/diff.gif

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

# Setup, not part of the recording: clean state, warm up so the first
# recorded run is a quiet, idempotent one
Hide
Type "export PS1='$ ' TANGSIBLE_CALLBACK_DIR=$PWD/callback" Enter
Type "rm -rf /tmp/tangsible-demo && cd screenrecordings/demo-project && rm -rf .tangsible && git checkout -q -- roles/nginx/templates && clear" Enter
Type "ansible-playbook site.yml >/dev/null; ansible-playbook site.yml >/dev/null; clear; echo war''med" Enter
Wait+Screen /warmed/
Type "clear" Enter
Sleep 500ms
Show

# 1. A first run: nothing to do
Type "tangsible run site.yml" Sleep 300ms Enter
Wait+Screen /Playbook completed successfully/
Sleep 1500ms
Type "q"
Sleep 500ms

# 2. Edit the nginx template and run again
Type "sed -i 's/worker_processes auto/worker_processes 4/' roles/nginx/templates/nginx.conf.j2" Sleep 500ms Enter
Sleep 500ms
Type "tangsible run site.yml" Sleep 300ms Enter
Wait+Screen /Playbook completed successfully/
Sleep 1500ms

# 3. Diff mode: pick the previous run, only what differs is shown
Type "d"
Sleep 1500ms
Enter
Sleep 2500ms

# ... and drill into a host: unified diff of the two runs' results
Down
Sleep 600ms
Enter
Sleep 1s
Down
Sleep 600ms
Enter
Sleep 2500ms
Tab
Sleep 3500ms
Escape
Sleep 500ms
Escape
Sleep 500ms
Escape
Sleep 1s
Type "q"
Sleep 500ms

# Leave the repository clean
Hide
Type "git checkout -q -- roles/nginx/templates"
Enter
