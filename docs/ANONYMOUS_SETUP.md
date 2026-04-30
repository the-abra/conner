# Ultimate Anonymity & Security Guide for CONNER Server

This guide provides step-by-step instructions for deploying the CONNER Chat Server ensuring absolute anonymity and privacy. It covers security considerations during installation, setup, and operational usage.

## 1. Pre-Installation: Operational Security (OpSec)

Before installing anything, consider your physical and network environment:
*   **Hardware**: Use dedicated hardware (like a Raspberry Pi or an isolated VM) with no personal data attached.
*   **Network**: Never install or operate the server from your home IP. Connect through a commercial VPN or route your SSH traffic through Tor before executing the installation script.
*   **Diskless Mode**: Alpine Linux can run entirely in RAM (`sys` or `data` mode set to `none`). If power is lost, all data vanishes instantly. Do not use swap partitions.

## 2. Operating System Setup (Alpine Linux)

### Anti-Forensics: Disabling System Logging
We must disable `syslog` to ensure the OS does not keep track of network connections or internal events.
```bash
rc-update del syslog boot
rc-service syslog stop
find /var/log -type f -exec truncate -s 0 {} \;
```
The `scripts/anti_forensics.sh` script automates this by linking log files to `/dev/null` and setting up `tmpfs` mounts for `/var/log` and `/tmp`.

## 3. Application Setup & Network Hardening

### NGINX Reverse Proxy (TCP Stream Mode)
CONNER communicates via raw TCP sockets with custom framing. The proxy must be configured to pass TCP streams without HTTP headers, and it must keep no logs.

> [!CAUTION]
> **Important Note on Binaries:**
> If you are using pre-compiled binaries from the release page, ensure you rename the architecture-specific file (e.g., `conner-server-linux-amd64`) to **`conner-server`**. The restricted shell and installation scripts are hardcoded to look for this exact filename.

```nginx
stream {
    server {
        listen 80;
        listen [::]:80;
        proxy_pass 127.0.0.1:6666;
        proxy_buffer_size 128k;
    }
}
```

### Tor Hidden Service
Hosting as a Tor Hidden Service hides the server's IP address and provides end-to-end encryption by default.
*   The `install-server.sh` script automatically configures Tor to point to port 80 (NGINX), which forwards to 6666 (Go Server).
*   **Important**: Tor logs are redirected to `/dev/null` (`Log err file /dev/null`) to leave no footprint.

### Restricted Shell Environment
The application runs under a restricted user (`conner`) using a custom shell.
*   The user cannot execute system commands like `ls`, `cd`, or `su`.
*   They are only allowed to run `conner-server` and view the `.onion` address.
*   Execution rights to critical package managers (`apk`) and privilege escalation tools (`sudo`) are removed for this user.

## 4. Usage & Operational Privacy

### Server Administrator Guidelines
*   **Shadow Banning**: Use `/block <nick>` to silently redirect "troll" or suspicious users to the Shadow Room. They will see "Approved by bot" and think they are in, but they will be isolated from your main community.
*   **Persistent Blacklist**: Shadow bans persist across reconnects by IP and Nickname.
*   **Purge command**: Use `/purge` in the admin panel to immediately wipe all message history from RAM.

### Client User Guidelines
*   **Client Connections**: The Go client automatically handles SOCKS5 proxying for `.onion` addresses. No local DNS leaks occur.
*   **Hardened Media**: Use `/upload <path>` for files. The system implements path traversal protection (G305) and decompression bomb protection (G110).
*   **Nicknames**: Do not use identifiable nicknames.

## 5. Security Shield

The CONNER server implements a **Framed Binary Protocol**:
*   **Detection:** It uses big-endian 4-byte length headers. Any frame larger than 50MB is immediately rejected.
*   **Action:** Malicious payloads attempting to overflow buffers or inject shell commands are stopped by the strict Go memory safety and JSON unmarshaling.
*   **Silent TUI:** The server silences raw stdout/stderr output while the TUI is active, preventing "log bleed" that could be captured by terminal-recording malware.
