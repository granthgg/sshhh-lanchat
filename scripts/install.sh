#!/usr/bin/env sh
# One-command installer for `lanchat` (macOS / Linux).
#
#   ./scripts/install.sh
#
# Builds the single binary and installs it so you can run `lanchat` from ANY
# directory, with no manual setup:
#
#   1. If a standard bin dir is already on your PATH and writable (e.g.
#      /opt/homebrew/bin, /usr/local/bin), it installs there — works instantly.
#   2. Otherwise it installs to ~/.local/bin and adds that to your PATH by
#      editing your shell startup file for you.
#
# No root/sudo required.

set -eu

BINARY="lanchat"
PKG="./cmd/lanchat"
# Repo root is the parent of the directory holding this script.
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"

say() { printf '%s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }
on_path() { case ":${PATH}:" in *":$1:"*) return 0 ;; *) return 1 ;; esac; }

# --- 1. build --------------------------------------------------------------
command -v go >/dev/null 2>&1 || die "Go is not installed (need 1.25+): https://go.dev/dl/"
say "==> using $(go env GOVERSION)"
say "==> building $BINARY ..."
( cd "$ROOT" && go build -ldflags "-s -w" -o "$BINARY" "$PKG" ) || die "build failed"

# --- 2. choose an install directory ----------------------------------------
# Prefer somewhere already on PATH and writable: instant, no config, no sudo.
DEST=""
for d in /usr/local/bin /opt/homebrew/bin "$(go env GOPATH)/bin" "$HOME/.local/bin" "$HOME/bin"; do
	if on_path "$d" && [ -d "$d" ] && [ -w "$d" ]; then
		DEST="$d"
		break
	fi
done
# Fallback: a per-user dir we create and add to PATH ourselves.
NEED_PATH_EDIT=0
if [ -z "$DEST" ]; then
	DEST="$HOME/.local/bin"
	mkdir -p "$DEST"
	on_path "$DEST" || NEED_PATH_EDIT=1
fi

# --- 3. install ------------------------------------------------------------
if ! install -m 0755 "$ROOT/$BINARY" "$DEST/$BINARY" 2>/dev/null; then
	cp "$ROOT/$BINARY" "$DEST/$BINARY" && chmod 0755 "$DEST/$BINARY" || die "could not copy to $DEST"
fi
say "==> installed $DEST/$BINARY"

# --- 4. make sure it runs from anywhere ------------------------------------
if [ "$NEED_PATH_EDIT" -eq 0 ]; then
	say ""
	say "✔ done. Open a terminal anywhere and run:  $BINARY"
	say "  (if this exact terminal still says 'command not found', run 'hash -r' or open a new one)"
	exit 0
fi

# Add DEST to PATH by editing the right startup file for the user's shell.
marker="# added by lanchat installer"
export_line="export PATH=\"$DEST:\$PATH\""
shell_name="$(basename "${SHELL:-sh}")"
edited=""

append_line() { # $1 = rc file, $2 = line to add
	rc="$1"
	[ -e "$rc" ] || : > "$rc"
	if ! grep -qF "$marker" "$rc" 2>/dev/null; then
		printf '\n%s\n%s\n' "$marker" "$2" >> "$rc"
	fi
	edited="$edited $rc"
}

case "$shell_name" in
	zsh)
		append_line "$HOME/.zshrc" "$export_line"
		;;
	bash)
		append_line "$HOME/.bashrc" "$export_line"
		[ "$(uname 2>/dev/null)" = "Darwin" ] && append_line "$HOME/.bash_profile" "$export_line"
		;;
	fish)
		conf="$HOME/.config/fish/config.fish"
		mkdir -p "$(dirname "$conf")"
		append_line "$conf" "fish_add_path $DEST"
		;;
	*)
		append_line "$HOME/.profile" "$export_line"
		;;
esac

say ""
say "✔ installed, and added $DEST to your PATH in:$edited"
say ""
say "  This terminal won't see it until you reload. Do ONE of:"
say "      • open a new terminal window, or"
say "      • run:  export PATH=\"$DEST:\$PATH\""
say ""
say "  Then, from any directory:  $BINARY"
