# Vault

## Situation

Handling encrypted variables is usually handled with ansible-vault which
provides basic functionalities to encrypt / decrypt whole files and to
encrypt simple values.

But many actions (especially on individual variables) are very cumbersome to
perform. E.g.

- overwriting an existing variable in a file
  - remove variable from vault.yml first
  - ansible-vault encrypt_string 'value' --name variable
  - paste its output back into vault.yml
- or decrypting a single value
  - either copy paste just the encrypted value into a new file and decrypt
    the new file
  - or write a separate playbook to do it

The fact that this is a task that users only perform infrequently makes it
even more difficult, frequently requires looking up information and trial and
error...

Consensus seems to use full file encryption and add a second yaml source file
which references the variables in the encrypted file but that just feels like
an ugly workaround.

## Proposed solution

Implement functionality in ansible which simplifies these standard operations
on individual variables. significantly. The idea would be to provide an
interface which basically feels like full file encryption with

```
ansible-vault edit
```

but which does not operate on the whole file but on individual variables.
i.e.

```
tangsible vault <filename>
```

* takes a file with individually encrypted variables.
* decrypts all of them
* puts it all back into a file that looks like a plain ansible file with variable definitions
* opens the editor
* and after the editor has been closed, encrypts the individual variables
  again and put them together in a yaml file with individually encrypted
  variables


### Things to take care of

1. The salt problem — this is the one that'll surprise you first.

Vault's AES256 encryption uses a random salt every time, so encrypting the
same plaintext twice produces two different ciphertexts. If tangsible naively
does "decrypt everything → edit → re-encrypt everything," then every value's
ciphertext changes on every save, even values the user didn't touch. We need
to diff the decrypted-before vs. edited-after content per-key and only
re-encrypt values that actually changed, leaving unchanged keys' original
ciphertext untouched.

Since this only needs to handle top-level keys (see point 4 below) and the
main use case is adding or changing one or two secrets in an otherwise
stable file, the diff itself can be a plain key-set comparison rather than a
structural merge:

- key present before and after, decrypted value unchanged → leave the
  original ciphertext byte-for-byte untouched
- key present before and after, decrypted value changed → re-encrypt
- key is new → encrypt
- key removed → drop it, nothing to encrypt

If the editor closes with the decrypted content completely unchanged, skip
writing the target file entirely — same file, same mtime, no diff. Never
rewrite just to normalize formatting.

2. Structure preservation.

We must take care that as much structure / comments etc. are kept intact so
that both the user will recognize its original file and diffs are kept
minimal.

`gopkg.in/yaml.v3`'s node-based API (already a dependency, used by
`source.go` for task-source lookup) decodes the file into a `yaml.Node`
tree rather than a struct, which is how the *editable* view gets built:
decode → swap each vaulted value's tag/text for its decrypted plaintext →
encode. That round-trip preserves comments, key order, and anchors well
enough for a throwaway temp file whose own formatting is never diffed
against anything.

The *final* file written back out is a different matter, and does **not**
go through that same encode step — see "Implementation" below for why:
re-encoding the whole document turned out to reformat every untouched
vaulted block on every save, not just the ones actually touched. Instead,
reassembly splices raw source-text spans (see `keyContentSpans`):
an unedited entry's exact original bytes, indentation included, are
reused verbatim rather than regenerated. The one accepted `yaml.v3`
round-trip gap that remains is in the *editable view* specifically: blank
lines between entries there aren't reliably preserved. That gap doesn't
propagate to the saved file, since untouched entries there come from raw
source text, not from the view's own re-encoding.

3. What to encrypt, what to leave alone

