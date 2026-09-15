package client

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/proxy"

	"conner/internal/appdir"
	"conner/internal/config"
	"conner/internal/crypto"
	"conner/internal/filesync"
	"conner/internal/protocol"
	"conner/internal/tor"

	"google.golang.org/protobuf/proto"
)

var ErrBanned = fmt.Errorf("You are banned from this server")

// isTorRunning probes the SOCKS5 port without sending any data.
func isTorRunning() bool {
	return socksOpen(config.TorSocksAddr)
}

func socksOpen(addr string) bool {
	c, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

func ensureTorRunningAt(socks string) error {
	if socksOpen(socks) {
		return nil
	}
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(2 * time.Second)
		if socksOpen(socks) {
			return nil
		}
	}
	return fmt.Errorf("Tor SOCKS5 %s not up after 60s (embedded CGO binary, or --pt system-tor with tor running)", socks)
}

func ensureTorRunning() error {
	return ensureTorRunningAt(config.TorSocksAddr)
}

type Client struct {
	Conn       net.Conn
	SessionKey []byte
	Nickname   string
	Messages   []string
	SendChan   chan *protocol.ChatMessage
	p2p        *P2PService // P2P Service for Tor mode
	mu         sync.RWMutex
	UpdateChan chan *protocol.ChatMessage // typed messages for rich TUI rendering
	// Security items
	SigningPriv     []byte            // Ed25519 Private Key for identity
	SigningPub      []byte            // Ed25519 Public Key for identity
	UserKeys        map[string][]byte // Other users' Identity public keys
	IdentityStore   *IdentityStore
	RoomKey         []byte // unused in secure mode; kept for vault token hash compat
	VaultToken      string // capability token from hub (not a content key)
	E2EPriv         []byte
	E2EPub          []byte
	PeerE2E         map[string][]byte
	Senders         *crypto.SenderState
	CurrentRoom     string
	outboxDir       string
	inboxDir        string
	ServerOnion     string // Onion address of the hub
	ServerHTTPPort  int    // Port for Direct mode file transfers
	UseTor          bool
	SocksAddr       string
	syncLedger      map[string]time.Time // filename -> last sync time
	activeDownloads map[string]bool
	pendingDecrypt  []*protocol.ChatMessage
	queue           *DiskQueue
	fileGets        map[string]chan *protocol.ChatMessage
	Ctx             context.Context
	Cancel          context.CancelFunc
	autoSyncOnce    sync.Once
}

func loadIdentityKeys(nick string) (pub []byte, priv []byte) {
	keyFile := appdir.IdentityKey(nick)
	wrapFile := appdir.IdentityKeyWrap(nick)
	pass := os.Getenv("CONNER_PASSPHRASE")
	if data, err := os.ReadFile(keyFile); err == nil && len(data) == 64 {
		if pass != "" {
			_ = crypto.WriteWrapped(wrapFile, pass, data)
			_ = os.Remove(keyFile)
		}
		return data[32:], data[:64]
	}
	if pass != "" {
		if data, err := crypto.ReadWrapped(wrapFile, pass); err == nil && len(data) == 64 {
			return data[32:], data[:64]
		}
	}
	p, s, err := crypto.GenerateSigningKeyPair()
	if err != nil {
		return nil, nil
	}
	_ = os.MkdirAll(filepath.Dir(keyFile), 0700)
	if pass != "" {
		_ = crypto.WriteWrapped(wrapFile, pass, s)
	} else {
		_ = os.WriteFile(keyFile, s, 0600)
	}
	return p, s
}

func identityStorePath(nick string) string {
	return appdir.IdentityStore(nick)
}

