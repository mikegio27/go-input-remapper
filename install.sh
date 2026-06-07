#!/usr/bin/env bash
# One-line installer for go-input-remapper.
#
# Quick install (no clone needed):
#
#   curl -fsSL https://raw.githubusercontent.com/mikegio27/go-input-remapper/main/install.sh | sudo bash
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

REPO="mikegio27/go-input-remapper"

if [[ $EUID -ne 0 ]]; then
	echo "this installer must run as root — pipe it to 'sudo bash', or run 'sudo ./install.sh'" >&2
	exit 1
fi

# If invoked from a checkout (this script sits next to packaging/), use it
# directly. Otherwise (curl | bash) fetch a source snapshot for the packaging
# assets and the source-build fallback.
SRC=""
SELF="${BASH_SOURCE[0]:-}"
if [[ -n "$SELF" && -f "$(dirname "$SELF")/packaging/install.sh" ]]; then
	SRC="$(cd "$(dirname "$SELF")" && pwd)"
else
	REF="${GIR_REF:-main}"
	TMP="$(mktemp -d)"
	trap 'rm -rf "$TMP"' EXIT
	echo "==> fetching source snapshot ($REF)"
	curl -fsSL "https://github.com/$REPO/archive/$REF.tar.gz" | tar xz -C "$TMP"
	SRC="$(echo "$TMP"/*/)"
fi

exec bash "$SRC/packaging/install.sh" "$@"
