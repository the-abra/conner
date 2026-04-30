#!/usr/bin/env sh
# ==============================================================================
# CONNER - Container Entrypoint (Go Edition)
# Automates service startup for the Go-based CONNER server on Alpine Linux
# ==============================================================================

set -e

CONNER_PORT="${CONNER_PORT:-6666}"
TOR_HIDDEN_DIR="/var/lib/tor/conner_chat"
INSTALL_DIR="$(cd "$(dirname "$0")/.." && pwd)"

echo "[*] Booting CONNER Shielded Environment (Go Edition)..."

# 1. Run Anti-Forensics Shield
if [ -f "$INSTALL_DIR/scripts/anti_forensics.sh" ]; then
    echo "[*] Activating Anti-Forensics Shield..."
    sh "$INSTALL_DIR/scripts/anti_forensics.sh"
fi

# 2. Fix Tor Permissions
echo "[*] Setting up Tor environment..."
mkdir -p "$TOR_HIDDEN_DIR"
chown -R tor:tor /var/lib/tor 2>/dev/null || true
chmod 700 /var/lib/tor 2>/dev/null || true
chmod 700 "$TOR_HIDDEN_DIR" 2>/dev/null || true

# 3. Start NGINX stealth proxy
echo "[*] Starting NGINX stealth proxy (port 80 → :${CONNER_PORT})..."
pkill nginx 2>/dev/null || true
sleep 1
nginx

# 4. Start Tor
echo "[*] Starting Tor hidden service..."
pkill tor 2>/dev/null || true
sleep 1
tor >/dev/null 2>&1 &

# 5. Wait for Tor hostname
echo "[*] Waiting for Tor to initialize (may take 30-60s on first run)..."
MAX_TRIES=30
COUNT=0
while [ ! -f "$TOR_HIDDEN_DIR/hostname" ] && [ "$COUNT" -lt "$MAX_TRIES" ]; do
    printf "."
    sleep 2
    COUNT=$((COUNT + 1))
done
echo ""

if [ ! -f "$TOR_HIDDEN_DIR/hostname" ]; then
    echo "[!] ERROR: Tor failed to generate hostname within timeout."
    echo "    Check: tor --verify-config"
    exit 1
fi

ONION_ADDR=$(cat "$TOR_HIDDEN_DIR/hostname")
echo "[+] Onion Address: $ONION_ADDR"

# 6. Expose hostname so conner user can read it.
#    /var/lib/tor/conner_chat/ is 700 tor:tor — a symlink pointing inside
#    it is unreadable by conner. We copy and set 644 instead.
cp "$TOR_HIDDEN_DIR/hostname" "$INSTALL_DIR/hostname"
chmod 644 "$INSTALL_DIR/hostname"
chown conner:conner "$INSTALL_DIR/hostname" 2>/dev/null || true

# 8. Export CONNER_PORT via profile.d so the conner login shell inherits it.
#    'su -' sources /etc/profile → /etc/profile.d/* automatically.
#    This avoids passing "VAR=val cmd" to the restricted shell (which blocks it).
mkdir -p /etc/profile.d
printf 'export CONNER_PORT="%s"\nexport CONNER_WORKDIR="%s"\n' \
    "${CONNER_PORT}" "${INSTALL_DIR}" \
    > /etc/profile.d/conner.sh
chmod 644 /etc/profile.d/conner.sh

# 9. Launch CONNER Server as the restricted user
echo "[*] Launching CONNER Go Server..."
echo "    Listening on 0.0.0.0:${CONNER_PORT}"
exec su - conner -c "start-server"
