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

## 3. Application Setup & Network Hardening

### NGINX Reverse Proxy (TCP Stream Mode)
CONNER communicates via raw TCP sockets. The proxy passes TCP streams without HTTP headers and keeps no logs.

```nginx
stream {
    server {
        listen 80;
        listen [::]:80;
        proxy_pass 127.0.0.1:6666;
    }
}
```

### Tor Hidden Service
The server should only be accessible via Tor to hide its physical location.

```fortran
# /etc/tor/torrc
HiddenServiceDir /var/lib/tor/conner_chat/
HiddenServicePort 80 127.0.0.1:80
```

## 4. Shielded Execution Environment

CONNER includes a restricted shell (`conner-shell`) to prevent lateral movement if the application is compromised.

1. **Restricted Shell**: Users assigned to `conner-shell` can only run `start-server` and `show-onion`.
2. **Isolation**: The application runs as a non-root `conner` user with no write access to sensitive system directories.

## 5. Panic Switch & Data Destruction

In case of a physical or digital breach, use the `/burn` command from any authorized admin client. 
- **Server-side**: The server should be configured with `shred` to wipe session keys.
- **Client-side**: The `/burn` command wipes the `identity.key` and `downloads/` directory using multiple passes.

## 6. Maintenance

- **Wipe Free Space**: Regularly run `sfill` or `shred` on unused disk space if not running in RAM-only mode.
- **Metadata**: Never share screenshots of the TUI that might contain window titles or system clock information.
