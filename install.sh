#!/bin/bash
#
# Wink - One-click Install Script for Linux
# https://github.com/makt28/wink
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/makt28/wink/main/install.sh | bash
#
# After installation, use the `wink` command to manage the service:
#   wink start | stop | restart | status | logs | update | uninstall | reinstall
#

set -euo pipefail

REPO="makt28/wink"
INSTALL_DIR="/opt/wink"
BIN_PATH="${INSTALL_DIR}/wink"
SERVICE_NAME="wink"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"
CLI_PATH="/usr/local/bin/wink"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

info()  { echo -e "${GREEN}[INFO]${NC} $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*"; exit 1; }

check_root() {
    if [ "$(id -u)" -ne 0 ]; then
        error "This script must be run as root. Try: sudo bash install.sh"
    fi
}

detect_arch() {
    local arch
    arch=$(uname -m)
    case "$arch" in
        x86_64|amd64)   echo "amd64" ;;
        aarch64|arm64)   echo "arm64" ;;
        *)               error "Unsupported architecture: $arch (only amd64 and arm64 are supported)" ;;
    esac
}

get_latest_version() {
    local version
    version=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | head -1 | sed -E 's/.*"tag_name":\s*"([^"]+)".*/\1/')
    if [ -z "$version" ]; then
        error "Failed to fetch latest version from GitHub"
    fi
    echo "$version"
}

download_binary() {
    local version="$1"
    local arch="$2"
    local filename="wink-linux-${arch}"
    local url="https://github.com/${REPO}/releases/download/${version}/${filename}"

    info "Downloading ${filename} (${version})..."
    mkdir -p "$INSTALL_DIR"

    if ! curl -fsSL -o "${BIN_PATH}" "$url"; then
        error "Download failed. Check if the release exists: ${url}"
    fi

    chmod +x "$BIN_PATH"
    info "Binary installed to ${BIN_PATH}"
}

create_systemd_service() {
    info "Creating systemd service..."
    cat > "$SERVICE_FILE" <<EOF
[Unit]
Description=Wink Uptime Monitor
After=network.target

[Service]
Type=simple
WorkingDirectory=${INSTALL_DIR}
ExecStart=${BIN_PATH}
Restart=on-failure
RestartSec=5
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF

    systemctl daemon-reload
    systemctl enable "$SERVICE_NAME" >/dev/null 2>&1
    info "Systemd service created and enabled"
}

create_cli() {
    info "Creating management command..."
    cat > "$CLI_PATH" <<'SCRIPT'
#!/bin/bash
set -euo pipefail

SERVICE_NAME="wink"
INSTALL_DIR="/opt/wink"
REPO="makt28/wink"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

info()  { echo -e "${GREEN}[INFO]${NC} $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*"; exit 1; }

check_root() {
    if [ "$(id -u)" -ne 0 ]; then
        error "This command requires root. Try: sudo wink $*"
    fi
}

detect_arch() {
    local arch
    arch=$(uname -m)
    case "$arch" in
        x86_64|amd64)   echo "amd64" ;;
        aarch64|arm64)   echo "arm64" ;;
        *)               error "Unsupported architecture: $arch" ;;
    esac
}

get_latest_version() {
    local version
    version=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | head -1 | sed -E 's/.*"tag_name":\s*"([^"]+)".*/\1/')
    if [ -z "$version" ]; then
        error "Failed to fetch latest version from GitHub"
    fi
    echo "$version"
}

cmd_start() {
    check_root
    systemctl start "$SERVICE_NAME"
    info "Wink started"
}

cmd_stop() {
    check_root
    systemctl stop "$SERVICE_NAME"
    info "Wink stopped"
}

cmd_restart() {
    check_root
    systemctl restart "$SERVICE_NAME"
    info "Wink restarted"
}

cmd_status() {
    systemctl status "$SERVICE_NAME" --no-pager
}

cmd_logs() {
    journalctl -u "$SERVICE_NAME" -f --no-pager
}