func Connect(nickname, address string, useTor bool, et *tor.EmbeddedTor) (*Client, error) {
	var conn net.Conn
	var err error

	if useTor {
		socks := config.TorSocksAddr
		if et != nil && et.SocksAddr != "" {
			socks = et.SocksAddr
		}
		if torErr := ensureTorRunningAt(socks); torErr != nil {
			return nil, torErr
		}
		dialer, dialErr := proxy.SOCKS5("tcp", socks, nil, proxy.Direct)
		if dialErr != nil {
			return nil, fmt.Errorf("failed to create Tor SOCKS5 dialer: %w", dialErr)
		}
		conn, err = dialer.Dial("tcp", address)
	} else {
		conn, err = net.Dial("tcp", address)
	}

	if err != nil {
		return nil, fmt.Errorf("connection failed: %w", err)
	}
	if conn == nil {
		return nil, fmt.Errorf("connection is nil after dial (unexpected)")
	}

	// ── Handshake ──────────────────────────────────────────────────────────

	// 1. Receive KEY_EXCHANGE from server
	payload, err := protocol.ReadFrame(conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("handshake: read failed: %w", err)
	}

	ke, err := protocol.UnmarshalKE(payload)
	if err != nil || len(ke.GetX25519Pub()) == 0 || len(ke.GetNonce()) == 0 {
		conn.Close()
		return nil, fmt.Errorf("handshake: invalid key exchange")
	}
	serverPub := ke.GetX25519Pub()
	nonce := ke.GetNonce()
	difficulty := int(ke.GetPowDifficulty())
	if difficulty == 0 {
		difficulty = crypto.PoWDifficulty
	}
	powNonce := crypto.ComputePoW(nonce, difficulty)

	priv, pub, kerr := crypto.GenerateKeyPair()
	if kerr != nil {
		conn.Close()
		return nil, kerr
	}
	sessionKey, kerr := crypto.DeriveSharedKey(priv, serverPub)
	if kerr != nil {
		conn.Close()
		return nil, kerr
	}
	e2ePriv, e2ePub, _ := crypto.GenerateKeyPair()

	// 2. Sign Challenge
	idPub, idPriv := loadIdentityKeys(nickname)
	sig := crypto.Sign(idPriv, nonce)

	// 2. Send CLIENT_HELLO
	identity := "unknown"
	if b, err := os.ReadFile("hostname"); err == nil {
		identity = strings.TrimSpace(string(b))
	} else if b, err := os.ReadFile("/var/lib/tor/conner_chat/hostname"); err == nil {
		identity = strings.TrimSpace(string(b))
	} else {
		identity = strings.Split(conn.LocalAddr().String(), ":")[0]
	}

	hello := &protocol.ClientHello{
		X25519Pub:  pub,
		Nickname:   nickname,
		Identity:   identity,
		SigningPub: idPub,
		Signature:  sig,
		PowNonce:   powNonce,
		E2EPub:     e2ePub,
		Version:    config.Version,
	}
	helloBytes, err := protocol.Marshal(hello)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if err := protocol.SendFrame(conn, helloBytes); err != nil {
		return nil, fmt.Errorf("handshake: CLIENT_HELLO send failed: %w", err)
	}

	respPayload, err := protocol.ReadFrame(conn)
	if err != nil {
		return nil, fmt.Errorf("handshake: response read failed: %w", err)
	}
	if herr, err := protocol.UnmarshalErr(respPayload); err == nil {
		conn.Close()
		if strings.Contains(strings.ToLower(herr.GetReason()), "ban") {
			return nil, ErrBanned
		}
		return nil, fmt.Errorf("server error: %s", herr.GetReason())
	}
	okMsg, err := protocol.UnmarshalOK(respPayload)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("handshake: expected HandshakeOk")
	}
	srvHTTPPort := int(okMsg.GetVaultPort())

	// 1. Initialize P2P Service once
	client := &Client{
		Conn:            conn,
		SessionKey:      sessionKey,
		Messages:        []string{},
		Nickname:        nickname,
		SendChan:        make(chan *protocol.ChatMessage, 256),
		UpdateChan:      make(chan *protocol.ChatMessage, 200),
		SigningPub:      idPub,
		SigningPriv:     idPriv,
		UserKeys:        make(map[string][]byte),
		PeerE2E:         make(map[string][]byte),
		E2EPriv:         e2ePriv,
		E2EPub:          e2ePub,
		CurrentRoom:     config.DefaultRoom,
		IdentityStore:   NewIdentityStore(identityStorePath(nickname)),
		ServerOnion:     strings.Split(address, ":")[0],
		ServerHTTPPort:  srvHTTPPort,
		UseTor:          useTor,
		SocksAddr:       config.TorSocksAddr,
		syncLedger:      make(map[string]time.Time),
		activeDownloads: make(map[string]bool),
		pendingDecrypt:  nil,
		queue:           NewDiskQueue(nickname),
		fileGets:        make(map[string]chan *protocol.ChatMessage),
	}
	client.SetRoomDirs(config.DefaultRoom)
	seed := crypto.GenerateRandomKey()
	client.Senders = crypto.NewSenderState(nickname, 1, seed)
	client.Ctx, client.Cancel = context.WithCancel(context.Background())

	if et != nil {
		client.SocksAddr = et.SocksAddr
	}

	go client.readPump()
	go client.writePump()
	go client.flushQueue()
	go func() {
		time.Sleep(150 * time.Millisecond)
		client.publishSenderKey()
		time.Sleep(150 * time.Millisecond)
		client.publishSenderKey()
	}()

	// 5. Heartbeat loop (30s) to keep Tor connection alive
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-client.Ctx.Done():
				return
			case <-ticker.C:
				ping := &protocol.ChatMessage{
					Type:   config.MsgTypePing,
					Sender: client.Nickname,
				}
				client.SendChan <- ping
			}
		}
	}()

	return client, nil
}

