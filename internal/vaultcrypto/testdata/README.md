# Golden fixtures

These files prove `vaultcrypto` interoperates with real `ansible-vault`
output, not just with itself.

- `golden_password.txt` — the fixture password, no trailing newline.
- `golden_1.vault` — the header+body block produced by a real
  `ansible-vault encrypt_string` invocation, dedented to column 0 (the
  indentation `encrypt_string`/YAML's `!vault |` block scalar adds is
  cosmetic and irrelevant to decryption).
- `golden_1.plaintext.txt` — the known plaintext, no trailing newline.

Regenerated with:

```sh
ansible-vault encrypt_string 'this is the golden fixture plaintext value' \
    --vault-password-file golden_password.txt --name golden_secret \
    | tail -n +2 | sed 's/^[[:space:]]*//' > golden_1.vault
```

Verified against the real binary at generation time via
`ansible-vault decrypt golden_1.vault --vault-password-file golden_password.txt --output -`.
