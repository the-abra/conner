# CONNER Architecture

This document describes the internal workings and cryptographic design of the CONNER messaging system.

## 1. Cryptographic Protocol

CONNER utilizes a multi-layered encryption approach to ensure Zero-Knowledge privacy:

- **Session Encryption**: Upon connection, the client and server perform an initial handshake to establish a shared session key. All traffic is encrypted using **ChaCha20-Poly1305**.
- **Double Ratchet E2EE**: Messaging within a room is protected by a rotating key system (Ratchet). Each message or session event triggers a key rotation, ensuring **Perfect Forward Secrecy (PFS)**.
- **Message Integrity**: All chat content is encrypted with the current Ratchet Key (**AES-256-GCM**) before being sent to the relay. The server never has access to these keys.
- **Identity Pinning**: Clients use **Ed25519** signing keys to authenticate messages. On first connection, the server pins the client's public key to their nickname, preventing impersonation and MITM attacks.

## 2. Relay & Communication

CONNER operates as a **Blind Postman Relay**:
- **Protobuf Binary Protocol**: Unlike traditional JSON-based relays, CONNER uses a high-efficiency **Protocol Buffers** (binary) serialization format. This reduces overhead and improves reliability over high-latency Tor circuits.
- **Metadata-Only Awareness**: The server knows who is connected but has no access to the content of the communication.
- **Heartbeat System**: Integrated PING/PONG heartbeats (every 30s) maintain active Tor circuits and prevent timeouts.
- **Auto-Approval System**: The server supports an `--auto-approve` mode where the connection-approval handshake is automated, allowing trusted or private networks to bypass the manual `/approve` admin requirement.

## 3. Hybrid File Sharing Architecture

The system supports two distinct file-sharing pathways depending on the transport:

### A. P2P Tor Sharing (Anonymity First)
In Tor mode, when a user shares a file, the client starts a temporary **ephemeral Onion Service** on a random port.
1. The sender broadcasts a `MsgTypeShare` containing the ephemeral Onion address and a one-time token.
2. The receiver connects directly to the sender's Onion service via Tor.
3. The file is transferred P2P, bypassing the central relay server entirely.

### B. Server Vaulting (Speed First)
In Direct mode, files are uploaded to the relay server's ephemeral Vault.
1. **Dynamic Port Allocation**: The server automatically finds an available port for the Vault (starting from 6667) and communicates this to all clients during the handshake.
2. The sender encrypts the file and uploads it to the server's Vault.
3. The server provides a File ID.
4. Receivers download the encrypted chunks from the server and decrypt them locally into the `./downloads/` folder.

## 4. Connectivity & Stealth

- **Embedded Tor Engine**: A C-compiled Tor motor is embedded within the Go binary, eliminating the need for system-wide Tor configuration.
- **Connectivity Engine**: A background goroutine monitors the connection state. If a disconnect is detected (e.g., due to a Tor circuit reset), the client automatically enters an exponential backoff retry loop to re-establish the session without user intervention.
- **Stealth Module**: When activated (`--stealth`), the system applies anti-forensic measures:
    - Securely wipes system logs and bash history.
    - Mounts sensitive directories as `tmpfs` (RAM-disk) where possible.
    - Enables kernel-level protection for process isolation.

## 5. Panic Switch

The `/burn` command provides a quick way to:
1. Securely wipe the local `identity.key`, `uploads/`, and `downloads/` directories using **shred** (multiple overwrite passes).
2. Clear any cached ephemeral file metadata and session logs.
3. Terminate the process immediately, leaving no forensic trace of the user's identity.