func (c *Client) readPump() {
	defer func() {
		// Always notify the TUI when the connection ends.
		c.sendUpdate(&protocol.ChatMessage{
			Type:    config.MsgTypeSystem,
			Sender:  "SERVER",
			Content: "❌ Connection closed by server.",
		})
		// 6. Final cleanup on close
		c.Cancel()
	}()

	for {
		payload, err := protocol.ReadFrame(c.Conn)
		if err != nil {
			break
		}

		raw := string(payload)

		// Handle server error frames (e.g. "ERROR:nickname already taken")
		if strings.HasPrefix(raw, "ERROR:") {
			c.sendUpdate(&protocol.ChatMessage{
				Type:    config.MsgTypeSystem,
				Sender:  "SERVER",
				Content: "⚠️  " + strings.TrimPrefix(raw, "ERROR:"),
			})
			break
		}

		enc, err := crypto.Base64Decode(raw)
		if err != nil {
			continue
		}

		// Decrypt with SessionKey (since server uses our SessionKey)
		dec, err := crypto.Decrypt(c.SessionKey, enc)
		if err != nil {
			continue
		}

		msg, err := protocol.Decode(dec)
		if err != nil {
			continue
		}

		if msg.Type == config.MsgTypeRoomKey {
			continue // legacy; vault token arrives via EPOCH
		}
		// Security Check: Engine Compatibility
		if msg.Type == config.MsgTypeJoin {
			peerVersion := msg.FileId // We'll hijack FileId for version during Join
			if peerVersion != "" && peerVersion != config.Version {
				c.sendUpdate(&protocol.ChatMessage{
					Type:    config.MsgTypeSystem,
					Sender:  "SECURITY",
					Content: fmt.Sprintf("⚠️ WARNING: Peer %s is using a different version (%s). Sync might be unstable.", msg.Sender, peerVersion),
				})
			}
		}

		if msg.Type == config.MsgTypeFileData {
			c.mu.Lock()
			ch := c.fileGets[msg.FileId]
			c.mu.Unlock()
			if ch != nil {
				select {
				case ch <- msg:
				default:
				}
			}
			continue
		}

		if msg.Type == config.MsgTypeEpoch {
			if tok := strings.TrimSpace(msg.Content); tok != "" {
				c.mu.Lock()
				c.VaultToken = tok
				c.mu.Unlock()
				if c.p2p != nil {
					c.p2p.SetToken(tok)
				}
			}
			seed := crypto.GenerateRandomKey()
			if c.Senders != nil {
				c.Senders.ResetEpoch(c.Senders.Epoch()+1, seed)
				c.publishSenderKey()
			}
			continue
		}
		if msg.Type == config.MsgTypeSenderKey && msg.Sender != c.Nickname {
			seed, err := crypto.Base64Decode(msg.Content)
			if err == nil && c.Senders != nil {
				c.Senders.AdoptPeer(msg.Sender, seed)
				c.flushPending(msg.Sender)
			}
			continue
		}
		if (msg.Type == config.MsgTypeChat || msg.Type == config.MsgTypeFileOffer) && msg.IsE2Ee && msg.Sender != c.Nickname {
			encContent, err := crypto.Base64Decode(msg.Content)
			if err == nil && c.Senders != nil {
				mk, err := c.Senders.Recv(msg.Sender, msg.RatchetStep)
				if err == nil {
					decContent, err := crypto.Decrypt(mk, encContent)
					if err == nil {
						msg.Content = string(decContent)
					} else {
						msg.Content = "[decrypt failed]"
					}
				} else {
					c.queuePending(msg)
					msg.Content = "[waiting for sender key]"
					c.publishSenderKey()
				}
			}
		}
		if msg.Type == config.MsgTypePrivate && msg.IsE2Ee && msg.Sender != c.Nickname {
			encContent, err := crypto.Base64Decode(msg.Content)
			if err == nil && len(c.E2EPriv) > 0 {
				peerPub, _ := crypto.Base64Decode(msg.PubKey)
				if len(peerPub) == 0 {
					c.mu.RLock()
					peerPub = c.PeerE2E[msg.Sender]
					c.mu.RUnlock()
				}
				if len(peerPub) == 32 {
					sk, err := crypto.DeriveSharedKey(c.E2EPriv, peerPub)
					if err == nil {
						decContent, err := crypto.Decrypt(sk, encContent)
						crypto.Wipe(sk)
						if err == nil {
							msg.Content = string(decContent)
						}
					}
				}
			}
		}

		if msg.Type == config.MsgTypeFileOffer {
			parts := strings.Split(msg.Content, "|")
			if len(parts) >= 3 {
				filename := parts[0]
				senderOnion := parts[1]
				fileID := parts[2]

				// CHECK: Already synced OR currently downloading OR is our own file
				if c.isSeen(filename) || c.isDownloading(filename) || msg.Sender == c.Nickname {
					continue
				}

				c.sendUpdate(&protocol.ChatMessage{
					Type:    config.MsgTypeSystem,
					Sender:  "SYNC",
					Content: "📥 Auto-sync triggered for: " + filename + " (Source: " + msg.Sender + ")",
				})

				checksum := ""
				if len(parts) >= 4 {
					checksum = parts[3]
				}

				// Mark as active to prevent duplicate triggers
				c.setDownloading(filename, true)

				go func(f, addr, id, expectedSum string) {
					defer func() {
						c.setDownloading(f, false)
					}()

					dest := c.inboxDir
					if dest == "" {
						dest = "downloads"
					}
					err := c.DownloadMux(f, dest)
					if err != nil {
						err = c.DownloadSharedFile(f, addr, dest)
					}

					if err == nil && expectedSum != "" {
						dest := c.inboxDir
						if dest == "" {
							dest = "downloads"
						}
						actual, _ := calculateSHA256(filepath.Join(dest, f))
						if actual != expectedSum {
							os.Remove(filepath.Join(dest, f))
							err = fmt.Errorf("checksum mismatch: security breach suspected")
						}
					}

					if err != nil {
						c.sendUpdate(&protocol.ChatMessage{
							Type:    config.MsgTypeSystem,
							Sender:  "SYNC",
							Content: "❌ Auto-download failed: " + f + " (" + err.Error() + ")",
						})
					}
				}(filename, senderOnion, fileID, checksum)
			}
			continue
		}

		if msg.Type == config.MsgTypeUserList {
			c.publishSenderKey()
			for _, p := range strings.Split(msg.Content, ",") {
				parts := strings.Split(p, "|")
				if len(parts) >= 2 {
					nick := parts[0]
					signingB64 := parts[1]

					signingPub, _ := crypto.Base64Decode(signingB64)

					if len(parts) >= 3 {
						e2e, _ := crypto.Base64Decode(parts[2])
						if len(e2e) == 32 {
							c.mu.Lock()
							c.PeerE2E[nick] = e2e
							c.mu.Unlock()
						}
					}
					if len(signingPub) == 32 {
						isOK, isNew := c.IdentityStore.Check(nick, signingB64)
						if !isOK {
							c.sendUpdate(&protocol.ChatMessage{
								Type:    config.MsgTypeSystem,
								Sender:  "SECURITY",
								Content: fmt.Sprintf("🚨 WARNING: %s has changed their identity key!", nick),
							})
							continue
						}
						if isNew && nick != c.Nickname {
							c.sendUpdate(&protocol.ChatMessage{
								Type:    config.MsgTypeSystem,
								Sender:  "SECURITY",
								Content: fmt.Sprintf("🛡️ New identity discovered: %s", nick),
							})
						}
						c.UserKeys[nick] = signingPub
					}
				}
			}
		}

		if msg.Type == config.MsgTypePing {
			pongMsg := protocol.CreateMessage(config.MsgTypePong, "", c.Nickname)
			c.SendChan <- pongMsg
			continue
		}

		c.sendUpdate(msg)
	}
}

