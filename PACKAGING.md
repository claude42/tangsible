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

## Official `.deb` and `.rpm`

Each release also carries `tangsible_<version>_<arch>.deb` and
`tangsible-<version>-1.<arch>.rpm` (amd64/x86_64, arm64/aarch64), built by
GoReleaser's `nfpms` block in `.goreleaser.yaml` with the layout above (plus
a gzipped changelog and `examples/` under `/usr/share/doc/tangsible/`). Both
depend on `ansible-core` (Debian 12+/Ubuntu 22.04+, Fedora, RHEL 8+).

Per format: the deb gets a Debian-format `copyright`
(`packaging/deb/copyright`) and `changelog.gz`; the rpm gets `LICENSE` under
`/usr/share/licenses/tangsible/` and `Changelog.md.gz` as a doc. The man
pages and changelog are gzipped by a before-hook into `pkg-gz/` (gitignored).

Try it locally with `goreleaser release --snapshot --clean`, then
`dpkg -I`/`dpkg -c`/`lintian` or `rpm -qpi`/`rpm -qpl`/`rpmlint` on the files
in `dist/`. Known, accepted findings: a statically linked binary (Go), no
Debian-format changelog, rpm without signature, `%changelog`, or `%doc`
marks (nFPM can't mark globbed files as docs), and rpmlint's spelling check
on "ansible". Not done yet: signing, and a hosted apt/dnf repository.

## apt repository

The release workflow uploads each `.deb` to Forgejo's Debian package registry
(`uploads:` in `.goreleaser.yaml`) as distribution `stable`, component
`main`, under the `claude` owner. Forgejo generates and signs the repository
metadata itself, with a key it creates for the owner; the user-facing setup
is in `README.md` ("On Debian/Ubuntu").

Needs the Forgejo Actions secret `FORGEJO_PACKAGES_TOKEN`: an access token of
the `claude` account with only `package: Read and Write`. It is separate from
`GITEA_TOKEN` (releases). Uploading a version that already exists is
rejected, so re-running a finished release job fails at the upload step;
delete the version first (`DELETE /api/packages/claude/debian/pool/stable/
main/tangsible/<version>/<arch>`). The `.rpm` isn't uploaded yet.

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
