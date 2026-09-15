# Operational workflow

1. Hub: `conner --server --tor` → copy `conner://v1/...` invite (no DNS).
2. Client: `conner alice '<invite>'`. First member is admin. Others wait. Admin `/approve bob` **in the same TUI** (or hub panel).
3. Chat in `#general`. `/room ops` is a room id on the wire; content is still sender-key encrypted.
4. Files: `~/.conner/rooms/<room>/outbox` → `FILE_PUT` on the chat session. Hub stores ciphertext in `~/.conner/vault`. Peers `FILE_GET`.
   Client **Ctrl+F** / `/vault` opens the vault page (inbox/outbox, sizes, Enter copies path). Hub Shift+Tab → **Vault** tab lists ciphertext blobs.
5. Kick/leave: `EPOCH` resets sender keys; vault token stays valid for existing files.
6. `/burn` wipes `~/.conner` (and leftover cwd `vault`/`uploads`/`downloads`). Best-effort, not a classified wipe.
7. Disconnect: client rebuilds every 5s; TUI shows bootstrap % until handshake completes.
8. High contrast: `CONNER_HIGH_CONTRAST=1` or `/contrast`.
9. Clipboard: Ctrl+Y / `/copy` last visible line; Ctrl+V / `/paste` into the composer (sanitized). Needs a clipboard helper on Linux (`wl-clipboard` or `xclip`).
10. Room tabs sit under the title. Unread is text (`#ops (2)`), not color-only. Scroll-up pauses auto-follow until Ctrl+G.

See THREAT_MODEL.md for what the hub can and cannot read.
