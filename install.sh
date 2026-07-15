#!/usr/bin/env sh
# One-command installer for `lanchat` (macOS / Linux).
#
#   ./install.sh
#   curl -fsSL <raw-url>/install.sh | sh   # if you host it
#
# Builds the single binary from source with the Go toolchain and copies it to a
# directory on your PATH. No root required (falls back to ~/.local/bin).

set -eu

BINARY="lanchat"
SRC_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"

say()  { printf '%s\n' "$*"; }
die()  { printf 'error: %s\n' "$*" >&2; exit 1; }

# 1. Toolchain check.
command -v go >/dev/null 2>&1 || die "Go is not installed. Get it from https://go.dev/dl/ (need 1.25+), then re-run."

GOVER="$(go env GOVERSION 2>/dev/null || echo unknown)"
say "==> using $GOVER"

# 2. Build.
say "==> building $BINARY ..."
( cd "$SRC_DIR" && go build -ldflags "-s -w" -o "$BINARY" . ) || die "build failed"

# 3. Choose an install directory that is writable and (ideally) on PATH.
if [ -w /usr/local/bin ] 2>/dev/null; then
	DEST="/usr/local/bin"
elif [ -n "${HOME:-}" ]; then
	DEST="$HOME/.local/bin"
else
	DEST="$SRC_DIR"
fi
mkdir -p "$DEST"

install -m 0755 "$SRC_DIR/$BINARY" "$DEST/$BINARY" 2>/dev/null \
	|| cp "$SRC_DIR/$BINARY" "$DEST/$BINARY" && chmod 0755 "$DEST/$BINARY"

say "==> installed $DEST/$BINARY"

# 4. PATH hint.
case ":$PATH:" in
	*":$DEST:"*) : ;;
	*) say ""
	   say "    $DEST is not on your PATH. Add this to your shell profile:"
	   say "        export PATH=\"$DEST:\$PATH\"" ;;
esac

say ""
say "done. try it:   $BINARY               (open room 'lobby')"
say "                $BINARY -r team -ask    (private room)"
say ""
say "if '$BINARY' says 'command not found', restart your terminal (or run the"
say "full path: $DEST/$BINARY)."
say ""
say "tip: to share with a friend who doesn't have Go, just send them the single"
say "     '$BINARY' file — it is self-contained."
