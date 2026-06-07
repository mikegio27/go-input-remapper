#!/usr/bin/env bash
# Update an existing go-input-remapper system install in place.
#
# Stops the service (releasing device grabs and freeing the running binary so the
# new one can't hit "text file busy"), installs the latest binary (a prebuilt
# release when available, else a source build), and starts the service again. Run
# with sudo:
#
#   sudo packaging/update.sh
#
# To pull the latest release binary without a checkout at all, you can also just
# re-run the curl installer. For a first-time install, or when the udev rules /
# uinput module config change, use packaging/install-system.sh instead.
#
# Environment overrides (see packaging/lib.sh): GIR_VERSION, GIR_BUILD_FROM_SOURCE.
set -euo pipefail

if [[ $EUID -ne 0 ]]; then
	echo "this updater must run as root (sudo packaging/update.sh)" >&2
	exit 1
fi

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN=/usr/local/bin/go-input-remapper
UNIT=go-input-remapper.service

# shellcheck source=packaging/lib.sh
source "$REPO_ROOT/packaging/lib.sh"

if [[ ! -f /etc/systemd/system/$UNIT ]]; then
	echo "service not installed yet — run 'sudo packaging/install-system.sh' first" >&2
	exit 1
fi

echo "==> stopping $UNIT (frees the binary and releases device grabs)"
systemctl stop "$UNIT" 2>/dev/null || true

install_binary

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
