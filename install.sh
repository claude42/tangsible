#!/bin/sh
# tangsible installer — per-user, no root, one step at a time.
#
# Two modes, picked automatically:
#
#   * download — resolve the latest release, fetch it, verify its SHA-256,
#     install. This is what you get piping it from the web:
#
#       curl -fsSL https://code.aw.net/claude/tangsible/raw/branch/main/install.sh | sh
#       curl -fsSL https://raw.githubusercontent.com/claude42/tangsible/main/install.sh | sh
#
#   * local — this script ships inside every release archive. Run it from
#     the unpacked archive and it installs the files sitting next to it,
#     with no download and no version lookup:
#
#       tar xzf tangsible_*.tar.gz && ./tangsible_*/install.sh
#
# Either way it prompts before every step (reading /dev/tty, so the pipe
# form still asks), uses no sudo, and writes only inside your home
# directory. Force a mode with --local / --download.
#
# (A cryptographic signature on the checksums file would be the next trust
# step up for the download mode; not there yet.)

set -eu

# ----------------------------------------------------------------------
# defaults (all overridable by flags or environment)
# ----------------------------------------------------------------------
OWNER_REPO="claude/tangsible"
REPO_BASE="${TANGSIBLE_REPO_BASE:-https://code.aw.net/${OWNER_REPO}}"
API_BASE="${TANGSIBLE_API_BASE:-https://code.aw.net/api/v1/repos/${OWNER_REPO}}"

VERSION="${TANGSIBLE_VERSION:-}"          # empty => resolve latest (download mode)
MODE="${TANGSIBLE_MODE:-auto}"            # auto | local | download
ASSUME_YES="${TANGSIBLE_ASSUME_YES:-0}"
WANT_COMPLETIONS=1
DO_UNINSTALL=0
PREFIX_SET=0
PREFIX="${TANGSIBLE_PREFIX:-$HOME/.local}"

usage() {
	cat <<'EOF'
tangsible installer — per-user install, no root.

Usage:
  install.sh [options]                       # from an unpacked release archive
  curl -fsSL <url>/install.sh | sh           # bootstrap: download + install
  curl -fsSL <url>/install.sh | sh -s -- --yes

Options:
  -y, --yes             accept every step without prompting
  --local               install the files next to this script (no download)
  --download            ignore local files; fetch a release
  --version <vX.Y.Z>    download mode: install a specific release (default: latest)
  --no-completions      skip the shell completion files
  --prefix <dir>        install under <dir>/bin and <dir>/share
                        (default: ~/.local, honouring $XDG_DATA_HOME)
  --uninstall           remove a previous per-user install
  -h, --help            show this help

Environment:
  TANGSIBLE_VERSION   same as --version
  TANGSIBLE_PREFIX    same as --prefix
  TANGSIBLE_MODE      auto | local | download
  XDG_DATA_HOME       man pages, docs and completions live under here
  XDG_BIN_HOME        the binary lives here (default ~/.local/bin)
  TANGSIBLE_REPO_BASE, TANGSIBLE_API_BASE
                     point download mode elsewhere (e.g. the GitHub mirror)
EOF
}

# ----------------------------------------------------------------------
# arg parsing
# ----------------------------------------------------------------------
while [ $# -gt 0 ]; do
	case "$1" in
	-y | --yes) ASSUME_YES=1 ;;
	--local) MODE=local ;;
	--download) MODE=download ;;
	--version) VERSION="${2:?--version needs a value}"; shift ;;
	--version=*) VERSION="${1#*=}" ;;
	--no-completions) WANT_COMPLETIONS=0 ;;
	--prefix) PREFIX="${2:?--prefix needs a value}"; PREFIX_SET=1; shift ;;
	--prefix=*) PREFIX="${1#*=}"; PREFIX_SET=1 ;;
	--uninstall) DO_UNINSTALL=1 ;;
	-h | --help) usage; exit 0 ;;
	*) printf 'error: unknown option: %s\n\n' "$1" >&2; usage >&2; exit 2 ;;
	esac
	shift
