#!/usr/bin/env bash
# Update an existing go-input-remapper system install in place.
#
# Use this after pulling new code: it stops the service (releasing device grabs
# and freeing the running binary so the rebuild can't hit "text file busy"),
# rebuilds the binary, refreshes the systemd unit in case it changed, and starts
# the service again. Run with sudo from the repo root:
#
#   sudo packaging/update.sh
#
# For a first-time install, or when the udev rules / uinput module config change,
# use packaging/install.sh instead.
set -euo pipefail

if [[ $EUID -ne 0 ]]; then
	echo "this updater must run as root (sudo packaging/update.sh)" >&2
	exit 1
fi

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN=/usr/local/bin/go-input-remapper
UNIT=go-input-remapper.service

if [[ ! -f /etc/systemd/system/$UNIT ]]; then
	echo "service not installed yet — run 'sudo packaging/install.sh' first" >&2
	exit 1
fi

echo "==> stopping $UNIT (frees the binary and releases device grabs)"
systemctl stop "$UNIT" 2>/dev/null || true

echo "==> building $BIN"
( cd "$REPO_ROOT" && go build -o "$BIN" . )

# Note: the installed unit is customized by install.sh (config dir / ProtectHome),
# so update.sh deliberately leaves it alone. Re-run install.sh if the unit itself
# changes.

echo "==> starting $UNIT"
systemctl start "$UNIT"

echo
echo "Updated. Current status:"
systemctl --no-pager --lines=0 status "$UNIT" || true
echo
echo "Tip: follow the daemon log while you test:"
echo "    journalctl -u $UNIT -f"
