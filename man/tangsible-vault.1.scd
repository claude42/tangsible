tangsible-vault(1) "Tangsible" "Tangsible Manual"

# NAME

tangsible-vault - edit individually vault-encrypted variables in place

# SYNOPSIS

*tangsible vault* _filename_ [--vault-password-file _path_ | --ask-vault-pass]

# DESCRIPTION

*vault* provides an *ansible-vault edit*-like experience, but scoped to
individual variables inside the file instead of the whole file.

_filename_ is decrypted in place: every top-level *!vault*-tagged value
becomes its plain decrypted string, opened in the editor. On save, only
values that actually changed are re-encrypted; everything else — comments,
key order, previously unencrypted values, and any untouched ciphertext — is
spliced back byte-for-byte from the original file, not regenerated.

# EXIT STATUS

*0*
	The file was saved successfully, or the editor closed with no
	changes at all (in which case nothing was written).

*1*
	A genuine failure: the file couldn't be read or decrypted (wrong
	password, corrupted data), or some other I/O error.

*2*
	Usage error: no _filename_ was given, or both password flags were
	given together.

*130*
	The session was reverted — either explicitly, via the fix-or-revert
	prompt, or by Ctrl-C reaching *tangsible* directly (only possible
	outside an active editor session: before it first opens, or between
	reopen rounds). The target file is left exactly as it was.

# LIMITATIONS

Multiple vault-ids (Ansible's *$ANSIBLE_VAULT;1.2;AES256;label* form,
letting one file mix ciphertexts under different passwords) are not
supported — a labeled vault header is rejected the same way an unsupported
shape anywhere else is, rather than silently mishandled.

# SEE ALSO

*tangsible*(1), *ansible-vault*(1)