done

case "$ASSUME_YES" in
"" | 0 | no | NO | false | FALSE) ASSUME_YES=0 ;;
*) ASSUME_YES=1 ;;
esac

# Can we actually read from the terminal? `[ -r /dev/tty ]` lies when there
# is no controlling terminal (open() fails with ENXIO), so probe for real.
# The probe runs in a subshell: a failed redirect on ':' would otherwise
# kill a POSIX shell outright, if-condition or not.
HAVE_TTY=0
if ( : </dev/tty ) 2>/dev/null; then HAVE_TTY=1; fi

# ----------------------------------------------------------------------
# derived paths
# ----------------------------------------------------------------------
if [ "$PREFIX_SET" = 1 ]; then
	BIN_DIR="$PREFIX/bin"
	DATA_DIR="$PREFIX/share"
else
	BIN_DIR="${XDG_BIN_HOME:-$HOME/.local/bin}"
	DATA_DIR="${XDG_DATA_HOME:-$HOME/.local/share}"
fi
MAN_DIR="$DATA_DIR/man/man1"
DOC_DIR="$DATA_DIR/doc/tangsible"
# bash-completion and fish both read third-party completions from
# <data dir>/... via $XDG_DATA_DIRS, so these follow --prefix / $XDG_DATA_HOME.
BASH_COMP_DIR="$DATA_DIR/bash-completion/completions"
FISH_COMP_DIR="$DATA_DIR/fish/vendor_completions.d"

# ----------------------------------------------------------------------
# helpers
# ----------------------------------------------------------------------
say() { printf '%s\n' "$*"; }
err() { printf 'error: %s\n' "$*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || err "required command not found: $1"; }

confirm() {
	# $1 = question. Returns 0 for yes, 1 for no.
	if [ "$ASSUME_YES" = 1 ]; then
		say "==> $1  [--yes]"
		return 0
	fi
	if [ "$HAVE_TTY" != 1 ]; then
		err "no terminal to read a confirmation from; re-run with --yes"
	fi
	printf '==> %s [y/N] ' "$1"
	read -r _ans </dev/tty || _ans=n
	case "$_ans" in
	y | Y | yes | YES | Yes) return 0 ;;
	*) say "    skipped."; return 1 ;;
	esac
}

fetch() {
	# $1 = url, $2 = destination file
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL --proto '=https' --tlsv1.2 -o "$2" "$1"
	else
		wget -qO "$2" "$1"
	fi
}

fetch_stdout() {
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL --proto '=https' --tlsv1.2 "$1"
	else
		wget -qO- "$1"
	fi
}

# ----------------------------------------------------------------------
# pick the mode
# ----------------------------------------------------------------------
SELF_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" 2>/dev/null && pwd || echo ".")
looks_local() {
	[ -f "$SELF_DIR/tangsible" ] && [ -d "$SELF_DIR/man" ] && [ -f "$SELF_DIR/LICENSE" ]
}
if [ "$MODE" = auto ]; then
	if looks_local; then MODE=local; else MODE=download; fi
fi
if [ "$MODE" = local ] && ! looks_local; then
	err "--local given but no release files found next to this script ($SELF_DIR)"
fi

# ----------------------------------------------------------------------
# uninstall (mode-independent)
# ----------------------------------------------------------------------
if [ "$DO_UNINSTALL" = 1 ]; then
	say "Removing a per-user tangsible install."
	say ""
	confirm "Remove $BIN_DIR/tangsible?" && rm -f "$BIN_DIR/tangsible"
	confirm "Remove man pages $MAN_DIR/tangsible*.1?" && rm -f "$MAN_DIR"/tangsible*.1
	confirm "Remove $DOC_DIR/ ?" && rm -rf "$DOC_DIR"
	confirm "Remove shell completions?" && {
		rm -f "$BASH_COMP_DIR/tangsible" "$FISH_COMP_DIR/tangsible.fish"
	}
	say ""
	say "Done."
	exit 0
