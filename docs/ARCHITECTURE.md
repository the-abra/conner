# CONNER architecture (code-aligned)

## Topology

Star hub. One TCP session per client (Tor SOCKS → onion, or LAN). Frames: 4-byte big-endian length + payload (max 8 MiB). After handshake, payloads are `base64(AES-GCM(session, protobuf ChatMessage))`.

## Handshake

1. Hub: protobuf `KeyExchange` (X25519 pub, nonce, PoW bits, version)
2. Client: PoW + Ed25519(sign nonce) in `ClientHello` (+ E2E X25519 pub, version)
3. Hub: tagged `HandshakeOk` (vault port, approved, version) or `HandshakeErr`

Session key = HKDF-SHA256(X25519(shared), `"conner-session-v1"`).  
`CompatibleVersion` requires the same major (`v3`). Empty version is rejected. Nickname 1–32 chars.

## Content

- Rooms: `ChatMessage.OnionAddr` is the room id (default `general`).
- Group chat / file offers: sender-key ratchet (`RatchetStep`, `IsE2Ee`). Seeds: `SENDER_KEY`. Missed keys queue ciphertext and flush on seed.
- DMs: `PRIVATE` to peer X25519 from user list `nick|ed25519|x25519`.
- `EPOCH` on membership change: **sender keys reset**; vault token in the payload is the **stable** capability token (not rotated on join).
- First `WHITELISTED` member gets `IsAdmin` (persisted).
- Hub commands use `CMD` (session-encrypted only, **not** sender-key). Client TUI `/approve` `/kick` `/op` `/block`.
- Rate limit: 20 chat/private/file-offer messages per second.

## Files

Primary file plane: **session mux** (`FILE_PUT`/`FILE_GET`/`FILE_DATA`) on the chat TCP/onion. Hub stores length-prefixed AEAD chunks under `~/.conner/vault`. HTTP vault on **127.0.0.1** remains as fallback; not required for WAN. Identity keys wrap at rest when `CONNER_PASSPHRASE` / `--passphrase` is set (`identity_<nick>.key.wrap`). Outbound overflow → `~/.conner/queue/<nick>.ndjson`.

## Tor

Embedded go-libtor (`--tor`, CGO) or `--pt system-tor`. Invite: `conner://v1/<base64url json>`.

## Client UX

- Outbox queue 256 frames (survives short disconnects until the old connection's writePump dies).
- Room switcher: title bar lists rooms; `(N unread)` is text, not color-only.
- `/contrast` + `CONNER_HIGH_CONTRAST=1`.
- Shortcuts: Ctrl+Y copy last line, Ctrl+V paste (32k, NULs stripped), Alt+arrows rooms, Ctrl+P DM, Ctrl+F files. Clipboard: atotto then wl-copy/xclip/pbcopy (`internal/clipx`).
- `/burn` → `appdir.Burn()`.

## CI

GitHub Actions: `CGO_ENABLED=0` vet/build/test; optional `-race` on crypto/invite/protocol/rooms/store/appdir.
