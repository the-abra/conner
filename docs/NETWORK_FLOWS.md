# CONNER Network Flows: Tor vs. Direct Mode

This document outlines the two distinct network architectures supported by CONNER. Regardless of the mode, all messaging and file transfers are protected by **AES-256-GCM End-to-End Encryption (E2EE)**.

---

## 🛰️ 1. Tor Mode (`--tor`)
Designed for **maximum anonymity**, metadata protection, and NAT traversal (no port forwarding required).

### A. Server Workflow
1. **Tor Initialization**: The server starts an embedded Tor motor (or connects to a system-wide Tor daemon).
2. **Onion Creation**: It requests a multi-port hidden service from the Tor controller:
   - **External Port 6666** → Maps to Internal Chat Port (TCP).
   - **External Port 80** → Maps to Internal File Server Port (HTTP).
3. **Address Generation**: A unique `.onion` address is generated (e.g., `abcdef...xyz.onion`).
4. **Ready State**: The server waits for clients to connect via the Tor network.

### B. Client Workflow
1. **Proxy Setup**: The client starts a local Tor SOCKS5 proxy (`127.0.0.1:9050`).
2. **Handshake**: The client connects to the `.onion:6666` address *through* the SOCKS5 proxy.
3. **File Sync (P2P Hybrid)**: 
   - **Upload**: Files are sent to `http://[onion]:80/upload` or via ephemeral P2P Onion services.
   - **Download**: Files are fetched from `http://[onion]:80/download` or directly from peers.
4. **Anonymity**: Neither the server nor the clients know each other's real IP addresses.
5. **Reconnection**: If the Tor circuit collapses, the client automatically rotates its local SOCKS proxy and attempts to reconnect every 5s until successful.

---

## ⚡ 2. Direct Mode (Default)
Designed for **maximum speed**, local area networks (LAN), or scenarios where anonymity is not the primary concern.

### A. Server Workflow
1. **TCP Bind**: The server listens directly on `0.0.0.0:6666`.
2. **Dynamic File Server**: Starts the internal HTTP file server (Vault) by searching for an available port starting from `6667`.
3. **Address Reporting**: The server informs clients of its assigned Vault port during the initial handshake.
4. **Ready State**: The server accepts direct TCP connections.

### B. Client Workflow
1. **Direct Connect**: The client opens a raw TCP socket to the `IP:6666`.
2. **No Proxy**: Traffic flows directly over the standard internet/local network stack.
3. **File Sync**:
   - **Upload**: Files are sent directly to the server's Vault on its dynamic port (e.g., `:6667`).
   - **Download**: Files are fetched from the Vault and saved to `./downloads/`.
4. **Performance**: Significantly lower latency compared to Tor, but your IP address is visible to the server.
5. **Auto-Approval**: If the server is in `--auto-approve` mode, the client is whitelisted immediately upon connection.

---

## 🛠️ Comparison Table

| Feature | Tor Mode (`--tor`) | Direct Mode |
| :--- | :--- | :--- |
| **Privacy** | Absolute (IP Hidden) | Minimal (IP Visible) |
| **Connectivity** | Works behind NAT/Firewall | May require Port Forwarding |
| **Latency** | High (Multi-hop) | Low (Direct) |
| **File Sync Port** | 80 (Onion mapped) | Dynamic (6667+) |
| **Encryption** | Onion + E2EE | Raw TCP + E2EE |
| **Setup** | Automated (Embedded Tor) | Instant |

---

## 🔐 Shared Security Layer (E2EE)
Regardless of the flow chosen above, CONNER applies a mandatory E2EE layer:
1. **Session Key**: Every connection has a unique ChaCha20-Poly1305 key for transport security.
2. **Room Key**: All group messages and file metadata are encrypted with a shared AES-256-GCM key that the server never sees.
3. **Identity**: Messages are signed with Ed25519 keys to prevent person-in-the-middle attacks.
