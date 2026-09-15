# CONNER

Terminal hub for **small trusted groups**: rooms, chat, DMs, files.

This is **not** a certified COMSEC product. The threat model in [docs/THREAT_MODEL.md](docs/THREAT_MODEL.md) wins if anything here disagrees.

## What the code actually does

| Layer | Mechanism |
|---|---|
| Join | Protobuf `KeyExchange` / `ClientHello` / `HandshakeOk` (PoW + Ed25519). Major version must be `v3`. |
| Transport | X25519 → HKDF-SHA256 → AES-GCM session to the **hub**. |
| Rooms | Sender keys (HMAC ratchet + AES-GCM). Hub never mints content keys. |
| DMs | Pairwise X25519 + HKDF. Hub relays ciphertext. |
| Files | Chunked AES-GCM on the **same session** (`FILE_PUT`/`GET`). Vault on disk is **ciphertext**. |
| WAN | `--server --tor`: bind `127.0.0.1`, onion invite, **no DNS**. |
| LAN | `--server --lan`: bind `0.0.0.0`. |
| Admin | First member + `/op` persist in `~/.conner/hub-acl.json` across hub restarts. |
| Epoch | Join/leave/kick → `EPOCH` (rotate **sender keys**). Vault token is **stable** for the hub process. |
| At rest | `--passphrase` / `CONNER_PASSPHRASE` wraps identity keys (`*.key.wrap`). |
| Rate limit | 20 chat/DM/file offers per second per connection. |

`--auto-approve` + `--tor` is refused unless `CONNER_I_UNDERSTAND_OPEN_RELAY=1`.

## Data dir

`~/.conner` (override `CONNER_HOME`):

```
identity/     Ed25519 keys, TOFU pins
rooms/<id>/{outbox,inbox}
vault/        hub file blobs (AEAD chunks)
tor/          Tor data
history/      optional encrypted local history
```

## Hub

```bash
./conner --server --tor              # onion; prints conner://v1/... invite
./conner --server --lan --port 6666  # LAN
./conner --server --pt system-tor    # your torrc / Snowflake
./conner --server --no-tui           # logs only
```

## Join

```bash
./conner <nick> 'conner://v1/...'
./conner --tor <nick> <onion>:6666
./conner <nick> 127.0.0.1:6666
```

Wait for admin `/approve` unless returning pinned key or `--auto-approve`.

## TUI

`/room` `/list` `/private` `/files` `/vault` `/fp` `/trust` `/approve` `/kick` `/op` `/burn` `/contrast` `/copy` `/paste` `/help`  
Admins can `/approve` from the **same TUI** (commands are hub `CMD` frames, not E2EE chat).

Room tabs under the title (`[#ops (2)]` = unread). Approve joins immediately (no extra Enter overlay). Scroll-up pauses follow; **Ctrl+G** resumes. Sidebar hides below ~56 columns.
**Copy:** **Ctrl+S** freezes the screen (no redraw) so Shift+drag works; Ctrl+S again resumes. Hub **Ctrl+Y** copies the invite. Client **Ctrl+Y** copies the last chat line. Live redraws otherwise clear terminal selection.
Shortcuts: **F1** help · **Ctrl+Y** copy last line · **Ctrl+V** paste (32k cap, NUL stripped) · **Ctrl+U** clear input · **Ctrl+P** DM selected user · **Ctrl+N** cycle users · **Ctrl+F** files · **Alt+←/→** rooms · **Ctrl+K/L** scroll · **Ctrl+G** latest.
Clipboard uses OSC/atotto, then `wl-copy` / `xclip` / `pbcopy`.
F1 help. Reconnect every 5s with bootstrap %. Unread counts in the title for other rooms.
`CONNER_HIGH_CONTRAST=1` or `/contrast` uses underline/bold instead of color-only state.
Drop files in `~/.conner/rooms/<room>/outbox`.

## Build and test

```bash
# Embedded Tor (needs C compiler)
CGO_ENABLED=1 go build -trimpath -o conner ./cmd/conner

# No CGO: system Tor only (--pt system-tor)
CGO_ENABLED=0 go build -o conner ./cmd/conner

CGO_ENABLED=0 go test ./internal/crypto ./internal/invite ./internal/protocol \
  ./internal/rooms ./internal/appdir ./internal/store ./internal/client \
  ./internal/filesync ./internal/server
```

Docker: `docker build -t conner .` then `docker run --name conner-hub conner --server --tor`  
Image does **not** `go mod tidy` at build time.

## Docs

- [Threat model](docs/THREAT_MODEL.md) — source of truth
- [Architecture](docs/ARCHITECTURE.md)
- [Network flows](docs/NETWORK_FLOWS.md)
- [Building](docs/BUILDING.md)
- [Workflow](docs/WORKFLOW.md)
