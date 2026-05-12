# CONNER: Decentralized P2P E2EE Chat

CONNER is a state-of-the-art secure communication system designed for absolute anonymity and resilience. It combines a unified client-server architecture with advanced cryptography and decentralized Tor-based file sharing.

## Key Features

- **True Group E2EE**: Powered by the Double Ratchet (Signal Protocol) and per-client Sender Keys. The server has zero knowledge of chat content.
- **Decentralized P2P File Sharing**: Files are shared directly between clients via Tor Ephemeral Hidden Services. No central relaying of media.
- **Tor-Native**: All traffic is routed through the Tor network by default.
- **Anti-Forensics**: 
    - `/burn` panic switch to wipe local identity and data instantly.
    - RAM-only execution support.
    - Sixel image rendering for inline visual feedback without saving to disk.
- **Stealth Server**: Advanced NGINX reverse proxy setup with no logging and TCP stream isolation.

## Quickstart

### Prerequisites
CONNER requires `tor`. If you are running the server, it also needs `nginx` and `iptables`.

### Build
```bash
CGO_ENABLED=0 go build -o conner ./cmd/conner/main.go
```

### Run Client
```bash
./conner <nickname> [server_onion_address:80]
```

### Run Server
```bash
sudo ./conner --server --stealth
```

## Commands

- `/list`: List online users.
- `/private <user> <msg>`: Send an encrypted private message.
- `/share`: Open the file picker to share a file via P2P.
- `/download <id>`: Download a shared file via Tor.
- `/burn`: Immediate panic wipe of all local data and keys.
- `/help`: Show all available commands.

## Local CI/CD

CONNER uses [`laction`](https://github.com/the-abra/local-actions) for local containerized development pipelines.

- **Build & Vet**: `laction .`
- **Run Tests**: `laction . test`
- **Cross-Platform Release**: `laction . release`
- **Security Audit**: `laction . security`

Documentation for these pipelines can be found in `.laction/` and `docs/BUILDING.md`.

## Manual Docker Env Setup (Alternative)

You can use [dockero](https://github.com/the-abra/dockero) to simplify the manual docker env setup.

```bash
dockero create conner-dev alpine:latest -v .:/workspace

```
at container:
```bash
cd /workspace
./conner alice alice.onion:80
```
to stop container:

```bash
dockero stop conner-dev
```

to restart container

```bash
dockero start conner-dev -c sh
```

to remove container

```bash
dockero remove conner-dev
```

> note: The shared volume is not deleting when you remove the container! To remove it just delete the folder. 

## Documentation

- [Architecture Overview](docs/ARCHITECTURE.md)
- [Building and Deployment](docs/BUILDING.md)
- [Anonymous Setup Guide](docs/ANONYMOUS_SETUP.md)
