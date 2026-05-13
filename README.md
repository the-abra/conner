# 🛡️ CONNER: Zero-Knowledge Messaging Relay

CONNER is a high-speed, minimalist messaging platform designed for absolute privacy. It combines a centralized relay architecture with zero-knowledge E2EE (End-to-End Encryption), operating seamlessly over Tor or direct TCP connections.

## 🚀 Key Features

- **Blind Postman Relay**: The server relays encrypted data without ever having access to the decryption keys. Room keys are generated and distributed, then immediately wiped from the server's memory.
- **Pure Messaging**: Stripped of all filesystem overhead for maximum speed and reliability over high-latency networks (Tor).
- **Always-On Stability**: Integrated heartbeat system keeps Tor circuits alive and connections stable.
- **Dynamic Rotation**: Security keys are automatically rotated every time a member joins or leaves the group.
- **Identity Verification**: Built-in Ed25519 identity keys ensure that you are always talking to who you think you are.

## 🛠️ Usage

CONNER supports two modes of operation: **Tor Mode** (for anonymity) and **Direct Mode** (for local/speed).

### Server Mode
```bash
# Direct Mode (Listens on 0.0.0.0:6666)
./conner --server

# Tor Mode (Generates a .onion address)
./conner --server --tor
```

### Client Mode
```bash
# Direct Mode
./conner <nickname> <ip_address>:6666

# Tor Mode
./conner --tor <nickname> <onion_address>:6666
```

## ⌨️ Commands

- `/list`: See who is currently online.
- `/private <nick> <msg>`: Send an encrypted private message.
- `/burn`: Panic switch: immediately wipe your identity keys and exit.
- `/help`: Show all available commands.
- `/quit`: Disconnect and exit.

---
*KISS: Keep It Simple, Stupid. Security shouldn't be complex.*

### Prerequisites
- Go 1.18+
- C Compiler (for embedded Tor)
- Optional: `shred` (for secure data wiping).

### Build
```bash
CGO_ENABLED=1 go build -o conner ./cmd/conner/main.go
```

## 🔐 Security Architecture

CONNER uses a layered security approach:
1. **Transport**: TLS-like handshake over Tor (Onion) or raw TCP.
2. **Session**: ChaCha20-Poly1305 encryption for all traffic using a session-derived key.
3. **Content**: AES-256-GCM encryption for group chat messages using a rotating Room Key.
4. **Identity**: Ed25519 signatures for verifying message origin.

## 🛠️ Development & CI/CD

CONNER uses [`laction`](https://github.com/the-abra/local-actions) for local containerized development pipelines.

- **Build & Vet**: `laction .`
- **Run Tests**: `laction . test`
- **Security Audit**: `laction . security`

## 📚 Documentation

- [Architecture Overview](docs/ARCHITECTURE.md)
- [Building and Deployment](docs/BUILDING.md)
- [Anonymous Setup Guide](docs/ANONYMOUS_SETUP.md)
