#!/usr/bin/env sh
# Uninstaller for lanchat (macOS / Linux). Reverses scripts/install.sh and
# scripts/get.sh:
#
#   1. removes the `lanchat` binary from wherever either installer put it, and
#   2. removes the PATH line they add to your shell startup file in the
#      fallback case (marked with a comment).
#
# lanchat stores no config, data, or history, so this removes it completely.
#
#   ./scripts/uninstall.sh

set -u

BINARY="lanchat"
MARKER="# added by lanchat installer"

say() { printf '%s\n' "$*"; }

# --- 1. remove the binary from the directories an installer might have used --
removed=0
for d in /usr/local/bin /opt/homebrew/bin "$(go env GOPATH 2>/dev/null)/bin" "$HOME/.local/bin" "$HOME/bin"; do
	f="$d/$BINARY"
	if [ -e "$f" ] || [ -L "$f" ]; then
		if rm -f "$f" 2>/dev/null; then
			say "removed $f"
			removed=1
		else
			say "could not remove $f (permission denied) — try:  sudo rm \"$f\""
		fi
	fi
done
[ "$removed" -eq 0 ] && say "no $BINARY binary found in the usual install locations"

# Warn about any other copy still reachable on PATH (e.g. a manual install).
other="$(command -v "$BINARY" 2>/dev/null || true)"
[ -n "$other" ] && say "note: '$BINARY' still resolves to $other — remove that copy manually if it is a leftover"

# --- 2. remove the PATH line the installer may have added ------------------
# The installer appends a blank line, the marker comment, then one PATH line.
# We drop the marker line and the single line that follows it.
clean_rc() {
	rc="$1"
	[ -f "$rc" ] || return 0
	grep -qF "$MARKER" "$rc" 2>/dev/null || return 0
	tmp="$rc.lanchat-uninstall.$$"
	if awk -v m="$MARKER" '
		index($0, m) { skip = 2 }
		skip > 0     { skip--; next }
		             { print }
	' "$rc" > "$tmp"; then
		cat "$tmp" > "$rc" && say "removed PATH line from $rc"
	fi
	rm -f "$tmp"
}
for rc in "$HOME/.zshrc" "$HOME/.bashrc" "$HOME/.bash_profile" "$HOME/.profile" "$HOME/.config/fish/config.fish"; do
	clean_rc "$rc"
done

say ""
say "done — lanchat is fully removed."
say "open a new terminal (or run 'hash -r') so the change takes effect."
