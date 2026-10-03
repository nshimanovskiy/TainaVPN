#!/bin/sh
# Installs Tainavpn for the current user and grants the binary the network
# capabilities needed for TUN mode (no need to run the app as root).
set -e
cd "$(dirname "$0")"
BIN_DIR="$HOME/.local/bin"
APP_DIR="$HOME/.local/share/applications"
ICON_DIR="$HOME/.local/share/icons/hicolor/512x512/apps"
mkdir -p "$BIN_DIR" "$APP_DIR" "$ICON_DIR"
install -m 755 tainavpn "$BIN_DIR/tainavpn"
install -m 644 tainavpn.png "$ICON_DIR/tainavpn.png"
sed "s|@BIN@|$BIN_DIR/tainavpn|" tainavpn.desktop > "$APP_DIR/tainavpn.desktop"
echo "Granting CAP_NET_ADMIN to $BIN_DIR/tainavpn (sudo password may be required)…"
sudo setcap cap_net_admin,cap_net_bind_service,cap_net_raw+ep "$BIN_DIR/tainavpn"
echo "Done. Launch «Tainavpn» from the applications menu or run: $BIN_DIR/tainavpn"
