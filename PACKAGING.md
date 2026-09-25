# Packaging tangsible for a distro

## File layout

Each release (`tangsible_<version>_<os>_<arch>.tar.gz`) contains:

| Source                          | Destination                                    |
|----------------------------------|------------------------------------------------|
| `tangsible`                      | `/usr/bin/tangsible`                            |
| `callback/tangsible_jsonl.py`    | `/usr/share/tangsible/tangsible_jsonl.py`       |
| `callback/LICENSE`               | `/usr/share/tangsible/LICENSE`                  |
| `man/*.1`                        | `/usr/share/man/man1/`                          |
| `README.md`, `LICENSE`           | `/usr/share/doc/tangsible/`                     |
| `completions/tangsible.bash`     | `/usr/share/bash-completion/completions/tangsible` |
| `completions/tangsible.fish`     | `/usr/share/fish/vendor_completions.d/tangsible.fish` |
| - | `/etc/tangsible/config.toml` |

/etc/tangsible/config.toml is not required, the tangsible package does not
ship with a default config file.

Building from source instead of the tarball: `go build .` for the binary;
`man/*.1` and `callback/tangsible_jsonl.py` are already-generated,
checked-in files, no extra build step for either.

## Different prefix

The tangsible binary will look for its callback plugin starting from its own
location at ../share/tangsible/tangsible_jsonl.py. Use `tangsible version` to
confirm whether `tangsible` was able to find its plugin.

## Licensing

The binary/CLI is **Apache-2.0** licensed. The bundled
`callback/tangsible_jsonl.py` is a separate **GPL-3.0-or-later**
derivative of `ansible.posix.jsonl` - reflect both in your package's license
metadata (e.g. Debian's `debian/copyright`), not a single license for the
whole package.

## Runtime dependencies

No bundled/vendored Ansible - tangsible always shells out to a locally
installed **ansible-core**, specifically `ansible-playbook`,
`ansible-doc`, `ansible-inventory`, and `ansible-config` (all part of the
`ansible-core` distribution).
