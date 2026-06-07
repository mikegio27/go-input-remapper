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
CONFIG_DIR=/etc/go-input-remapper

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

echo "==> installing and enabling systemd service"
install -m 0644 "$REPO_ROOT/packaging/go-input-remapper.service" /etc/systemd/system/go-input-remapper.service
systemctl daemon-reload
systemctl enable --now go-input-remapper.service

echo
echo "Installed. Add yourself to the 'input' group to use the TUI without sudo:"
echo "    sudo usermod -aG input \$USER   # then log out and back in"
echo
echo "Then run:  go-input-remapper        (opens the TUI)"
echo "Status:    systemctl status go-input-remapper"
