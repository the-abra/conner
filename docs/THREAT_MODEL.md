# CONNER threat model (matches the code)

This document is the source of truth. If README and this file disagree, this file wins.

## What CONNER is

A small-team **terminal hub**: onion-first (or LAN) relay with **member-side encryption**.

- **Hub** sees membership, timing, sizes, and ciphertext. It does **not** mint group content keys.
- **Members** encrypt room traffic with **sender keys** (per-sender HMAC ratchet, AES-256-GCM).
- **DMs** use pairwise X25519 → HKDF-SHA256 → AES-GCM. The hub relays ciphertext only.
- **Session** (client↔hub) is X25519 + HKDF + AES-GCM. That is transport privacy to the hub, not E2EE.

## What CONNER is not

- Not Common Criteria / NIAP / “military certified.”
- Not immune to global passive correlation on Tor.
- Not safe if the endpoint is owned.
- Not “zero-knowledge” in the marketing sense: the hub still sees a social graph of connections.

## Trust modes

| Mode | How | Hub bind | Use |
|---|---|---|---|
| **Ops (default WAN)** | `--server --tor` | `127.0.0.1` + onion | Invite blob, no DNS |
| **LAN** | `--server --lan` | `0.0.0.0` | Trusted network only |
| **Lab** | `--auto-approve` | any | **Forbidden** with Tor unless `CONNER_I_UNDERSTAND_OPEN_RELAY=1` |

## Keys

| Key | Who creates it | Who can decrypt content |
|---|---|---|
| Ed25519 identity | Client, `~/.conner/identity/` | N/A (auth / pin) |
| X25519 session | Handshake | Hub + that client (frames) |
| Sender-key seed | Each member | Members who received `SENDER_KEY` |
| DM shared secret | Both DM peers | Those two peers |
| Vault token | Hub (random) | File ACL only, not chat |

On kick/join, hub sends `EPOCH`. Clients **reset sender chains** and republish seeds. A kicked client should not decrypt *new* traffic if members rotated. Missed `SENDER_KEY` frames yield `[waiting for sender key]`.

## Censorship

- Join with `conner://v1/...` or a raw `.onion:port`. **No DNS required.**
- Default WAN is Tor hidden service. Port 6666 on the public internet is **not** the advertised mode.
- `--pt system-tor`: use a Tor daemon you configured (Snowflake, meek, etc.). CONNER does not bundle Snowflake.
- Cover-443 / I2P: not implemented; do not claim them.

## Files

- Per-room dirs: `~/.conner/rooms/<id>/{outbox,inbox}`.
- Offers are sender-key encrypted metadata. Bytes move as `FILE_*` frames on the **chat session** (same onion). HTTP vault is localhost fallback only.
- Vault stores **client AEAD chunks as-is** under `~/.conner/vault`. Clients decrypt with SHA256("conner-file-v1" || capability-token || fileID).
- `EPOCH` rotates **sender keys**. Vault token is issued once at hub start and reused on epoch so join/leave does not invalidate stored files.
- First whitelisted member is granted hub **admin** (`IsAdmin`) so `/approve` works without a chicken-and-egg.

## Panic

`/burn` deletes `~/.conner` (and leftover `vault` / cwd dirs) then exits. This is best-effort, not a classified wipe.

## Residual risks (honest)

- Sender-key distribution is in-band; a delayed joiner needs a republish (epoch).
- Hub can drop, delay, or reorder frames (availability, not confidentiality of new sender-key traffic).
- Identity TOFU: key change blocks **sending** until `/trust <nick>` (out-of-band). Receiving still warns.
- Tor blocking without a PT = you are offline.
- Rate limit is per TCP session (20/s chat-like types); not a global anti-DDoS.
- Hub ACL (whitelist, bans, admins, vault token) lives in `~/.conner/hub-acl.json`.
- Files go through the hub vault only (no second P2P HTTP plane). Directories are not auto-zipped.
