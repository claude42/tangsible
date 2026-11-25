tangsible-vault(1) "Tangsible" "Tangsible Manual"

# NAME

tangsible-vault - edit individually vault-encrypted variables in place

# SYNOPSIS

*tangsible vault* [--vault-password-file _path_ | --ask-vault-pass] [--vault-id _id@source_]... [--encrypt-vault-id _label_] _filename_

# DESCRIPTION

*vault* provides an *ansible-vault edit*-like experience, but scoped to
individual variables inside the file instead of the whole file.

_filename_ is decrypted in place: every top-level *!vault*-tagged value
becomes its plain decrypted string, opened in the editor. On save, only
values that actually changed are re-encrypted; everything else — comments,
key order, previously unencrypted values, and any untouched ciphertext — is
spliced back byte-for-byte from the original file, not regenerated.

If _filename_ does not exist, it is created empty. An empty file is a file
with no variables: the editor opens on it, and every variable added there is
encrypted on save.

# OPTIONS

*--vault-password-file* _path_
	Read the vault password from _path_ (a file, or an executable that
	prints it).

*--ask-vault-pass*
	Prompt for the vault password.

*--vault-id* _id@source_
	A password and the vault id (label) it belongs to; the source is a file,
	an executable, or *prompt*. Without *id@* the id is *default*.
	May be repeated.

*--encrypt-vault-id* _label_
	The vault id a new value is encrypted with.

With none of these, the password comes from *$ANSIBLE_VAULT_PASSWORD_FILE*,
then *vault_password_file* in *ansible.cfg*, then a prompt. The vault ids
configured in *$ANSIBLE_VAULT_IDENTITY_LIST* or *vault_identity_list* in
*ansible.cfg* are used first, then every *--vault-id*. The id for new
values defaults to *$ANSIBLE_VAULT_ENCRYPT_IDENTITY* or
*vault_encrypt_identity*.

# VAULT IDS

Values may be encrypted under different vault ids (*$ANSIBLE_VAULT;1.2;AES256;label*),
each with its own password. Every value is decrypted with the password whose
id matches the label in its header first, then with any other password you
gave, as Ansible does: a label is a hint, any password that decrypts the
value works.

A changed value is encrypted again with the id and password it had. A new
value uses *--encrypt-vault-id*; without one, the vault id all of the file's
values share if you gave its password, otherwise the first password.

A value that none of the given passwords decrypts (another team's, say) is
shown as *<vaulted: label>* and saved unchanged as long as the placeholder is
left alone. Changing it is refused; deleting the line removes the value. This
also holds when no value at all can be decrypted (say a file whose values all
use another vault id): the file opens with placeholders only, and a value
added is encrypted with the id given by *--encrypt-vault-id*, else the first
password.

# EXIT STATUS

*0*
	The file was saved successfully, or the editor closed with no
	changes at all (in which case nothing was written).

*1*
	A genuine failure: the file couldn't be read or decrypted (wrong
	password, corrupted data), or some other I/O error.

*2*
	Usage error: no _filename_ was given, both password flags were
	given together, or *--encrypt-vault-id* names an id no password was
	given for.

*130*
	The session was reverted — either explicitly, via the fix-or-revert
	prompt, or by Ctrl-C reaching *tangsible* directly (only possible
	outside an active editor session: before it first opens, or between
	reopen rounds). The target file is left exactly as it was.

# LIMITATIONS

Only top-level values of a flat YAML mapping are handled, and not a file
that is encrypted as a whole.

# SEE ALSO

*tangsible*(1), *ansible-vault*(1)