fi

# ----------------------------------------------------------------------
# root check
# ----------------------------------------------------------------------
if [ "$(id -u)" = 0 ]; then
	say ""
	say "WARNING: running as root. This script only ever does a per-user"
	say "install — as root that means into /root ($HOME). It is not a"
	say "system-wide installer. You probably want to run it as your user."
	say ""
	confirm "Continue anyway?" || exit 1
fi

# ----------------------------------------------------------------------
# obtain the files -> $SRC, and a display version -> $VER
# ----------------------------------------------------------------------
if [ "$MODE" = local ]; then
	SRC="$SELF_DIR"
	VER=$(basename "$SELF_DIR" | sed -n 's/^tangsible_\([^_]*\)_.*/\1/p')
	if [ -z "$VER" ] && [ -x "$SRC/tangsible" ]; then
		VER=$("$SRC/tangsible" version 2>/dev/null | head -n1 | cut -d' ' -f2 || true)
	fi
	[ -n "$VER" ] || VER="local files"
	say ""
	say "Installing tangsible $VER from $SRC"
else
	command -v curl >/dev/null 2>&1 || command -v wget >/dev/null 2>&1 ||
		err "need either curl or wget"
	need tar
	need mktemp
	need uname

	if [ -z "$VERSION" ]; then
		say "Resolving the latest release..."
		VERSION=$(fetch_stdout "$API_BASE/releases/latest" |
			sed -n 's/.*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1)
		[ -n "$VERSION" ] ||
			err "could not read the latest version from $API_BASE/releases/latest"
	fi
	case "$VERSION" in v*) TAG="$VERSION" ;; *) TAG="v$VERSION" ;; esac
	VER="${TAG#v}"

	OS=$(uname -s)
	ARCH=$(uname -m)
	case "$OS" in
	Linux) OS=linux ;;
	Darwin) OS=darwin ;;
	*) err "unsupported operating system: $OS" ;;
	esac
	case "$ARCH" in
	x86_64 | amd64) ARCH=amd64 ;;
	aarch64 | arm64) ARCH=arm64 ;;
	*) err "unsupported architecture: $ARCH" ;;
	esac

	ARCHIVE="tangsible_${VER}_${OS}_${ARCH}.tar.gz"
	SUMS="tangsible_${VER}_checksums.txt"
	DL_BASE="$REPO_BASE/releases/download/$TAG"

	TMP=$(mktemp -d "${TMPDIR:-/tmp}/tangsible-install.XXXXXX")
	trap 'rm -rf "$TMP"' EXIT INT TERM

	say ""
	say "Downloading tangsible $VER ($OS/$ARCH)..."
	fetch "$DL_BASE/$ARCHIVE" "$TMP/$ARCHIVE"
	fetch "$DL_BASE/$SUMS" "$TMP/$SUMS"

	say "Verifying SHA-256..."
	grep " ${ARCHIVE}\$" "$TMP/$SUMS" >"$TMP/$SUMS.one" ||
		err "no checksum entry for $ARCHIVE in $SUMS"
	if command -v sha256sum >/dev/null 2>&1; then
		(cd "$TMP" && sha256sum -c "$SUMS.one" >/dev/null) ||
			err "SHA-256 mismatch — refusing to install"
	elif command -v shasum >/dev/null 2>&1; then
		(cd "$TMP" && shasum -a 256 -c "$SUMS.one" >/dev/null) ||
			err "SHA-256 mismatch — refusing to install"
	else
		say "warning: no sha256sum/shasum found — skipping verification"
	fi

	(cd "$TMP" && tar -xzf "$ARCHIVE")
	SRC="$TMP/tangsible_${VER}_${OS}_${ARCH}"
fi

[ -f "$SRC/tangsible" ] || err "no ./tangsible in $SRC"