// Atomic Map Helpers
func (c *Client) isSynced(filename string, modTime time.Time) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	lastSync, seen := c.syncLedger[filename]
	if !seen {
		return false
	}
	return !modTime.After(lastSync)
}

func (c *Client) isSeen(filename string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, seen := c.syncLedger[filename]
	return seen
}

func (c *Client) markSynced(filename string, modTime time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.syncLedger[filename] = modTime
}

func (c *Client) setDownloading(filename string, active bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if active {
		c.activeDownloads[filename] = true
	} else {
		delete(c.activeDownloads, filename)
	}
}

func (c *Client) isDownloading(filename string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.activeDownloads[filename]
}

func (c *Client) StartAutoSync() {
	c.autoSyncOnce.Do(func() {
		out := c.outboxDir
		if out == "" {
			out = "uploads"
		}
		in := c.inboxDir
		if in == "" {
			in = "downloads"
		}
		os.MkdirAll(out, 0755)
		os.MkdirAll(in, 0755)

		tempDir := filepath.Join(".conner_data", "temp_shares")
		os.RemoveAll(tempDir)
		os.MkdirAll(tempDir, 0700)

		w := filesync.New(out, time.Second)
		go func() {
			<-c.Ctx.Done()
			w.Stop()
		}()
		go w.Events(func(path string, info os.FileInfo) {
			name := filepath.Base(path)
			if c.isDownloading(name) || c.isSynced(name, info.ModTime()) {
				return
			}
			c.markSynced(name, info.ModTime())
			c.sendUpdate(&protocol.ChatMessage{
				Type:    config.MsgTypeSystem,
				Sender:  "SYNC",
				Content: "📤 Auto-uploading: " + name,
			})
			go c.offerFile(path, info)
		})
	})
}

