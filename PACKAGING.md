# Packaging tangsible for a distro

## File layout

Each release (`tangsible_<version>_<os>_<arch>.tar.gz`) contains:

| Source                          | Destination                                    |
|----------------------------------|------------------------------------------------|
| `tangsible`                      | `/usr/bin/tangsible`                            |
| `man/*.1`                        | `/usr/share/man/man1/`                          |
| `README.md`, `LICENSE`           | `/usr/share/doc/tangsible/`                     |
| `completions/tangsible.bash`     | `/usr/share/bash-completion/completions/tangsible` |
| `completions/tangsible.fish`     | `/usr/share/fish/vendor_completions.d/tangsible.fish` |
| - | `/etc/tangsible/config.toml` |

/etc/tangsible/config.toml is not required, the tangsible package does not
ship with a default config file.

Building from source instead of the tarball: `go build .` for the binary;
`man/*.1` are already-generated, checked-in files, no extra build step.

## The callback plugin

Nothing to install: `callback/tangsible_events.py` is compiled into the
binary. On first use tangsible extracts it to
`$XDG_CACHE_HOME/tangsible/plugin-<hash>/` (default `~/.cache/tangsible/...`),
or to a private temp directory if that isn't writable. Plugin and binary
speak a private protocol, so a loose copy is deliberately never searched
for. `$TANGSIBLE_CALLBACK_DIR` (a directory holding `tangsible_events.py`)
overrides this, e.g. from a wrapper script. `tangsible version` shows which
is in effect. A different `--prefix` changes nothing here.

## Licensing

The binary/CLI and the bundled `callback/tangsible_events.py` are both
**Apache-2.0** licensed.

## Runtime dependencies

No bundled/vendored Ansible - tangsible always shells out to a locally
installed **ansible-core**, specifically `ansible-playbook`,
`ansible-doc`, `ansible-inventory`, and `ansible-config` (all part of the
`ansible-core` distribution).
