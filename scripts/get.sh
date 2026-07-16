#!/usr/bin/env sh
# Web installer for `lanchat` (macOS / Linux) — no Go, no git, no build.
#
#   curl -fsSL https://raw.githubusercontent.com/granthgg/sshhh-lanchat/main/scripts/get.sh | sh
#
# Downloads the prebuilt binary for this machine from the latest GitHub
# release, verifies its SHA-256 against the release's checksums.txt, and
# installs it so `lanchat` runs from any directory.
#
# Why this avoids the macOS "unverified developer" block: Gatekeeper only
# screens files that arrive with the quarantine attribute, which browsers set
# and curl does not. The binary is unsigned either way — verify it via the
# published checksums (done automatically here), or read the source and build
# it yourself.
#
#   LANCHAT_VERSION=v2.2.0 sh get.sh    # pin a release (default: latest)

set -eu

REPO="granthgg/sshhh-lanchat"
BINARY="lanchat"
VERSION="${LANCHAT_VERSION:-latest}"

say() { printf '%s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }
on_path() { case ":${PATH}:" in *":$1:"*) return 0 ;; *) return 1 ;; esac; }

command -v curl >/dev/null 2>&1 || die "curl is required"

# --- 1. pick the asset for this machine -------------------------------------
case "$(uname -s)" in
	Darwin) os="macos" ;;
	Linux)  os="linux" ;;
	*) die "unsupported OS: $(uname -s) — use scripts/get.ps1 on Windows, or build from source" ;;
esac
case "$(uname -m)" in
	arm64|aarch64) arch="arm64" ;;
	x86_64|amd64)  arch="amd64" ;;
	*) die "unsupported architecture: $(uname -m) — build from source instead" ;;
esac
ASSET="lanchat-$os-$arch"

if [ "$VERSION" = "latest" ]; then
	BASE="https://github.com/$REPO/releases/latest/download"
else
	BASE="https://github.com/$REPO/releases/download/$VERSION"
fi

# --- 2. download and verify ---------------------------------------------------
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT INT TERM

say "==> downloading $ASSET ($VERSION) ..."
curl -fsSL -o "$TMP/$ASSET" "$BASE/$ASSET" ||
	die "download failed — check https://github.com/$REPO/releases"

if curl -fsSL -o "$TMP/checksums.txt" "$BASE/checksums.txt" 2>/dev/null; then
	want="$(grep " $ASSET\$" "$TMP/checksums.txt" | awk '{print $1}' | head -n1)"
	[ -n "$want" ] || die "checksums.txt has no entry for $ASSET"
	if command -v sha256sum >/dev/null 2>&1; then
		got="$(sha256sum "$TMP/$ASSET" | awk '{print $1}')"
	elif command -v shasum >/dev/null 2>&1; then
		got="$(shasum -a 256 "$TMP/$ASSET" | awk '{print $1}')"
	else
		die "need sha256sum or shasum to verify the download"
	fi
	[ "$got" = "$want" ] ||
		die "checksum verification FAILED (expected $want, got $got) — refusing to install"
	say "==> checksum verified"
else
	say "==> note: this release has no checksums.txt (pre-2.2.0); skipping verification"
fi

chmod 0755 "$TMP/$ASSET"
# Belt and braces: curl downloads carry no quarantine attribute, but clear it
# in case a wrapper added one (macOS only; harmless elsewhere).
command -v xattr >/dev/null 2>&1 && xattr -d com.apple.quarantine "$TMP/$ASSET" 2>/dev/null || true

# --- 3. choose an install directory (same policy as scripts/install.sh) -------
DEST=""
for d in /usr/local/bin /opt/homebrew/bin "$HOME/.local/bin" "$HOME/bin"; do
	if on_path "$d" && [ -d "$d" ] && [ -w "$d" ]; then
		DEST="$d"
		break
	fi
done
NEED_PATH_EDIT=0
if [ -z "$DEST" ]; then
	DEST="$HOME/.local/bin"
	mkdir -p "$DEST"
	on_path "$DEST" || NEED_PATH_EDIT=1
fi

mv "$TMP/$ASSET" "$DEST/$BINARY"
chmod 0755 "$DEST/$BINARY"
say "==> installed $DEST/$BINARY"

# --- 4. make sure it runs from anywhere ---------------------------------------
if [ "$NEED_PATH_EDIT" -eq 0 ]; then
	say ""
	say "✔ done. Open a terminal anywhere and run:  $BINARY"
	say "  (if this exact terminal says 'command not found', run 'hash -r' or open a new one)"
	exit 0
fi

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
