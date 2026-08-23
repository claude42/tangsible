# Shell completion

Completion scripts for `tangsible`'s own verbs (`run`, `rerun`, `role`,
`template`, `host`, `hosts`) and playbook/role positionals, plus every
`ansible-playbook` flag Tangsible passes through, live in `completions/`.

**Bash** (needs the `bash-completion` package for the automatic directory):

```
cp completions/tangsible.bash /usr/share/bash-completion/completions/tangsible
```

or source it directly from `~/.bashrc`:

```
source /path/to/tangsible/completions/tangsible.bash
```

**Fish:**

```
cp completions/tangsible.fish ~/.config/fish/completions/tangsible.fish
```

No separate zsh script is provided; zsh's `bashcompinit` can load the bash
script above (`autoload -U bashcompinit && bashcompinit` in `.zshrc`, then
source `tangsible.bash`).

