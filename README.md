#  CONNER: Zero-Knowledge Messaging & Sync Relay

CONNER is a high-speed, minimalist messaging platform designed for absolute privacy. It combines a centralized relay architecture with zero-knowledge E2EE (End-to-End Encryption) and **Automatic File Synchronization**, operating seamlessly over Tor or direct TCP connections.

##  Key Features

- **Blind Postman Relay**: The server relays encrypted data without ever having access to the decryption keys.
- **Smart Sync**: Files shared by you go to `./uploads/`, and files received from others go to `./downloads/`. Full directory synchronization supported.
- **Resilient Connectivity**: Automatic background reconnection engine handles network drops and Tor circuit resets seamlessly.
- **Zero-Touch Anonymity**: Integrated Tor engine handles everything. No manual proxy configuration needed.
- **Auto-Approval**: Server can be set to automatically whitelist new connections for frictionless onboarding.
- **Identity Verification**: Ed25519 identity keys ensure you are always talking to verified members.

##  Usage

CONNER supports two modes of operation: **Tor Mode** (for anonymity) and **Direct Mode** (for local speed).

### Server Mode
```bash
# Direct Mode (Listens on 0.0.0.0:6666)
./conner --server

# Tor Mode (Generates a .onion address)
./conner --server --tor

# Auto-Approve Mode (No manual admin approval required)
./conner --server --auto-approve
```

### Client Mode
```bash
# Direct Mode
./conner <nickname> <ip_address>:6666

# Tor Mode (Auto-syncs via Tor)
./conner --tor <nickname> <onion_address>:6666
```

## Commands

- `/list`: See who is currently online.
- `/private <nick> <msg>`: Send an encrypted private message.
- `/burn`: Panic switch: securely wipes identity keys, history, uploads, and downloads before exit.
- `/help`: Show all available commands (F1 also works).
- `/quit`: Disconnect and exit.

###  Automated File Synchronization
CONNER features a zero-configuration background sync engine:
1. **Drop it**: Move any file or folder into your local `./uploads/` directory.
2. **Sync it**: The system automatically detects changes and chooses the best path:
   - **Tor Mode**: Uses secure **P2P transfers** with **Hash Handshaking**. Files flow directly between peers; the server only handles metadata. 🛰️
   - **Direct Mode**: Relays via the server's ephemeral **Vault** with dynamic port allocation. ⚡
3. **Receive it**: Files are automatically "installed" on all other clients in their local `./downloads/` folder.

---
*KISS: Keep It Simple, Stupid. Security shouldn't be complex.*

### Build
```bash
CGO_ENABLED=1 go build -o conner ./cmd/conner/main.go
```

##  Documentation

- [Architecture Overview](docs/ARCHITECTURE.md)
- [Network Flows (Tor vs. Direct)](docs/NETWORK_FLOWS.md)
- [Building and Deployment](docs/BUILDING.md)
- [Anonymous Setup Guide](docs/ANONYMOUS_SETUP.md)
