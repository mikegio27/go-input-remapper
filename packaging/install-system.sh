#!/usr/bin/env bash
# === packaging/install-system.sh — THE INSTALLER ===
#
# The actual installer that does the system-level work. It runs either directly
# from a checkout or via the top-level bootstrap (../install.sh, the public
# `curl … | sudo bash` entrypoint — don't confuse the two).
#
# It installs the binary (downloading a prebuilt release when available, else
# building from source), the udev rules + uinput module config + systemd unit,
# creates the config directory, and enables the service. Run with sudo from the
# repo root:
#
#   sudo packaging/install-system.sh
#
# Uninstall with packaging/uninstall.sh.
#
# Environment overrides (see packaging/lib.sh): GIR_VERSION, GIR_BUILD_FROM_SOURCE.
set -euo pipefail

if [[ $EUID -ne 0 ]]; then
	echo "this installer must run as root (sudo packaging/install-system.sh)" >&2
	exit 1
fi

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN=/usr/local/bin/nereus
UNIT=/etc/systemd/system/nereus.service

# shellcheck source=packaging/lib.sh
source "$REPO_ROOT/packaging/lib.sh"

# Run the root service against the invoking desktop user's config so the same
# files the TUI edits (~/.config) are what the daemon reads. Falls back to a
# system-wide /etc config when installed straight as root (no sudo user).
TARGET_USER="${SUDO_USER:-root}"
if [[ "$TARGET_USER" == "root" ]]; then
	CONFIG_DIR=/etc/nereus
	PROTECT_HOME=true
else
	USER_HOME="$(getent passwd "$TARGET_USER" | cut -d: -f6)"
	CONFIG_DIR="$USER_HOME/.config/nereus"
	PROTECT_HOME=false # the daemon must read/write the user's home config
fi

# Stop a running service first so the rebuild can't hit "text file busy" and the
# new binary is actually picked up when we start again below.
systemctl stop nereus.service 2>/dev/null || true

install_binary

echo "==> installing udev rules and uinput module config"
install -m 0644 "$REPO_ROOT/packaging/99-nereus.rules" /etc/udev/rules.d/99-nereus.rules
install -m 0644 "$REPO_ROOT/packaging/uinput.conf" /etc/modules-load.d/uinput.conf
modprobe uinput || true
udevadm control --reload-rules && udevadm trigger

echo "==> creating config dir $CONFIG_DIR"
mkdir -p "$CONFIG_DIR/profiles"
if [[ ! -f "$CONFIG_DIR/config.toml" ]]; then
	printf 'active_profile = "default"\nvirtual_prefix = "nereus"\n' > "$CONFIG_DIR/config.toml"
fi
if [[ ! -f "$CONFIG_DIR/profiles/default.toml" ]]; then
	printf 'name = "default"\n' > "$CONFIG_DIR/profiles/default.toml"
fi
if [[ "$TARGET_USER" != "root" ]]; then
	chown -R "$TARGET_USER:" "$CONFIG_DIR" # owner+group to the user; the TUI edits these
fi

echo "==> installing and enabling systemd service (config: $CONFIG_DIR)"
install -m 0644 "$REPO_ROOT/packaging/nereus.service" "$UNIT"
# Point the unit at the resolved config dir and let it reach there if it's under
# /home (ProtectHome). The shipped unit defaults to the /etc system layout.
sed -i \
	-e "s#--config-dir /etc/nereus#--config-dir $CONFIG_DIR#" \
	-e "s#^ProtectHome=.*#ProtectHome=$PROTECT_HOME#" \
	-e "s#^ReadWritePaths=.*#ReadWritePaths=$CONFIG_DIR /run#" \
	"$UNIT"
systemctl daemon-reload
systemctl enable nereus.service
systemctl restart nereus.service

echo
echo "Installed. Add yourself to the 'input' group so the TUI can reach the daemon"
echo "socket (and read devices directly) without sudo:"
echo "    sudo usermod -aG input \$USER   # then log out and back in"
echo
echo "Then run:  nereus        (opens the TUI; auto-finds the system socket)"
echo "Status:    systemctl status nereus"
echo "Logs:      journalctl -u nereus -f"
echo "Uninstall: sudo packaging/uninstall.sh   (add --purge to also remove your config)"
