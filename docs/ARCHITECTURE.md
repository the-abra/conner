# CONNER Architecture

This document describes the internal workings and cryptographic design of the CONNER communication system.

## 1. Cryptographic Protocol

### Double Ratchet & Sender Keys
CONNER implements a modified version of the Signal Protocol for group messaging:
- **Initialization**: Upon joining a group, clients perform a 1-to-1 X25519 DH exchange with every other member to establish a secure initial shared secret.
- **Sender Keys**: Every client generates their own symmetric "Sender Key". This key is ratcheted forward using a symmetric KDF (Key Derivation Function) for every message sent (Forward Secrecy).
- **Encryption**: Messages are encrypted using AES-256-GCM.
- **Deniability**: By using symmetric authentication (GCM tags) rather than asymmetric signatures for individual messages, CONNER provides plausible deniability.

## 2. P2P File Sharing via Tor

Unlike traditional systems that relay files through a central server, CONNER uses **Ephemeral Tor Hidden Services**:
1. **Host**: When a user selects a file to share, the client starts a local ephemeral HTTP server.
2. **Onion**: The client connects to the local Tor Control Port (9051) and requests an `ADD_ONION` ephemeral service.
3. **Broadcast**: The client broadcasts the `.onion` address and a unique 16-byte random token to the group over the E2EE channel.
4. **Fetch**: Recipients connect to the `.onion` address via their local Tor SOCKS5 proxy, providing the token in the `Authorization` header.

## 3. Binary Protocol (Protobuf)

All communication between the client and server (and P2P) uses a structured binary protocol defined in `internal/protocol/conner.proto`:
- **Message Types**: Differentiated by specific enum values (e.g., `MsgTypeChat`, `MsgTypeMediaInfo`, `MsgTypeKeyExchange`).
- **Framing**: Each message is length-prefixed for robust streaming over TCP.

## 4. Server Design (Zero-Knowledge)

The CONNER server acts as a **dumb relay**:
- It maintains a list of online users and their associated public identity keys.
- It forwards encrypted blobs between clients.
- It cannot decrypt any chat content or file metadata, as all cryptographic keys are managed exclusively at the edges (clients).
- In `--stealth` mode, it uses NGINX in `stream` mode to isolate the TCP traffic and strip all identifiable headers.
