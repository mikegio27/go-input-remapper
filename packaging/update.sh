#!/usr/bin/env bash
# Update an existing nereus system install in place.
#
# Stops the service (releasing device grabs and freeing the running binary so the
# new one can't hit "text file busy"), installs the binary, and starts the service
# again. By default it installs the latest prebuilt release (falling back to a
# source build). Run with sudo:
#
#   sudo packaging/update.sh                 # latest release (prebuilt)
#   sudo packaging/update.sh --source        # build from THIS checkout (local dev)
#   sudo packaging/update.sh --version v1.2.0 # a specific release tag
#
# The flags are the reliable way to choose a source: the equivalent env vars
# (GIR_BUILD_FROM_SOURCE / GIR_VERSION) are stripped by sudo's environment reset
# unless you set them on the sudo command line itself, so prefer the flags.
#
# To pull the latest release binary without a checkout at all, you can also just
# re-run the curl installer. For a first-time install, or when the udev rules /
# uinput module config change, use packaging/install-system.sh instead.
set -euo pipefail

usage() {
	cat >&2 <<'EOF'
Usage: sudo packaging/update.sh [options]

Update an installed nereus in place (stop service, install, restart).

Options:
  -s, --source         build from THIS checkout instead of downloading a release
  -v, --version <tag>  install a specific release tag (e.g. v1.2.0)
  -h, --help           show this help

With no options, installs the latest prebuilt release (source build fallback).
EOF
	exit "${1:-0}"
}

while [[ $# -gt 0 ]]; do
	case "$1" in
	-s | --source) export GIR_BUILD_FROM_SOURCE=1 ;;
	-v | --version)
		[[ $# -ge 2 ]] || { echo "--version needs a tag (e.g. --version v1.2.0)" >&2; exit 2; }
		export GIR_VERSION="$2"
		shift
		;;
	-h | --help) usage 0 ;;
	*) echo "unknown option: $1" >&2; usage 2 ;;
	esac
	shift
done

if [[ $EUID -ne 0 ]]; then
	echo "this updater must run as root (sudo packaging/update.sh)" >&2
	exit 1
fi

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN=/usr/local/bin/nereus
UNIT=nereus.service

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
