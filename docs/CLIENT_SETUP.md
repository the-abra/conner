# CONNER - Client Setup Guide (Alpine Linux / Go)

This guide provides instructions on how to set up and run the CONNER chat client within an Alpine Linux environment.

## 1. Quick Start (Build from Source)

The client is written in Go and requires Go 1.21+ for compilation.

```bash
# Build the binary
go build -o bin/conner-client cmd/client/main.go

# Run it
./bin/conner-client <nickname> [address:port]
```

## 2. Automated Installation (Alpine Linux)

Follow these steps to prepare your Alpine environment with all necessary networking dependencies.

### Step 1: Run the Client Installer
We provide a dedicated script to install Go and Tor, and compile the client binary.

```bash
sudo sh scripts/install-client.sh
```

### Step 2: Run the Client
The installer places the binary in `/usr/local/bin/conner-client`.

```bash
conner-client <YourNick>
```

## 3. Connecting via Tor (Onion Addresses)

CONNER features **Automated SOCKS5 Tor Support**. If you provide a `.onion` address:
1. The client will automatically check if the local Tor service is active.
2. If Tor is not detected, it will attempt to start the Tor daemon (`rc-service tor start`).
3. The client routes all `.onion` traffic through the local SOCKS5 proxy (port 9050) without DNS leaks. **No external tools like proxychains are required.**

Example:
```bash
conner-client abra ltqzlbzusjthhcrpjgtuwnwcskysnwamm2zyvvd2hxweparfx6kd5gyd.onion:80
```

## 4. TUI Features & Shortcuts

| Action | Shortcut / Command |
|:---|:---|
| **Help Menu** | `F1` or type `/help` |
| **Auto-complete** | `Tab` (while typing `/` commands) |
| **Select & Copy** | `Shift + Mouse Drag` |
| **Scroll Chat** | `Mouse Wheel` or `PgUp / PgDn` |
| **Close Menu** | `ESC` or `Enter` (inside help) |
| **Disconnect** | `/quit` or `Ctrl+C` |

## 5. Troubleshooting

- **Error: `lib64-compat` missing**
  - If using a pre-compiled binary on Alpine, ensure `apk add libc6-compat` is installed.
- **Tor Bootstrap Fail**
  - If the automatic start fails, check your connection and start Tor manually: `sudo rc-service tor start`.
- **Connection Refused**
  - Verify the server is running and the port (default 6666) is open.
- **TUI Rendering Issues**
  - Ensure your terminal supports 256 colors: `export TERM=xterm-256color`.
