#!/bin/bash
set -euo pipefail

REPO="fullexpi/piguard"
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/piguard"
STATE_DIR="/var/lib/piguard"
LOG_DIR="/var/log/piguard"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${GREEN}🛡️  PiGuard Installer${NC}"
echo ""

# Check root
if [[ $EUID -ne 0 ]]; then
    echo -e "${RED}Error: This script must be run as root (sudo)${NC}"
    exit 1
fi

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
    aarch64)   BINARY_ARCH="linux-arm64" ;;
    armv7l)    BINARY_ARCH="linux-armv7" ;;
    x86_64)    BINARY_ARCH="linux-amd64" ;;
    *)
        echo -e "${RED}Unsupported architecture: $ARCH${NC}"
        exit 1
        ;;
esac

echo -e "  Architecture: ${YELLOW}$ARCH${NC} → ${YELLOW}piguard-$BINARY_ARCH${NC}"

# Get latest release
echo "  Fetching latest release..."
LATEST=$(curl -sL "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name"' | head -1 | cut -d'"' -f4)

if [[ -z "$LATEST" ]]; then
    echo -e "${RED}Error: Could not fetch latest release${NC}"
    exit 1
fi

echo -e "  Version: ${YELLOW}$LATEST${NC}"

# Download binary
DOWNLOAD_URL="https://github.com/$REPO/releases/download/$LATEST/piguard-$BINARY_ARCH"
echo "  Downloading..."
curl -sL "$DOWNLOAD_URL" -o "$INSTALL_DIR/piguard"
chmod 755 "$INSTALL_DIR/piguard"

# Create directories
mkdir -p "$CONFIG_DIR" "$STATE_DIR" "$LOG_DIR"
chmod 750 "$CONFIG_DIR" "$STATE_DIR"

# Install default config if not exists
if [[ ! -f "$CONFIG_DIR/config.yaml" ]]; then
    curl -sL "https://raw.githubusercontent.com/$REPO/$LATEST/configs/default.yaml" \
        -o "$CONFIG_DIR/config.yaml"
    echo -e "  Config: ${GREEN}$CONFIG_DIR/config.yaml${NC} (default)"
else
    echo -e "  Config: ${YELLOW}$CONFIG_DIR/config.yaml${NC} (existing, kept)"
fi

# Install systemd service
curl -sL "https://raw.githubusercontent.com/$REPO/$LATEST/configs/piguard.service" \
    -o /etc/systemd/system/piguard.service
systemctl daemon-reload

echo ""
echo -e "${GREEN}✅ PiGuard $LATEST installed!${NC}"
echo ""
echo "  Next steps:"
echo "  1. Configure notifications:"
echo "     sudo nano $CONFIG_DIR/config.yaml"
echo ""
echo "  2. Test notifications:"
echo "     sudo piguard test"
echo ""
echo "  3. Start PiGuard:"
echo "     sudo systemctl enable --now piguard"
echo ""
echo "  4. Check status:"
echo "     sudo piguard status"
echo ""