Values that have not been encrypted before opening the file should be left
unencrypted when writing the file again — but tangsible should emit a
warning when it does this (e.g. `warning: 'region' is not vault-encrypted,
left as plaintext`), so an accidentally-unvaulted secret doesn't silently
land on disk (and in git) unnoticed. This is deliberately a warning, not an
error — a mix of plain and vaulted top-level values is expected to happen.
The warning fires only when the unvaulted value is itself a string — a
plaintext list/number/bool key is unremarkable and warning on it would just
be noise on every ordinary vars file; the rationale ("an accidentally-
unvaulted secret") only makes sense for something that could plausibly have
been one.

Variables that haven't been there when opening the file should be encrypted
when saving.

Encryption should only be applied to strings as other types cannot be
sensibly encrypted. If a key that *was* vault-encrypted before editing now
resolves to something other than a string (a number, list, dict, null),
that's a hard failure, not a silent fallback to plaintext — the risk of a
secret ending up unencrypted on disk outweighs the inconvenience. See point
5 below for how that failure is surfaced.

4. Scope: top-level keys only

`!vault` is technically a YAML scalar tag and can legally appear nested
inside a dict or list, not just as a top-level `key: value` entry — but
every vault file anyone here has actually built or seen uses top-level keys
only, which also matches how `ansible-vault encrypt_string`'s own output is
shaped (ready to paste in as a top-level entry) and Ansible's general
preference for flat, prefixed variable names over nested structures. So v1
only supports top-level keys. If tangsible finds a `!vault` value anywhere
else, it fails loudly and explains why, rather than attempting to handle a
shape that's never actually been exercised.

5. Invalid edits: reopen-with-annotation, never lose edits, never write
   something wrong

Three situations must never result in tangsible silently writing the target
file:

- a key that was vault-encrypted before editing now resolves to a
  non-string (see point 3)
- a `!vault` tag was typed by hand into the editable view — the view only
  ever contains plain values, so this can only mean the user pasted or
  typed one in (e.g. copying an already-encrypted value from another file);
  v1 doesn't support that, and rejects it the same way as any other
  unexpected `!vault` tag rather than trying to guess what was intended
- the edited file doesn't parse as valid YAML at all

Following the same pattern `visudo`/`crontab -e` use: annotate the problem
inline — a comment placed directly above the offending line, using the line
number from the parsed source — and reopen the editor on that same content,
so the user's other edits are never lost and they fix the problem in place
rather than starting over. If a save attempt has multiple problems at once,
all of them get annotated in the same pass rather than being caught one at a
time — fixing the first one shouldn't just surface the next one on the next
save.

For a file that doesn't parse as YAML at all, there's no successful parse to
pull a line number from, so this case falls back to showing the parse error
and reopening the raw edited text as-is, rather than an inline comment.

**The way out of this loop is an explicit prompt, not Ctrl-C.** The original
design called for Ctrl-C, matching this project's existing convention that
Ctrl-C behaves like running the underlying thing directly, no ambiguity —
but that turned out not to work, and not just as a matter of taste. Real
full-screen editors put the terminal into raw mode, which — confirmed with a
minimal reproduction outside this codebase entirely, not just inferred —
disables the terminal's own `ISIG` flag, the thing that makes a Ctrl-C
keypress become an actual `SIGINT` signal at all. Since that flag is a
property of the shared terminal, not of any one process, disabling it blocks
signal delivery to *every* process attached to that terminal, tangsible
included — not "vim catches and swallows the signal," but "the signal is
never generated by the kernel in the first place." Vim's own "Type `:qa`..."
hint on Ctrl-C is it handling the literal byte as ordinary input, not a
signal handler reacting to anything. This is the same underlying mechanism
`CLAUDE.md` already documents for tangsible's own TUI (`tcell`'s raw mode
disabling Ctrl-C-to-SIGINT for its own screen) — vim does the identical
thing, for the identical reason, and there was no way around it from
tangsible's side: nothing it does with its own signal handling can make the
kernel generate a signal that vim's terminal settings have turned off.

So instead: once the editor exits and hands the terminal back to tangsible
(which never puts it in raw mode itself, for this verb), tangsible prints
what's wrong and asks directly — *fix this in the editor again, or revert
and discard every change made so far?* This works precisely because it
happens in tangsible's own, ordinary cooked-mode terminal, not while an
editor owns it, so it doesn't depend on any signal being delivered at all.
Choosing to fix reopens the editor with the same inline annotation as
before; choosing to revert leaves the target file exactly as it was (never
written to in the first place) and exits cleanly. Unlike `visudo`, there's
still no "quit and save anyway" option, since that would mean writing a
state this doc already decided must never reach disk. Ctrl-C is kept as a
secondary safety net for the moments tangsible itself is blocking (the
password prompt, this very question, file reads) — genuinely useful there,
since an *unhandled* Ctrl-C would otherwise bypass Go's own deferred cleanup
and leave the scratch file behind — but it is no longer the documented way
to escape a detected problem.

