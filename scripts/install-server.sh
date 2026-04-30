#!/usr/bin/env sh
# ==============================================================================
# CONNER - Server Installer for Alpine Linux (Go Edition)
# Installs: NGINX stealth proxy, Tor hidden service, restricted shell,
#           anti-forensics hardening, conner user.
# ==============================================================================

if [ "$(id -u)" -ne 0 ]; then
  echo "This script must be run as root." >&2
  exit 1
fi

set -e

# --- Configuration ---
CONNER_USER="conner"
INSTALL_DIR="$(cd "$(dirname "$0")/.." && pwd)"
CONNER_PORT="${CONNER_PORT:-6666}"
TOR_HIDDEN_DIR="/var/lib/tor/conner_chat"

echo "=================================================================="
echo "  CONNER Server Installer — Alpine Linux (Go Edition)"
echo "=================================================================="
echo ""

# --- 1. System Dependencies ---
echo "[*] Installing system dependencies..."
apk update
apk add --no-cache libc6-compat nginx nginx-mod-stream tor iptables

# --- 2. Go + Build (OPTIONAL — uncomment if building from source) ---
#
# apk add --no-cache libc6-compat go git
#
# echo "[*] Building server binary from source..."
# cd "$INSTALL_DIR"
# go mod tidy
# go build -ldflags="-s -w" -o "${INSTALL_DIR}/bin/conner-server" ./cmd/server/main.go
# echo "[+] Binary built: ${INSTALL_DIR}/bin/conner-server"

# --- 3. Restricted Shell Setup ---
echo "[*] Creating restricted shell environment for conner user..."
cat << 'EOF' > /usr/local/bin/conner-shell
#!/bin/sh
# Restricted Shell for CONNER Server User

# Resolve workspace: prefer CONNER_WORKDIR (set by entrypoint via profile.d),
# fall back to /workspace, then /tmp as last resort.
WORKDIR="${CONNER_WORKDIR:-/workspace}"
cd "$WORKDIR" 2>/dev/null || cd /tmp

SERVER_BIN="${WORKDIR}/conner-server"

if [ -z "$1" ]; then
    echo "===================================================="
    echo "       CONNER SHIELDED EXECUTION ENVIRONMENT"
    echo "===================================================="
    echo "Allowed commands: 'start-server', 'show-onion', 'exit'"
    echo ""
    while true; do
        printf "conner@shielded:~$ "
        if ! read -r input; then exit 0; fi
        case "$input" in
            "start-server")
                exec "$SERVER_BIN"
                ;;
            "show-onion")
                # Check readable copy first, then fall back to Tor dir
                ONION_FILE="${WORKDIR}/hostname"
                if [ -f "$ONION_FILE" ]; then
                    echo "[+] Onion Address: $(cat "$ONION_FILE")"
                elif [ -f /var/lib/tor/conner_chat/hostname ]; then
                    echo "[+] Onion Address: $(cat /var/lib/tor/conner_chat/hostname)"
                else
                    echo "[-] Onion address not yet generated. Run entrypoint first."
                fi
                ;;
            "exit"|"quit")
                exit 0
                ;;
            "")
                continue
                ;;
            *)
                echo "[-] Access Denied: '$input' is not allowed."
                ;;
        esac
    done
else
    if [ "$1" = "-c" ]; then
        case "$2" in
            "start-server")
                exec "$SERVER_BIN"
                ;;
            "show-onion")
                ONION_FILE="${WORKDIR}/hostname"
                if [ -f "$ONION_FILE" ]; then
                    cat "$ONION_FILE"
                elif [ -f /var/lib/tor/conner_chat/hostname ]; then
                    cat /var/lib/tor/conner_chat/hostname
                else
                    echo "[-] Onion address not generated yet."
                    exit 1
                fi
                ;;
            *)
                echo "[-] Access Denied: Command restricted."
                exit 1
                ;;
        esac
    else
        echo "[-] Access Denied: Command restricted."
        exit 1
    fi
fi
EOF
chmod 755 /usr/local/bin/conner-shell

if ! grep -qxF '/usr/local/bin/conner-shell' /etc/shells; then
    echo '/usr/local/bin/conner-shell' >> /etc/shells
fi

# --- 4. User & Directory Security ---
echo "[*] Hardening user permissions..."
if ! id "$CONNER_USER" >/dev/null 2>&1; then
    adduser -D -s /usr/local/bin/conner-shell "$CONNER_USER"
fi

