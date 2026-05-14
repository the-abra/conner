# Conner Project Technical Workflows

This document provides a detailed map of data flows, functions, and variables involved in core operations of the Conner application.

---

## 1. File Upload & Auto-Sync Workflow

When a file or folder is placed in the `./uploads/` directory, the following sequence occurs:

### Detection & Filtering
1.  **Scanner**: `StartAutoSync()` in `internal/client/core.go` runs a ticker every 5 seconds.
2.  **Discovery**: `os.ReadDir("uploads")` identifies new items.
3.  **Deduplication**: The client checks `c.syncLedger` (a `map[string]time.Time`) using `c.isSynced()`. If the file's `ModTime` is not newer than the last recorded sync, it is ignored.

### Preparation
4.  **Compression**: If the item is a directory, `CreateZip()` archives it into `.conner_data/temp_shares/`.
5.  **Path Selection**: The `targetPath` variable is set to the final file to be sent.

### Routing (Based on Connection Mode)
*   **Direct Mode (Server Vault)**:
    *   `c.UploadToServer(path)` is called.
    *   Data flows via **HTTP POST** to `http://[server]/upload`.
    *   The server's `/upload` handler (`internal/server/core.go`) saves the file to the `vault/` directory.
*   **Tor Mode (P2P Share)**:
    *   `c.p2p.AddFile(fileID, path)` registers the file in the local P2P server.
    *   The file is served directly from the client's onion address.

### Announcement
6.  **Broadcast**: A `MsgTypeFileOffer` message is created via `protocol.CreateMessage()`.
7.  **Metadata**: The content contains `filename | onion_address | fileID | checksum`.
8.  **Transmission**: The message is sent to `c.SendChan` to notify all peers in the room.

---

## 2. File Receive & Ingestion Workflow

When a client receives a `MsgTypeFileOffer` from the network:

1.  **Parsing**: The message is decrypted using the `RoomKey` and split into its metadata parts.
2.  **Validation**: `c.isSeen()` and `c.isDownloading()` check if the file is already present or being processed.
3.  **Download Execution**:
    *   If `fileID == "VAULT"`: `c.DownloadSharedFile()` fetches it from the server's HTTP endpoint.
    *   Otherwise: `c.DownloadP2PFile()` fetches it directly from the sender's onion address.
4.  **Integrity Check**: If a checksum was provided, the client calculates the SHA256 of the downloaded file. If it mismatches, the file is deleted immediately.
5.  **Extraction**: If the file is a `.zip`, `ExtractZip()` runs automatically to restore the folder structure.
6.  **Finalization**: `c.markSynced()` updates the ledger to prevent re-uploading the received file.

---

## 3. Server Startup (`--server --tor`) Workflow

Executing `./conner --server --tor` initiates a multi-layered hosting environment:

1.  **Tor Initialization**:
    *   `startTor()` checks for a system Tor installation.
    *   If missing, `tor.StartEmbedded()` launches a built-in Tor process using `go-libtor`.
2.  **Core Services**:
    *   A **TCP Server** starts on port 6666 for chat traffic.
    *   An **HTTP Server** starts on a dynamic port (e.g., 6667) for Vault file transfers.
3.  **Onion Mapping**:
    *   `CreateServerOnion()` requests a new hidden service from the Tor controller.
    *   Port mapping: `Remote 6666 -> Local 6666 (TCP)` and `Remote 80 -> Local [Dynamic] (HTTP)`.
4.  **Admin UI**:
    *   The Bubble Tea TUI launches, providing the dashboard.
    *   `sysmon.Collect()` begins gathering CPU, RAM, and Network metrics for the display.

---

## 4. Messaging System (End-to-End Flow)

### Outbound (Sending)
1.  **Input**: The user types in the TUI; the string is sent to `cli.SendChan`.
2.  **Encryption**: `writePump` encrypts the text using **AES-256-GCM** with the `RoomKey`.
3.  **Signature**: The encrypted blob is signed using the client's **Ed25519 Private Identity Key**.
4.  **Framing**: The `ChatMessage` protobuf is serialized, prefixed with a length header, and written to the `net.Conn`.

### Inbound (Receiving)
1.  **Read**: `readPump` reads the frame and deserializes the protobuf.
2.  **Verification**: The sender's signature is verified against their stored **Public Identity Key**.
3.  **Decryption**: The content is decrypted using the `RoomKey`.
4.  **UI Update**: The decrypted text is sent to `c.UpdateChan` for terminal rendering.

### Privacy Layers
*   **RoomKey**: Protects message content from the server and outsiders.
*   **IdentityKey**: Ensures authenticity and prevents impersonation.
*   **SessionKey**: Used for `/private` messages, ensuring only the two participants can read the content (not even other room members).
