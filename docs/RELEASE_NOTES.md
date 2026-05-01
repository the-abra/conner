# CONNER Release Notes — V1.1.0 (The Modernization Update)

This release marks the completion of the modernization and stabilization phase for the CONNER TUI chat system. We have focused on identity security, network resilience, and architectural cleanliness while maintaining the system's ephemeral nature.

## 🛡️ Security & Identity
- **Cryptographic Identity**: Replaced fragile directory-based identity with **Ed25519 Signing Keys**. Each client has a persistent `identity.key` used for secure handshakes.
- **Challenge-Response Handshake**: Implemented a 6-part cryptographic handshake. The server issues a unique nonce (challenge) that the client must sign, preventing identity spoofing.
- **Access Denied Modal**: Users who are banned now see a clear, high-contrast modal explaining their status rather than a cryptic connection error.
- **Security Hardening**: Fixed multiple `gosec` findings, including path traversal protections and race conditions in directory walking.

## 📡 Network & Reliability
- **Ephemeral RAM-Only State**: Optimized the server to run entirely in RAM. All chat history, whitelists, and blocklists are volatile and wiped upon server restart, ensuring zero disk trace.
- **Auto-Reconnect with Backoff**: The client now automatically recovers from network drops using an exponential backoff strategy (1s, 2s, 4s... up to 30s).
- **Heartbeat Protocol**: Implemented an application-layer Ping/Pong mechanism to prune zombie connections and maintain real-time status.
- **Gzip Compression**: Integrated Gzip for all media transfers, significantly reducing bandwidth for shared archives.

## 📎 File Sharing Overhaul
- **Recursive Directory Sharing**: Replaced the directory watcher with an explicit `/upload <path>` system. Folders are automatically converted into compressed Tar archives.
- **Safe Extraction**: Downloaded files are extracted to a dedicated `./downloads/` directory with strict path-traversal verification.
- **Transfer Indicators**: Added `⏳ uploading…` and `⏳ downloading…` status indicators in the TUI title bar for immediate feedback.

## 🎨 TUI & User Experience
- **Modern Dark Theme**: Implementation of a sleek Dark Theme with high-contrast **White Accents**.
- **Admin Command Registry**: Refactored command handling into a modular registry. Admin menus now dynamically update based on permission levels.
- **Stability Enhancements**: 
    - Implemented a 1,000-line sliding window for chat history to prevent memory leaks.
    - Added defensive `nil` checks and improved error reporting in the console.

## 🛠 Architectural Changes
- **Modular Registry Pattern**: Shifted from monolithic switch blocks to a clean, registry-based command handler system.
- **Pure Go Dependencies**: Standardized on pure Go libraries for maximum portability across Linux/Unix systems.

---
*CONNER: Secure, Ephemeral, and Resilient Chat.*
