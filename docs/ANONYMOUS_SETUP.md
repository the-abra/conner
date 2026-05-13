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
 
CONNER is designed for **Zero-Touch Deployment**. 
- **Internal Tor HS**: The application starts its own Tor motor and generates a `.onion` address automatically. 
- **No Reverse Proxy**: Direct connection from Tor to the backend port ensures no extra points of failure or logging.
- **Rootless Operation**: You can run the server as a regular user, which is safer for anonymity (less fingerprinting of the host system).

## 4. Shielded Execution Environment

For maximum security, CONNER should be run within a containerized or isolated environment:

1. **Containerization**: Use the provided Dockerfile. It runs the server as a non-privileged user and mounts critical paths as `tmpfs` (RAM-disk).
2. **Restricted Shell**: If running on bare metal, manually create a restricted shell environment or use `jailkit` to prevent lateral movement if the application is compromised.
3. **No Persistence**: By default, CONNER stores message history in RAM. A simple restart or power loss wipes all evidence of recent communications.

## 5. Panic Switch & Data Destruction

In case of a physical or digital breach, use the `/burn` command from any authorized admin client. 
- **Server-side**: The server should be configured with `shred` to wipe session keys.
- **Client-side**: The `/burn` command wipes the `identity.key` and `downloads/` directory using multiple passes.

## 6. Maintenance

- **Wipe Free Space**: Regularly run `sfill` or `shred` on unused disk space if not running in RAM-only mode.
- **Metadata**: Never share screenshots of the TUI that might contain window titles or system clock information.
