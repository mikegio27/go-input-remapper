#!/usr/bin/env bash
# Install go-input-remapper as a system service.
#
# Builds the binary, installs udev rules + the uinput module config + the systemd
# unit, creates the system config directory, and enables the service. Run with
# sudo from the repo root:
#
#   sudo packaging/install.sh
#
set -euo pipefail

if [[ $EUID -ne 0 ]]; then
	echo "this installer must run as root (sudo packaging/install.sh)" >&2
	exit 1
fi

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN=/usr/local/bin/go-input-remapper
UNIT=/etc/systemd/system/go-input-remapper.service

# Run the root service against the invoking desktop user's config so the same
# files the TUI edits (~/.config) are what the daemon reads. Falls back to a
# system-wide /etc config when installed straight as root (no sudo user).
TARGET_USER="${SUDO_USER:-root}"
if [[ "$TARGET_USER" == "root" ]]; then
	CONFIG_DIR=/etc/go-input-remapper
	PROTECT_HOME=true
else
	USER_HOME="$(getent passwd "$TARGET_USER" | cut -d: -f6)"
	CONFIG_DIR="$USER_HOME/.config/go-input-remapper"
	PROTECT_HOME=false # the daemon must read/write the user's home config
fi

# Stop a running service first so the rebuild can't hit "text file busy" and the
# new binary is actually picked up when we start again below.
systemctl stop go-input-remapper.service 2>/dev/null || true

echo "==> building"
( cd "$REPO_ROOT" && go build -o "$BIN" . )

echo "==> installing udev rules and uinput module config"
install -m 0644 "$REPO_ROOT/packaging/99-go-input-remapper.rules" /etc/udev/rules.d/99-go-input-remapper.rules
install -m 0644 "$REPO_ROOT/packaging/uinput.conf" /etc/modules-load.d/uinput.conf
modprobe uinput || true
udevadm control --reload-rules && udevadm trigger

echo "==> creating config dir $CONFIG_DIR"
mkdir -p "$CONFIG_DIR/profiles"
if [[ ! -f "$CONFIG_DIR/config.toml" ]]; then
	printf 'active_profile = "default"\nvirtual_prefix = "go-input-remapper"\n' > "$CONFIG_DIR/config.toml"
fi
if [[ ! -f "$CONFIG_DIR/profiles/default.toml" ]]; then
	printf 'name = "default"\n' > "$CONFIG_DIR/profiles/default.toml"
fi
if [[ "$TARGET_USER" != "root" ]]; then
	chown -R "$TARGET_USER:" "$CONFIG_DIR" # owner+group to the user; the TUI edits these
fi

echo "==> installing and enabling systemd service (config: $CONFIG_DIR)"
install -m 0644 "$REPO_ROOT/packaging/go-input-remapper.service" "$UNIT"
# Point the unit at the resolved config dir and let it reach there if it's under
# /home (ProtectHome). The shipped unit defaults to the /etc system layout.
sed -i \
	-e "s#--config-dir /etc/go-input-remapper#--config-dir $CONFIG_DIR#" \
	-e "s#^ProtectHome=.*#ProtectHome=$PROTECT_HOME#" \
	-e "s#^ReadWritePaths=.*#ReadWritePaths=$CONFIG_DIR /run#" \
	"$UNIT"
systemctl daemon-reload
systemctl enable go-input-remapper.service
systemctl restart go-input-remapper.service

echo
echo "Installed. Add yourself to the 'input' group so the TUI can reach the daemon"
echo "socket (and read devices directly) without sudo:"
echo "    sudo usermod -aG input \$USER   # then log out and back in"
echo
echo "Then run:  go-input-remapper        (opens the TUI; auto-finds the system socket)"
echo "Status:    systemctl status go-input-remapper"
echo "Logs:      journalctl -u go-input-remapper -f"
