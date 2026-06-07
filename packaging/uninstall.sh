#!/usr/bin/env bash
# === packaging/uninstall.sh — UNINSTALLER ===
#
# Reverses packaging/install-system.sh: stops and removes the systemd service,
# the binary, the udev rules, and the uinput module-load config. By default it
# KEEPS your config (profiles) — pass --purge to remove it too.
#
# Self-contained (no other repo files needed), so it also works via curl:
#
#   sudo packaging/uninstall.sh [--purge]
#   curl -fsSL https://raw.githubusercontent.com/mikegio27/go-input-remapper/main/packaging/uninstall.sh | sudo bash
#   curl -fsSL .../packaging/uninstall.sh | sudo bash -s -- --purge
set -euo pipefail

BIN=/usr/local/bin/go-input-remapper
UNIT_NAME=go-input-remapper.service
UNIT=/etc/systemd/system/$UNIT_NAME
UDEV_RULE=/etc/udev/rules.d/99-go-input-remapper.rules
MODULES_CONF=/etc/modules-load.d/uinput.conf

PURGE=0
for arg in "$@"; do
	case "$arg" in
	--purge) PURGE=1 ;;
	-h | --help)
		sed -n '2,12p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
		exit 0
		;;
	*)
		echo "unknown option: $arg (use --purge or --help)" >&2
		exit 1
		;;
	esac
done

if [[ $EUID -ne 0 ]]; then
	echo "this uninstaller must run as root (sudo packaging/uninstall.sh)" >&2
	exit 1
fi

# Resolve the config dir the same way install-system.sh did, so --purge targets
# the right place and we can tell the user where their config is being kept.
TARGET_USER="${SUDO_USER:-root}"
if [[ "$TARGET_USER" == "root" ]]; then
	CONFIG_DIR=/etc/go-input-remapper
else
	USER_HOME="$(getent passwd "$TARGET_USER" | cut -d: -f6)"
	CONFIG_DIR="$USER_HOME/.config/go-input-remapper"
fi

echo "==> stopping and disabling $UNIT_NAME"
systemctl stop "$UNIT_NAME" 2>/dev/null || true
systemctl disable "$UNIT_NAME" 2>/dev/null || true

if [[ -f "$UNIT" ]]; then
	echo "==> removing systemd unit $UNIT"
	rm -f "$UNIT"
	systemctl daemon-reload
	systemctl reset-failed "$UNIT_NAME" 2>/dev/null || true
fi

echo "==> removing binary $BIN"
rm -f "$BIN"

if [[ -f "$UDEV_RULE" ]]; then
	echo "==> removing udev rule $UDEV_RULE"
	rm -f "$UDEV_RULE"
	udevadm control --reload-rules && udevadm trigger || true
fi

if [[ -f "$MODULES_CONF" ]]; then
	# Only ours to remove; leave the uinput module loaded since other software
	# (other remappers, Steam, etc.) may rely on /dev/uinput.
	echo "==> removing uinput module-load config $MODULES_CONF (module stays loaded until reboot)"
	rm -f "$MODULES_CONF"
fi

if [[ "$PURGE" -eq 1 ]]; then
	if [[ -d "$CONFIG_DIR" ]]; then
		echo "==> purging config dir $CONFIG_DIR"
		rm -rf "$CONFIG_DIR"
	fi
else
	if [[ -d "$CONFIG_DIR" ]]; then
		echo "==> keeping your config at $CONFIG_DIR (re-run with --purge to remove it)"
	fi
fi

echo
echo "Uninstalled."
echo "Note: your 'input' group membership was left as-is. To undo it:"
echo "    sudo gpasswd -d \$USER input   # then log out and back in"