A follow-on bug caught immediately after this shipped: the annotation
comment itself needs to be stripped from the editor's output before it's
looked at again, on *every* round - otherwise a still-unfixed problem stacks
a duplicate comment on top of itself each time it's saved again, and a
*fixed* problem's now-irrelevant comment lingers in the file forever, since
nothing else would ever remove it. Both are real, live-caught bugs, not
hypothetical - fixed by `vaultfile.StripAnnotations`, called at the top of
every reopen-loop round before anything else inspects the edited content.

6. Temp-file safety

The decrypted plaintext has to live in a temp file while the editor has it
open. Same discipline `ansible-vault edit` itself uses: restrictive
permissions (0600), cleaned up even if the editor crashes or tangsible
itself is interrupted mid-edit. Nothing beyond that is required.

7. Crypto: native Go implementation, not shelling out

Vault's cipher (AES256-CTR + HMAC) is a small, stable, documented format —
unlike Ansible's playbook execution engine, which Purpose.md deliberately
avoids reimplementing, this is tractable to implement directly in Go. Doing
so avoids spawning a subprocess per secret (awkward at scale, and awkward to
pipe a password into repeatedly) and reduces the surface area where
something could go wrong and leak a credential. The implementation should be
validated against real `ansible-vault encrypt_string`/`decrypt` output on
golden fixtures before being trusted on real secrets.

8. Password source

Mirrors `ansible-vault`'s own flags rather than inventing new ones, since
the whole feature's framing is "feels like `ansible-vault edit`":
`--vault-password-file <path>` and `--ask-vault-pass` (a no-echo terminal
prompt), rejected together the same way `ansible-vault` itself treats them
as mutually exclusive. Resolution precedence, most-explicit-wins:

1. `--vault-password-file`
2. `--ask-vault-pass`
3. `$ANSIBLE_VAULT_PASSWORD_FILE`
4. the project's own `ansible.cfg` (`[defaults] vault_password_file`) —
   the standard, most common place ansible users already configure this;
   found the same way `ansible` itself does (`$ANSIBLE_CONFIG`, then
   `./ansible.cfg`, then `~/.ansible.cfg`, then
   `/etc/ansible/ansible.cfg`). Ranked above tangsible's own config
   specifically because it's the user's real, pre-existing ansible
   configuration — tangsible's own setting exists only as a fallback for
   projects that don't already have one.
5. `.tangsible/config.toml`'s `[vault] password_file`
6. an interactive prompt, as a last resort

Step 6 is a deliberate divergence from real `ansible-vault`, which errors
("no vault secrets found") instead of prompting when nothing is configured.
Accepted here because this verb is inherently interactive by nature — there
is no batch/CI use case for "edit one variable" the way there is for
`ansible-playbook` itself.

Real-use gap caught after v1 first shipped: step 4 was missing entirely at
first — only `$ANSIBLE_VAULT_PASSWORD_FILE` and tangsible's own config were
checked, so a project configured the completely standard way (an
`ansible.cfg` with `vault_password_file` set, no env var) still got
prompted every time. Added as its own precedence step rather than folded
into the env-var check, since it needed its own discovery logic
(`internal/vault/ansiblecfg.go`'s `locateAnsibleConfig`/`iniValue` — a
small, deliberately non-general INI reader, not a full ansible.cfg parser).

## Deferred to v2

- **Multiple vault-ids.** Ansible supports per-label vault-ids
  (`$ANSIBLE_VAULT;1.2;AES256;label`), letting a single file mix ciphertexts
  encrypted under different passwords. v1 assumes a single default vault
  password (no `--vault-id` label support), and fails loudly if it
  encounters a labeled-vault-id file it can't handle rather than silently
  mishandling it. Revisit once there's real usage experience with multiple
  vault-ids to design against.

## Implementation

Built as three packages plus the verb wiring, in dependency order:

- `internal/vaultcrypto` — the AES256/format-1.1 cipher core (`Encrypt`/
  `Decrypt`), implemented natively against Go's standard library alone
  (`crypto/pbkdf2`, added in Go 1.24, meant no new third-party dependency
  was needed for this). Validated both directions against the real
  `ansible-vault` binary: decrypting its output, and having it decrypt
  tangsible's own output — both run as regular (skip-if-`ansible-vault`-
  missing) tests, not just a one-off manual check.
