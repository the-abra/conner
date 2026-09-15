# Network flows

## WAN (ops)

Client → SOCKS5 → Tor → hub onion:6666 (**chat + files on the same session**).
File bytes: `FILE_PUT` / `FILE_GET` / `FILE_DATA` (session AEAD). Hub stores length-prefixed ciphertext in `~/.conner/vault`.
Legacy HTTP vault on 127.0.0.1 still exists as a fallback for older clients; WAN ops should not need onion:80.

Invite is `conner://v1/<base64json>` — onion, port, room, optional `pt`. No DNS.

## LAN

`conner --server --lan` binds `0.0.0.0:6666`. Vault remains `127.0.0.1:6667+` so the file HTTP port is not advertised on WAN. Clients on the same host use it; remote LAN file sync still needs a path to that port (limitation: prefer onion for multi-host files).

## Pluggable transports

`--pt system-tor`: CONNER does not start libtor; it uses `127.0.0.1:9050`. Configure Snowflake in *your* `torrc`. `--pt snowflake` prints that instruction and does not pretend to embed Snowflake.
