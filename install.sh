#!/usr/bin/env bash
# === install.sh (repo root) — PUBLIC CURL ENTRYPOINT (bootstrap) ===
#
# This is the thin bootstrap users curl. It does NOT do the install itself — it
# fetches the source (for packaging assets + the source-build fallback) and then
# delegates to the real installer, packaging/install-system.sh.
#
# Quick install (no clone needed):
#
#   curl -fsSL https://raw.githubusercontent.com/mikegio27/nereus/main/install.sh | sudo bash
#
# It downloads a prebuilt binary (no Go required) when one exists for your
# platform, otherwise builds from source if Go is installed. Then it installs the
# udev rules, uinput module config, and systemd service.
#
# Environment overrides:
#   GIR_REF=<branch|tag>     source snapshot to use for packaging assets (default: main)
#   GIR_VERSION=vX.Y.Z       install a specific release instead of the latest
#   GIR_BUILD_FROM_SOURCE=1  force a source build instead of downloading a binary
#
# If you already have a checkout, just run: sudo ./install.sh
set -euo pipefail

REPO="mikegio27/nereus"

if [[ $EUID -ne 0 ]]; then
	echo "this installer must run as root — pipe it to 'sudo bash', or run 'sudo ./install.sh'" >&2
	exit 1
fi

# If invoked from a checkout (this script sits next to packaging/), use it
# directly. Otherwise (curl | bash) fetch a source snapshot for the packaging
# assets and the source-build fallback.
SRC=""
SELF="${BASH_SOURCE[0]:-}"
if [[ -n "$SELF" && -f "$(dirname "$SELF")/packaging/install-system.sh" ]]; then
	SRC="$(cd "$(dirname "$SELF")" && pwd)"
else
	REF="${GIR_REF:-main}"
	TMP="$(mktemp -d)"
	trap 'rm -rf "$TMP"' EXIT
	URL="https://github.com/$REPO/archive/$REF.tar.gz"
	echo "==> fetching source snapshot ($REF)"
	# Download to a file first (so a network failure is distinguishable from a tar
	# failure) with timeouts, so a blocked/slow host fails fast instead of hanging
	# this script silently — GitHub serves these archives from codeload.github.com,
	# a different host than the raw.githubusercontent.com that serves this script,
	# so one can be reachable while the other is not.
	if ! curl -fSL --connect-timeout 20 --max-time 300 "$URL" -o "$TMP/src.tar.gz"; then
		echo "error: could not download the source snapshot from $URL" >&2
		echo "  If codeload.github.com is blocked or slow on your network, install from a" >&2
		echo "  clone instead:" >&2
		echo "    git clone https://github.com/$REPO" >&2
		echo "    sudo ./nereus/install.sh" >&2
		echo "  (or download a release tarball from https://github.com/$REPO/releases)" >&2
		exit 1
	fi
	if ! tar xzf "$TMP/src.tar.gz" -C "$TMP"; then
		echo "error: the downloaded archive could not be extracted (truncated or corrupt)" >&2
		exit 1
	fi
	SRC="$(echo "$TMP"/*/)"
fi

INSTALLER="$SRC/packaging/install-system.sh"
if [[ ! -f "$INSTALLER" ]]; then
	echo "error: installer not found at $INSTALLER (unexpected archive layout)" >&2
	exit 1
fi

# Hand off to the real installer. Redirect stdin from /dev/null: when this script
# arrived via 'curl | sudo bash' our stdin is the (now-closed) script pipe, and a
# child that ever reads stdin would otherwise block on it.
exec bash "$INSTALLER" "$@" </dev/null
