claude --resume c3c096b1-8043-43af-b768-865d682b6019

  # interactive
  curl -fsSL https://code.aw.net/claude/tangsible/raw/branch/main/install.sh | sh

  # unattended
  curl -fsSL https://code.aw.net/claude/tangsible/raw/branch/main/install.sh | sh -s -- --yes

  # specific version / from the GitHub mirror / uninstall
  … | sh -s -- --version v0.1.0
  TANGSIBLE_REPO_BASE=https://github.com/claude42/tangsible \
  TANGSIBLE_API_BASE=https://api.github.com/repos/claude42/tangsible … | sh
  … | sh -s -- --uninstall

  Also honors --prefix, --no-completions, XDG_DATA_HOME, XDG_BIN_HOME.