# ----------------------------------------------------------------------
# plan
# ----------------------------------------------------------------------
CURRENT=""
if command -v tangsible >/dev/null 2>&1; then
	CURRENT=$(tangsible version 2>/dev/null | head -n1 || true)
fi

say ""
say "Ready to install tangsible $VER. Each step asks first; say n to skip it."
say ""
if [ -n "$CURRENT" ]; then
	say "  (replacing: $CURRENT at $(command -v tangsible))"
fi
say "  1. binary            -> $BIN_DIR/tangsible"
say "  2. man pages         -> $MAN_DIR/"
say "  3. README + LICENSE  -> $DOC_DIR/"
if [ "$WANT_COMPLETIONS" = 1 ]; then
	say "  4. bash completion   -> $BASH_COMP_DIR/tangsible"
	say "  5. fish completion   -> $FISH_COMP_DIR/tangsible.fish"
fi
say ""
say "  No sudo. Nothing outside your home directory."
say ""

did_bin=0

# 1. binary
if confirm "Install the tangsible binary to $BIN_DIR/tangsible ?"; then
	mkdir -p "$BIN_DIR"
	cp "$SRC/tangsible" "$BIN_DIR/tangsible.tmp.$$"
	chmod 0755 "$BIN_DIR/tangsible.tmp.$$"
	mv "$BIN_DIR/tangsible.tmp.$$" "$BIN_DIR/tangsible"
	say "    installed $BIN_DIR/tangsible"
	did_bin=1
fi

# 2. man pages
if confirm "Install the man pages to $MAN_DIR/ ?"; then
	mkdir -p "$MAN_DIR"
	n=0
	for f in "$SRC"/man/*.1; do
		[ -e "$f" ] || continue
		cp "$f" "$MAN_DIR/"
		n=$((n + 1))
	done
	say "    installed $n man page(s)"
fi

# 3. docs
if confirm "Install README and LICENSE to $DOC_DIR/ ?"; then
	mkdir -p "$DOC_DIR"
	for f in README.md LICENSE; do
		if [ -f "$SRC/$f" ]; then cp "$SRC/$f" "$DOC_DIR/"; fi
	done
	say "    installed $DOC_DIR/"
fi

# 4/5. completions
if [ "$WANT_COMPLETIONS" = 1 ]; then
	if [ -f "$SRC/completions/tangsible.bash" ] &&
		confirm "Install bash completion to $BASH_COMP_DIR/tangsible ?"; then
		mkdir -p "$BASH_COMP_DIR"
		cp "$SRC/completions/tangsible.bash" "$BASH_COMP_DIR/tangsible"
		say "    installed"
	fi
	if [ -f "$SRC/completions/tangsible.fish" ] &&
		confirm "Install fish completion to $FISH_COMP_DIR/tangsible.fish ?"; then
		mkdir -p "$FISH_COMP_DIR"
		cp "$SRC/completions/tangsible.fish" "$FISH_COMP_DIR/tangsible.fish"
		say "    installed"
	fi
fi

# ----------------------------------------------------------------------
# post-install notes
# ----------------------------------------------------------------------
say ""
if [ "$did_bin" = 1 ]; then
	case ":$PATH:" in
	*":$BIN_DIR:"*) ;;
	*)
		say "note: $BIN_DIR is not on your PATH. Add to your shell startup file:"
		say ""
		say "    export PATH=\"$BIN_DIR:\$PATH\""
		say ""
		;;
	esac
	if command -v manpath >/dev/null 2>&1; then
		case ":$(manpath 2>/dev/null):" in
		*":$DATA_DIR/man:"*) ;;
		*) say "note: $DATA_DIR/man may not be on your MANPATH" \
			"(man-db usually adds it once $BIN_DIR is on PATH)." ;;
		esac
	fi
	say "Installed. Try:  tangsible version"
else
	say "Nothing installed."
fi
