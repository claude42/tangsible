# Vault: edit individual encrypted variables, keep everything else as it was.
#
# Render from the repository root (needs vhs, ttyd, ffmpeg, tangsible on PATH):
#
#   vhs screenrecordings/vault.tape
#
# Everything runs on localhost under /tmp/tangsible-demo; see demo-project/.

Output screenrecordings/output/vault.gif

Require tangsible

Set Shell "bash"
Set FontSize 14
Set Width 1200
Set Height 800
Set Framerate 10
Set TypingSpeed 30ms
Set WaitTimeout 60s
Set PlaybackSpeed 1.5
Set Theme "iTerm2 Dark Background"

# Setup, not part of the recording
Hide
Type "export PS1='$ ' GIT_PAGER=cat" Enter
Type "cd screenrecordings/demo-project && git checkout -q -- group_vars/databases.yml && clear" Enter
Sleep 500ms
Show

# 1. A vars file: plain values, comments and individually vaulted secrets
Type "cat group_vars/databases.yml" Sleep 300ms Enter
Sleep 5s

# 2. Open it - every secret shows up decrypted, comments intact
Type "clear" Enter
Type "tangsible vault group_vars/databases.yml --vault-password-file .vault_pass" Sleep 300ms Enter
Sleep 6s

# 3. Change one secret, add a new one
Type "10G$BC"
Sleep 1s
Type "n3w-p4ssw0rd-2026"
Sleep 2500ms
Escape
Type "Go"
Enter
Type "smtp_password: m41l-s3cr3t"
Sleep 500ms
Escape
Type "O# Outgoing mail relay"
Sleep 1s
Escape
Sleep 3500ms
Type ":wq" Sleep 500ms Enter
Sleep 3s

# 4. What changed on disk? Only the touched values.
Type "git diff -U1 group_vars/databases.yml" Sleep 300ms Enter
Sleep 6s

# Leave the repository clean
Hide
Type "git checkout -q -- group_vars/databases.yml"
Enter