mount -o remount,hidepid=2 /proc 2>/dev/null || echo "[!] Warning: Could not hide PID."
for tool in /usr/bin/sudo /sbin/apk /usr/bin/go; do
    [ -f "$tool" ] && chmod o-x "$tool" 2>/dev/null || true
done
mount -o remount,noexec /tmp 2>/dev/null || true

# --- 5. NGINX Stealth Proxy (Port 80 → Internal CONNER port) ---
echo "[*] Configuring NGINX stealth proxy (80 → :${CONNER_PORT})..."
rm -f /etc/nginx/http.d/*.conf /etc/nginx/conf.d/*.conf

cat << EOF > /etc/nginx/nginx.conf
user nginx;
worker_processes auto;
pcre_jit on;
error_log /dev/null crit;
pid /tmp/nginx.pid;
include /etc/nginx/modules/*.conf;

events {
    worker_connections 4096;
}

stream {
    server {
        listen 80;
        listen [::]:80;
        proxy_pass 127.0.0.1:${CONNER_PORT};
    }
}
EOF

chown -R nginx:nginx /var/lib/nginx 2>/dev/null || true

# --- 6. Tor Hidden Service ---
echo "[*] Configuring Tor Hidden Service..."
cat << EOF > /etc/tor/torrc
User tor
DataDirectory /var/lib/tor
Log err file /dev/null
SocksPort 0
HiddenServiceDir ${TOR_HIDDEN_DIR}/
HiddenServicePort 80 127.0.0.1:80
EOF

mkdir -p "$TOR_HIDDEN_DIR"
chown -R tor:tor /var/lib/tor
chmod 700 /var/lib/tor
chmod 700 "$TOR_HIDDEN_DIR"

# --- 7. Anti-Forensics Shield ---
echo "[*] Applying Anti-Forensics Shield..."
if [ -f "$INSTALL_DIR/scripts/anti_forensics.sh" ]; then
    chmod +x "$INSTALL_DIR/scripts/anti_forensics.sh"
    sh "$INSTALL_DIR/scripts/anti_forensics.sh"
fi

# --- 8. Start Services ---
echo "[*] Starting NGINX and Tor..."
rc-service nginx restart >/dev/null 2>&1 || nginx -g "daemon on;" 2>/dev/null || true
rc-service tor restart >/dev/null 2>&1 || tor >/dev/null 2>&1 &

# --- 9. Wait for Tor Hostname ---
echo "[*] Waiting for .onion hostname (30-60s on first run)..."
MAX_TRIES=30
COUNT=0
while [ ! -f "$TOR_HIDDEN_DIR/hostname" ] && [ "$COUNT" -lt "$MAX_TRIES" ]; do
    printf "."
    sleep 2
    COUNT=$((COUNT + 1))
done
echo ""

chmod +x "$INSTALL_DIR/scripts/entrypoint.sh" 2>/dev/null || true

# Expose hostname so the conner user can read it without root access.
# /var/lib/tor/conner_chat/ is 700 tor:tor — conner cannot enter it.
# We copy (not just symlink) so permissions can be set independently.
if [ -f "$TOR_HIDDEN_DIR/hostname" ]; then
    ONION_FILE="$INSTALL_DIR/hostname"
    cp "$TOR_HIDDEN_DIR/hostname" "$ONION_FILE"
    chmod 644 "$ONION_FILE"
    chown "${CONNER_USER}:${CONNER_USER}" "$ONION_FILE" 2>/dev/null || chmod 644 "$ONION_FILE"
    echo "[+] Hostname exposed at: $ONION_FILE (readable by conner)"
fi

# --- Done ---
echo ""
echo "=================================================================="
echo "  CONNER SERVER INSTALLATION COMPLETE"
echo "=================================================================="
echo ""
if [ -f "$TOR_HIDDEN_DIR/hostname" ]; then
    echo "  ONION ADDRESS   : $(cat "$TOR_HIDDEN_DIR/hostname")"
fi
echo "  SERVER BINARY   : ${INSTALL_DIR}/bin/conner-server"
echo "  CONNER PORT     : ${CONNER_PORT} (internal)"
echo "  PUBLIC PORT     : 80 (NGINX stealth proxy)"
echo "  USER            : ${CONNER_USER} (restricted shell)"
echo ""
echo "  START MANUALLY:"
echo "    su - ${CONNER_USER}"
echo "    start-server"
echo ""
echo "  AUTOBOOT:"
echo "    sh scripts/entrypoint.sh"
echo "=================================================================="