func (c *Client) offerFile(path string, info os.FileInfo) {
	if info.IsDir() {
		c.sendUpdate(&protocol.ChatMessage{Type: config.MsgTypeSystem, Sender: "SYNC", Content: "skip directory (share files, not folders): " + filepath.Base(path)})
		return
	}
	err := c.UploadToServer(path)
	if err == nil {
		c.sendUpdate(&protocol.ChatMessage{Type: config.MsgTypeSystem, Sender: "SYNC", Content: "✅ Uploaded to Vault: " + filepath.Base(path)})
		metadata := fmt.Sprintf("%s|%s|VAULT", filepath.Base(path), c.ServerOnion)
		c.SendChan <- protocol.CreateMessage(config.MsgTypeFileOffer, metadata, c.Nickname)
		return
	}
	c.sendUpdate(&protocol.ChatMessage{Type: config.MsgTypeSystem, Sender: "SYNC", Content: "❌ Sync failed: " + filepath.Base(path) + " - " + err.Error()})
}

func (c *Client) UploadMux(localPath string) error {
	info, err := os.Stat(localPath)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("skip directory")
	}
	if info.Size() > 100*1024*1024 {
		return fmt.Errorf("file too large for auto-sync (>100MB)")
	}
	f, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer f.Close()
	name := filepath.Base(localPath)
	c.mu.RLock()
	token := c.VaultToken
	c.mu.RUnlock()
	key := crypto.FileContentKey(token, name)
	buf := make([]byte, crypto.ChunkSize)
	var idx int32
	for {
		n, err := io.ReadFull(f, buf)
		if n > 0 {
			ct, e := crypto.EncryptChunk(key, name, uint32(idx), buf[:n])
			if e != nil {
				return e
			}
			m := protocol.CreateMessage(config.MsgTypeFilePut, crypto.Base64Encode(ct), c.Nickname)
			m.FileId = name
			m.ChunkIdx = idx
			c.EnqueueOrSend(m)
			idx++
		}
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			return err
		}
	}
	fin := protocol.CreateMessage(config.MsgTypeFilePut, "", c.Nickname)
	fin.FileId = name
	fin.ChunkIdx = -1
	fin.TotalChunks = idx
	c.EnqueueOrSend(fin)
	return nil
}