cmd_update() {
    check_root
    local arch version
    arch=$(detect_arch)
    version=$(get_latest_version)
    local filename="wink-linux-${arch}"
    local url="https://github.com/${REPO}/releases/download/${version}/${filename}"

    info "Updating to ${version}..."
    systemctl stop "$SERVICE_NAME" 2>/dev/null || true

    if ! curl -fsSL -o "${INSTALL_DIR}/wink" "$url"; then
        error "Download failed"
    fi
    chmod +x "${INSTALL_DIR}/wink"

    systemctl start "$SERVICE_NAME"
    info "Updated to ${version} and restarted"
}

cmd_uninstall() {
    check_root
    echo -e "${YELLOW}This will remove Wink binary, systemd service, and management command.${NC}"
    echo -e "${YELLOW}Data files in ${INSTALL_DIR} (config.json, history.json, etc.) will be preserved.${NC}"
    read -rp "Continue? [y/N] " confirm
    if [[ ! "$confirm" =~ ^[Yy]$ ]]; then
        info "Cancelled"
        exit 0
    fi

    systemctl stop "$SERVICE_NAME" 2>/dev/null || true
    systemctl disable "$SERVICE_NAME" 2>/dev/null || true
    rm -f "/etc/systemd/system/${SERVICE_NAME}.service"
    systemctl daemon-reload

    rm -f "${INSTALL_DIR}/wink"
    rm -f "/usr/local/bin/wink"

    info "Wink uninstalled. Data files preserved in ${INSTALL_DIR}"
}

cmd_reinstall() {
    check_root
    info "Reinstalling Wink..."
    local arch version
    arch=$(detect_arch)
    version=$(get_latest_version)
    local filename="wink-linux-${arch}"
    local url="https://github.com/${REPO}/releases/download/${version}/${filename}"

    systemctl stop "$SERVICE_NAME" 2>/dev/null || true

    if ! curl -fsSL -o "${INSTALL_DIR}/wink" "$url"; then
        error "Download failed"
    fi
    chmod +x "${INSTALL_DIR}/wink"

    systemctl start "$SERVICE_NAME"
    info "Reinstalled ${version} and restarted"
}

cmd_help() {
    echo "Wink - Uptime Monitor Management"
    echo ""
    echo "Usage: wink <command>"
    echo ""
    echo "Commands:"
    echo "  start       Start the Wink service"
    echo "  stop        Stop the Wink service"
    echo "  restart     Restart the Wink service"
    echo "  status      Show service status"
    echo "  logs        Follow service logs (Ctrl+C to exit)"
    echo "  update      Update to the latest release"
    echo "  uninstall   Remove Wink (preserves data files)"
    echo "  reinstall   Re-download and restart"
    echo "  help        Show this help message"
}

case "${1:-help}" in
    start)      cmd_start ;;
    stop)       cmd_stop ;;
    restart)    cmd_restart ;;
    status)     cmd_status ;;
    logs)       cmd_logs ;;
    update)     cmd_update ;;
    uninstall)  cmd_uninstall ;;
    reinstall)  cmd_reinstall ;;
    help|--help|-h) cmd_help ;;
    *)          echo "Unknown command: $1"; cmd_help; exit 1 ;;
esac
SCRIPT

    chmod +x "$CLI_PATH"
    info "Management command installed: wink"
}

do_install() {
    check_root

    info "Installing Wink..."
    local arch version
    arch=$(detect_arch)
    version=$(get_latest_version)

    download_binary "$version" "$arch"
    create_systemd_service
    create_cli

    systemctl start "$SERVICE_NAME"

    echo ""
    info "Wink ${version} installed successfully!"
    echo ""
    echo "  Web UI:    http://$(hostname -I 2>/dev/null | awk '{print $1}' || echo 'localhost'):8080"
    echo "  Login:     admin / 123456"
    echo "  Data dir:  ${INSTALL_DIR}"
    echo ""
    echo "  Management commands:"
    echo "    sudo wink start|stop|restart|status|logs|update|uninstall|reinstall"
    echo ""
}

do_install
