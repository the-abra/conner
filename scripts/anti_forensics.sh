#!/usr/bin/env sh

# ==============================================================================
# CONNER Anti-Forensics & Security Shield (Go Edition)
# Description: Sets up iptables for DDoS protection, disables history logging,
# and links critical log files to /dev/null for absolute anonymity.
# Compatible with Alpine Linux / OpenRC.
# ==============================================================================

if [ "$(id -u)" -ne 0 ]; then
  echo "Please run as root"
  exit 1
fi

echo "[*] Initializing CONNER Anti-Forensics Shield (Go Edition)..."

# 3. NETWORK SECURITY & DDoS MITIGATION (iptables)
echo "[*] Configuring iptables for DDoS mitigation and stealth..."

# 1. DISABLE SHELL HISTORY
echo "[*] Disabling bash/sh history for all users..."
export HISTSIZE=0
export HISTFILESIZE=0
unset HISTFILE

# Ensure history is off for future sessions
if ! grep -q "HISTSIZE=0" /etc/profile; then
    echo "export HISTSIZE=0" >> /etc/profile
    echo "export HISTFILE=/dev/null" >> /etc/profile
fi

# Remove existing history files
find / -name ".bash_history" -type f -exec rm -f {} + 2>/dev/null
find / -name ".sh_history" -type f -exec rm -f {} + 2>/dev/null

# 2. WIPE AND LINK SYSTEM LOGS TO /dev/null
echo "[*] Shredding logs and linking to /dev/null..."
for file in \
    /var/log/messages \
    /var/log/syslog \
    /var/log/auth.log \
    /var/log/secure \
    /var/log/dmesg \
    /var/log/wtmp \
    /var/log/btmp \
    /var/log/lastlog \
    /var/log/faillog \
    /var/log/nginx/access.log \
    /var/log/nginx/error.log; do
    if [ -f "$file" ]; then
        shred -u "$file" 2>/dev/null || rm -f "$file"
    fi
    # If the directory doesn't exist, create it (like for nginx)
    mkdir -p "$(dirname "$file")"
    ln -s /dev/null "$file" 2>/dev/null
done

# Stop syslog service if running (Alpine/OpenRC)
if command -v rc-service >/dev/null 2>&1; then
    rc-service syslog stop 2>/dev/null
    rc-update del syslog boot 2>/dev/null
elif command -v systemctl >/dev/null 2>&1; then
    systemctl stop rsyslog 2>/dev/null
    systemctl disable rsyslog 2>/dev/null
fi

# Allow Go CONNER server port (default 6666, or override via $CONNER_PORT)
CONNER_PORT="${CONNER_PORT:-6666}"

if ! command -v iptables >/dev/null 2>&1; then
    apk add iptables 2>/dev/null || echo "[!] iptables not available, skipping firewall rules."
fi

if command -v iptables >/dev/null 2>&1; then
    if ! iptables -F 2>/dev/null; then
        echo "[!] iptables: insufficient permissions (no NET_ADMIN). Skipping firewall rules."
    else
iptables -X

# Drop invalid packets
iptables -A INPUT -m conntrack --ctstate INVALID -j DROP

# Drop fragmented packets
iptables -A INPUT -f -j DROP

# Drop TCP packets with abnormal flags (XMAS, NULL, FIN)
iptables -A INPUT -p tcp --tcp-flags ALL ALL -j DROP
iptables -A INPUT -p tcp --tcp-flags ALL NONE -j DROP
iptables -A INPUT -p tcp --tcp-flags SYN,FIN SYN,FIN -j DROP
iptables -A INPUT -p tcp --tcp-flags SYN,RST SYN,RST -j DROP
iptables -A INPUT -p tcp --tcp-flags FIN,RST FIN,RST -j DROP
iptables -A INPUT -p tcp --tcp-flags ACK,FIN FIN -j DROP
iptables -A INPUT -p tcp --tcp-flags ACK,PSH PSH -j DROP
iptables -A INPUT -p tcp --tcp-flags ACK,URG URG -j DROP

# Limit connections per IP on CONNER port
iptables -A INPUT -p tcp --dport "$CONNER_PORT" -m connlimit --connlimit-above 20 --connlimit-mask 32 -j REJECT --reject-with tcp-reset

# Allow established connections
iptables -A INPUT -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT

# Allow loopback
iptables -A INPUT -i lo -j ACCEPT

# Allow public-facing ports only
iptables -A INPUT -p tcp --dport 80 -j ACCEPT
iptables -A INPUT -p tcp --dport "$CONNER_PORT" -j ACCEPT

# Block everything else incoming
iptables -P FORWARD DROP
iptables -P OUTPUT ACCEPT
    fi # end iptables capability check
fi # end iptables

# 4. RAM-DISK ENFORCEMENT & HARDENING
echo "[*] Checking for RAM-only execution and applying Deep Shield..."
if ! grep -q "tmpfs /tmp" /proc/mounts; then
    echo "[!] /tmp is not on tmpfs. Mounting RAM disk..."
    mount -t tmpfs -o size=128M,noexec,nosuid,nodev tmpfs /tmp 2>/dev/null
fi

if ! grep -q "tmpfs /var/log" /proc/mounts; then
    echo "[!] /var/log is not on tmpfs. Mounting RAM disk..."
    mount -t tmpfs -o size=64M,noexec,nosuid,nodev tmpfs /var/log 2>/dev/null
fi

# 5. ENVIRONMENT HARDENING (for conner user)
if id "conner" >/dev/null 2>&1; then
    echo "export PATH=/usr/local/bin:/usr/bin:/bin" > /home/conner/.profile
    echo "export HISTSIZE=0" >> /home/conner/.profile
    echo "export HISTFILE=/dev/null" >> /home/conner/.profile
    # Do NOT expose Go tooling to the restricted user
    echo "export GOPATH=/dev/null" >> /home/conner/.profile
    chown root:root /home/conner/.profile
    chmod 644 /home/conner/.profile
fi

# 6. PROC PROTECTION
# Prevent users from seeing other processes
mount -o remount,hidepid=2 /proc 2>/dev/null || true

echo "[*] Anti-Forensics and Security Shield ACTIVE."
echo "[!] Warning: All logs are currently writing to /dev/null."