func (c *Client) DownloadMux(filename, destDir string) error {
	filename = filepath.Base(filename)
	_ = os.MkdirAll(destDir, 0o700)
	ch := make(chan *protocol.ChatMessage, 64)
	c.mu.Lock()
	c.fileGets[filename] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.fileGets, filename)
		c.mu.Unlock()
	}()
	req := protocol.CreateMessage(config.MsgTypeFileGet, filename, c.Nickname)
	req.FileId = filename
	c.EnqueueOrSend(req)
	c.mu.RLock()
	token := c.VaultToken
	c.mu.RUnlock()
	key := crypto.FileContentKey(token, filename)
	out, err := os.Create(filepath.Join(destDir, filename))
	if err != nil {
		return err
	}
	defer out.Close()
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		select {
		case msg := <-ch:
			if msg.ChunkIdx < 0 {
				return nil
			}
			ct, err := crypto.Base64Decode(msg.Content)
			if err != nil {
				return err
			}
			pt, err := crypto.DecryptChunk(key, filename, uint32(msg.ChunkIdx), ct)
			if err != nil {
				return err
			}
			if _, err := out.Write(pt); err != nil {
				return err
			}
		default:
			time.Sleep(20 * time.Millisecond)
		}
	}
	return fmt.Errorf("mux download timeout")
}

func (c *Client) UploadToServer(localPath string) error {
	return c.UploadMux(localPath)
}

func (c *Client) DownloadP2PFile(filename, senderOnion, fileID, destDir string) error {
	filename = filepath.Base(filename)
	c.mu.Lock()
	c.activeDownloads[filename] = true
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.activeDownloads, filename)
		c.mu.Unlock()
	}()

	os.MkdirAll(destDir, 0755)

	// Hash Handshake: Get current RoomKey hash
	c.mu.RLock()
	rk := c.RoomKey
	c.mu.RUnlock()

	h := sha256.New()
	h.Write(rk)
	myHash := hex.EncodeToString(h.Sum(nil))

	// Construct URL: http://onion:80/p2p_download?id=...
	urlStr := fmt.Sprintf("http://%s/p2p_download?id=%s", senderOnion, fileID)
	if !strings.Contains(senderOnion, ":") {
		urlStr = fmt.Sprintf("http://%s:80/p2p_download?id=%s", senderOnion, fileID)
	}

	client := c.httpClient(60 * time.Minute)

	req, _ := http.NewRequest("GET", urlStr, nil)
	req.Header.Set("Authorization", "Bearer "+myHash)

	var resp *http.Response
	var err error
	for i := 0; i < 12; i++ {
		resp, err = client.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			break
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(5 * time.Second)
	}

	if err != nil {
		return fmt.Errorf("P2P download failed after retries: %w", err)
	}
	defer resp.Body.Close()

	destPath := filepath.Join(destDir, filename)
	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()

	if resp.Header.Get("X-Conner-AEAD") == "chunk-v1" {
		key := crypto.FileContentKey(myHash, fileID)
		err = crypto.DecryptReaderTo(out, resp.Body, key, fileID)
	} else {
		_, err = io.Copy(out, resp.Body)
	}
	out.Close()
	if err != nil {
		return err
	}

	// Update ledger so we don't re-upload what we just downloaded
	c.markSynced(filename, time.Now())

	// Auto-extract if it was a directory (zip)
	if strings.HasSuffix(filename, ".zip") {
		extractPath := filepath.Join(destDir, strings.TrimSuffix(filename, ".zip"))
		os.MkdirAll(extractPath, 0755)
		if err := ExtractZip(destPath, extractPath); err == nil {
			os.Remove(destPath)
		}
	}

	c.sendUpdate(&protocol.ChatMessage{
		Type:    config.MsgTypeSystem,
		Sender:  "P2P",
		Content: "✅ P2P Download Complete: " + filename,
	})

	return nil
}

