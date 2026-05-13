# Building and Deployment Guide

## Compilation

CONNER is written in Go and requires Go 1.22+.

### Build Binary
To build the unified binary with the embedded Tor motor:
```bash
CGO_ENABLED=1 go build -o conner ./cmd/conner/main.go
```

## System Dependencies

CONNER is designed for **Zero-Dependency** operation. All critical components are embedded.

| Component | Required For | Status |
|-----------|--------------|--------|
| `tor` | Network Anonymity | **Embedded** |
| `coreutils` | Secure deletion (`shred`) | Optional |

### Auto-Setup
The binary includes a minimal `autoSetup` feature that checks for the `shred` tool and warns if it's missing. It runs without root privileges.

## Automated Pipelines (laction)

We use `laction` to run reproducible builds and tests inside Docker containers. This ensures that the environment matches the deployment target.

### Installation
```bash
curl -sSL https://raw.githubusercontent.com/the-abra/local-actions/main/install.sh | bash
```

### Usage
Run these commands from the project root:

| Profile | Command | Description |
|---------|---------|-------------|
| **Default** | `laction .` | Runs `go mod tidy`, `go vet`, and `go build`. |
| **Test** | `laction . test` | Runs tests with the race detector enabled. |
| **Release** | `laction . release` | Builds binaries for Linux, Windows, and macOS. |
| **Security**| `laction . security`| Runs `gosec` to identify potential vulnerabilities. |

The configuration for these profiles is located in `laction.ini` and the scripts reside in `.laction/`.

## Docker Deployment

The recommended way to deploy a CONNER server is via Docker:

1. **Build Image**:
   ```bash
   docker build -t conner:latest .
   ```

2. **Run Server**:
   ```bash
   docker run -d \
     --name conner-server \
     --cap-add=NET_ADMIN \
     conner:latest --server --tor
   ```

Note: `--cap-add=NET_ADMIN` is required for `iptables` to function correctly within the container.

## Platform Specifics

### Alpine Linux
Optimized for memory-only execution. See `docs/ANONYMOUS_SETUP.md` for the ultimate stealth configuration.

### Debian / Ubuntu
Dependencies are managed via `apt`. The auto-setup script will handle the installation of `libsixel-bin` for image support.

### Arch Linux
Dependencies are managed via `pacman`.
