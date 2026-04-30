#!/usr/bin/env sh
# ==============================================================================
# CONNER - Client Installer for Alpine Linux (Go Edition)
# Installs: Tor SOCKS5 proxy (for .onion routing), conner-client binary.
# Lightweight — no NGINX, no restricted shell, no anti-forensics.
# ==============================================================================

if [ "$(id -u)" -ne 0 ]; then
  echo "This script must be run as root." >&2
  exit 1
fi

set -e

# --- Configuration ---
INSTALL_DIR="$(cd "$(dirname "$0")/.." && pwd)"
TOR_SOCKS_PORT="9050"

echo "=================================================================="
echo "  CONNER Client Installer — Alpine Linux (Go Edition)"
echo "=================================================================="
echo ""

# --- 1. System Dependencies ---
echo "[*] Installing system dependencies..."
apk update
apk add --no-cache libc6-compat tor

# --- 2. Go + Build (OPTIONAL — uncomment if building from source) ---
#
# apk add --no-cache libc6-compat go git
#
# echo "[*] Building client binary from source..."
# cd "$INSTALL_DIR"
# go mod tidy
# go build -ldflags="-s -w" -o "${INSTALL_DIR}/bin/conner-client" ./cmd/client/main.go
# echo "[+] Binary built: ${INSTALL_DIR}/bin/conner-client"

# --- 3. Configure Tor (SOCKS5 proxy only — no hidden service) ---
echo "[*] Configuring Tor as local SOCKS5 proxy for .onion routing..."
cat << EOF > /etc/tor/torrc
User tor
DataDirectory /var/lib/tor
Log err file /dev/null
SocksPort 127.0.0.1:${TOR_SOCKS_PORT}
# No HiddenService — client-only mode
EOF

mkdir -p /var/lib/tor
chown -R tor:tor /var/lib/tor
chmod 700 /var/lib/tor

# --- 4. Start Tor ---
echo "[*] Starting Tor SOCKS5 proxy..."
rc-service tor restart >/dev/null 2>&1 || tor >/dev/null 2>&1 &

echo "[*] Waiting for Tor to bootstrap (30-60s on first run)..."
MAX_TRIES=30
COUNT=0
while [ "$COUNT" -lt "$MAX_TRIES" ]; do
    # Tor signals readiness by opening the SOCKS port
    if nc -z 127.0.0.1 "$TOR_SOCKS_PORT" 2>/dev/null; then
        break
    fi
    printf "."
    sleep 2
    COUNT=$((COUNT + 1))
done
echo ""

if ! nc -z 127.0.0.1 "$TOR_SOCKS_PORT" 2>/dev/null; then
    echo "[!] WARNING: Tor SOCKS5 port not yet open. .onion connections may fail."
    echo "    Check: tor --verify-config"
else
    echo "[+] Tor SOCKS5 proxy ready at 127.0.0.1:${TOR_SOCKS_PORT}"
fi

# --- Done ---
echo ""
echo "=================================================================="
echo "  CONNER CLIENT INSTALLATION COMPLETE"
echo "=================================================================="
echo ""
echo "  CLIENT BINARY   : ${INSTALL_DIR}/bin/conner-client"
echo "  TOR SOCKS5      : 127.0.0.1:${TOR_SOCKS_PORT}"
echo ""
echo "  CONNECT (normal IP):"
echo "    ${INSTALL_DIR}/bin/conner-client <nickname> <host>:<port>"
echo ""
echo "  CONNECT (via Tor .onion):"
echo "    ${INSTALL_DIR}/bin/conner-client <nickname> <address>.onion:80"
echo "    (client auto-routes .onion addresses through Tor SOCKS5)"
echo "=================================================================="