func (c *Client) DownloadSharedFile(filename, serverAddr, destDir string) error {
	filename = filepath.Base(filename)
	c.setDownloading(filename, true)
	defer c.setDownloading(filename, false)

	os.MkdirAll(destDir, 0755)
	c.mu.RLock()
	token := c.VaultToken
	c.mu.RUnlock()

	url := fmt.Sprintf("http://%s/download?f=%s&t=%s", serverAddr, filename, token)
	// If it's an onion address and no port is specified, use 80
	if strings.HasSuffix(serverAddr, ".onion") && !strings.Contains(serverAddr, ":") {
		url = fmt.Sprintf("http://%s:80/download?f=%s&t=%s", serverAddr, filename, token)
	} else if !strings.Contains(serverAddr, ":") {
		// Use the learned ServerHTTPPort for Direct Mode
		if c.ServerHTTPPort > 0 {
			url = fmt.Sprintf("http://%s:%d/download?f=%s&t=%s", serverAddr, c.ServerHTTPPort, filename, token)
		} else {
			url = fmt.Sprintf("http://%s:6666/download?f=%s&t=%s", serverAddr, filename, token)
		}
	}

	client := c.httpClient(10 * time.Minute)

	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+token)

	var resp *http.Response
	var err error
	for i := 0; i < 3; i++ {
		resp, err = client.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			break
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(2 * time.Second) // Wait before retry
	}

	if err != nil {
		return fmt.Errorf("download failed after 3 attempts: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server error: %d", resp.StatusCode)
	}

	destPath := filepath.Join(destDir, filename)
	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create local file: %w", err)
	}
	defer out.Close()

	if resp.Header.Get("X-Conner-AEAD") == "chunk-v1" {
		key := crypto.FileContentKey(token, filename)
		err = crypto.DecryptReaderTo(out, resp.Body, key, filename)
	} else {
		_, err = io.Copy(out, resp.Body)
	}
	if err != nil {
		return fmt.Errorf("failed to write data: %w", err)
	}

	// Update ledger so we don't re-upload what we just downloaded
	info, _ := os.Stat(destPath)
	c.markSynced(filename, info.ModTime())

	// If it was a zip, extract it
	if strings.HasSuffix(filename, ".zip") {
		extractPath := filepath.Join(destDir, strings.TrimSuffix(filename, ".zip"))
		os.MkdirAll(extractPath, 0755)
		if err := ExtractZip(destPath, extractPath); err == nil {
			os.Remove(destPath)
		} else {
			return fmt.Errorf("failed to extract zip: %w", err)
		}
	}

	c.sendUpdate(&protocol.ChatMessage{
		Type:    config.MsgTypeSystem,
		Sender:  "SYNC",
		Content: "✅ Auto-download complete: " + filename,
	})

	return nil
}

func CreateZip(src, dst string) error {
	zipFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer zipFile.Close()

	zw := zip.NewWriter(zipFile)
	defer zw.Close()

	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name, _ = filepath.Rel(filepath.Dir(src), path)
		if info.IsDir() {
			header.Name += "/"
		} else {
			header.Method = zip.Deflate
		}
		writer, err := zw.CreateHeader(header)
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		_, err = io.Copy(writer, file)
		return err
	})
}

func ExtractZip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		fpath := filepath.Join(dest, f.Name)
		if !strings.HasPrefix(fpath, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("illegal file path: %s", fpath)
		}
		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, os.ModePerm)
			continue
		}
		if err = os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
			return err
		}
		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return err
		}
		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// sendUpdate sends to UpdateChan without blocking — drops if channel is full.
func (c *Client) sendUpdate(msg *protocol.ChatMessage) {
	select {
	case c.UpdateChan <- msg:
	default:
		// TUI is too slow; discard rather than block the read loop.
	}
}

func (c *Client) flushQueue() {
	if c.queue == nil {
		return
	}
	msgs, err := c.queue.Drain()
	if err != nil {
		return
	}
	for _, m := range msgs {
		select {
		case c.SendChan <- m:
		default:
			_ = c.queue.Enqueue(m)
			return
		}
	}
}

func (c *Client) EnqueueOrSend(msg *protocol.ChatMessage) {
	select {
	case c.SendChan <- msg:
	default:
		if c.queue != nil {
			_ = c.queue.Enqueue(msg)
		}
	}
}