- `internal/vaultfile` — `BuildDecryptedView` (source → plaintext editable
  view, via a `yaml.Node` decode/mutate/encode, same idiom
  `internal/source/source.go` already uses for reading) and `Reassemble`
  (the per-key diff, then reconstructing the final file by *splicing raw
  source-text spans* - `keyContentSpans` - rather than re-encoding a
  mutated `yaml.Node` tree), plus `AnnotateProblems` for the
  reopen-with-annotation loop.
- `internal/vault` — the verb itself (`RunVaultVerb`), argument parsing,
  password resolution, and the editor loop (temp file, the fix-or-revert
  prompt, the reopen cycle).
- Wired in as `VerbVault` alongside the other standalone verbs
  (`internal/config/resolve.go`, `internal/session/main.go`) — like
  `template`/`host`/`hosts`/`revisit`, it bypasses the run/rerun/role tree
  machinery entirely; unlike all of them, it never touches `tview` at all.

A real bug caught in live use, not by the test suite (worth recording in
detail since it directly motivated the current design): the first shipped
version of `Reassemble` rebuilt the *entire* output file by mutating the
parsed `yaml.Node` tree and re-encoding it as a whole, splicing in only
the *original node* (`Tag`/`Value`/`Style`) for an unedited vaulted key.
That kept the ciphertext *value* correct — an untouched secret still
decrypted to the same plaintext — but `yaml.v3`'s re-emission doesn't
preserve a node's original formatting. Concretely: its encoder silently
resets any `SetIndent` value outside `[2, 9]` back to 2, with no error
(confirmed against its source, `apic.go`'s `yaml_emitter_set_indent`),
and real `ansible-vault`'s own `encrypt_string` output indents a vault
block by 10 spaces, one past the top of that range. The result: adding or
changing *one* key reformatted every other vaulted block's indentation
too (10 spaces → 2), which made a plain `diff`/`git diff` show every
single secret in the file as changed, even though not one byte of actual
ciphertext had. From the outside this looked exactly like the salt
problem point 1 exists to prevent, and was reported as such.

The fix is structural, not a bigger clamp: `Reassemble` no longer
re-encodes the document at all. It reconstructs the file by splicing raw
*source text* spans instead (`keyContentSpans`, `formatVaultBlock`) - an
unedited vaulted key's exact original bytes (indentation included) are
reused verbatim; an unedited plaintext key's bytes come from the edited
file's own text; only a genuinely re-encrypted or brand-new value gets
freshly hand-formatted text, at the *source file's own* detected indent
width (`detectIndentWidth`, no longer clamped to `[2, 9]` at all, since
hand-formatted text isn't bound by `SetIndent`'s limitation the way
`yaml.v3`'s own encoder is). Net effect: a freshly re-encrypted or
new block now matches real `ansible-vault`'s own 10-space convention
exactly, and an untouched key's diff is now empty, not just
"cryptographically equivalent." Regression-tested end-to-end
(`TestReassemble_UntouchedKeyIsByteIdenticalToSource`,
`TestReassemble_ReencryptedBlockUsesSourceIndentWidth`) specifically
because a unit test on `detectIndentWidth` alone, in isolation, didn't
and couldn't have caught the original bug - only encoding real output and
comparing raw bytes could.

A second real bug, caught immediately after the fix above shipped:
`keyContentSpans` split each key's raw span into "content" (kept only for
a key whose own decision uses the edited file's text) and "gap" (always
kept, regardless of decision) by trimming *blank* trailing lines only. A
`# comment` line typed directly above a brand-new key isn't blank, so it
stayed classified as the *previous* key's trailing content - and if that
previous key was itself unedited (spliced from the original file, which
obviously never had a comment about a key that didn't exist yet), its
edited-side content - comment included - was simply never used, and the
comment vanished with no error or warning. Fixed by also trimming
top-level (column-0) `#` lines into the gap, not just blank ones -
deliberately column-0 only, since an *indented* line starting with `#`
could legitimately be part of a multi-line secret's own literal content,
not a comment at all. `TestReassemble_CommentBeforeNewKeySurvives`
regression-tests the exact reported scenario.