func (c *Client) writePump() {
	for chatMsg := range c.SendChan {
		if chatMsg.OnionAddr == "" {
			chatMsg.OnionAddr = c.CurrentRoom
		}
		if (chatMsg.Type == config.MsgTypeChat || chatMsg.Type == config.MsgTypePrivate) && c.IdentityStore != nil && c.IdentityStore.HasMismatch() {
			c.sendUpdate(&protocol.ChatMessage{Type: config.MsgTypeSystem, Sender: "SECURITY", Content: "send blocked: a peer's identity key changed. /trust <nick> after out-of-band check, or ignore that peer."})
			continue
		}
		switch chatMsg.Type {
		case config.MsgTypeCmd, config.MsgTypeFilePut, config.MsgTypeFileGet, config.MsgTypeFileData:
			// hub-visible; session AEAD only (vault mux)
		case config.MsgTypeChat, config.MsgTypeFileOffer:
			if c.Senders != nil {
				if mk, step, ok := c.Senders.NextSend(); ok {
					encContent, err := crypto.Encrypt(mk, []byte(chatMsg.Content))
					if err == nil {
						chatMsg.Content = crypto.Base64Encode(encContent)
						chatMsg.RatchetStep = step
						chatMsg.IsE2Ee = true
					}
				}
			}
		case config.MsgTypePrivate:
			c.mu.RLock()
			peerPub := c.PeerE2E[chatMsg.ReplyTo]
			c.mu.RUnlock()
			if len(peerPub) == 32 && len(c.E2EPriv) > 0 {
				sk, err := crypto.DeriveSharedKey(c.E2EPriv, peerPub)
				if err == nil {
					encContent, err := crypto.Encrypt(sk, []byte(chatMsg.Content))
					crypto.Wipe(sk)
					if err == nil {
						chatMsg.Content = crypto.Base64Encode(encContent)
						chatMsg.IsE2Ee = true
						chatMsg.PubKey = crypto.Base64Encode(c.E2EPub)
					}
				}
			}
		}

		jsonBytes, _ := chatMsg.Encode()
		enc, err := crypto.Encrypt(c.SessionKey, jsonBytes)
		if err != nil {
			continue
		}
		_ = protocol.SendFrame(c.Conn, []byte(crypto.Base64Encode(enc)))
	}
}
func calculateSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (c *Client) queuePending(msg *protocol.ChatMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.pendingDecrypt) > 64 {
		c.pendingDecrypt = c.pendingDecrypt[1:]
	}
	cp, _ := proto.Clone(msg).(*protocol.ChatMessage)
	if cp == nil {
		cp = msg
	}
	c.pendingDecrypt = append(c.pendingDecrypt, cp)
}

func (c *Client) flushPending(nick string) {
	c.mu.Lock()
	left := c.pendingDecrypt[:0]
	var ready []*protocol.ChatMessage
	for _, m := range c.pendingDecrypt {
		if m.Sender != nick {
			left = append(left, m)
			continue
		}
		ready = append(ready, m)
	}
	c.pendingDecrypt = left
	c.mu.Unlock()
	for _, m := range ready {
		encContent, err := crypto.Base64Decode(m.Content)
		if err != nil || c.Senders == nil {
			continue
		}
		mk, err := c.Senders.Recv(m.Sender, m.RatchetStep)
		if err != nil {
			continue
		}
		pt, err := crypto.Decrypt(mk, encContent)
		if err != nil {
			continue
		}
		m.Content = string(pt)
		c.sendUpdate(m)
	}
}

func (c *Client) publishSenderKey() {
	if c.Senders == nil {
		return
	}
	seed := c.Senders.ExportMySeed()
	if len(seed) == 0 {
		return
	}
	msg := protocol.CreateMessage(config.MsgTypeSenderKey, crypto.Base64Encode(seed), c.Nickname)
	select {
	case c.SendChan <- msg:
	default:
	}
}

func (c *Client) SetRoomDirs(room string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.CurrentRoom = room
	c.outboxDir = appdir.RoomOutbox(room)
	c.inboxDir = appdir.RoomInbox(room)
	_ = os.MkdirAll(c.outboxDir, 0700)
	_ = os.MkdirAll(c.inboxDir, 0700)
}

func (c *Client) InboxDir() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.inboxDir == "" {
		return "downloads"
	}
	return c.inboxDir
}

func (c *Client) OutboxDir() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.outboxDir == "" {
		return "uploads"
	}
	return c.outboxDir
}
