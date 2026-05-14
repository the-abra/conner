package client

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/proxy"

	"conner/internal/config"
	"conner/internal/crypto"
	"conner/internal/protocol"
	"conner/internal/tor"
)


var ErrBanned = fmt.Errorf("You are banned from this server")

// isTorRunning probes the SOCKS5 port without sending any data.
func isTorRunning() bool {
	c, err := net.DialTimeout("tcp", config.TorSocksAddr, 2*time.Second)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// ensureTorRunning checks whether Tor's SOCKS5 port is open.
// If not, it attempts to start the system 'tor' daemon and waits
// up to 60 seconds for the port to become available.
// Only called when a .onion address is supplied.
func ensureTorRunning() error {
	if isTorRunning() {
		return nil
	}

	// Tor should have been started by main.go. 
	// We just wait up to 60s for it to bootstrap.
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(2 * time.Second)
		if isTorRunning() {
			return nil
		}
	}
	return fmt.Errorf("Tor SOCKS5 port (%s) not available after 60s — check if Tor initialized correctly", config.TorSocksAddr)
}

type Client struct {
	Conn               net.Conn
	SessionKey         []byte
	Nickname           string
	Messages           []string
	SendChan           chan *protocol.ChatMessage
	p2p                *P2PService // P2P Service for Tor mode
	mu                 sync.RWMutex
	UpdateChan         chan *protocol.ChatMessage // typed messages for rich TUI rendering
	// Security items
	SigningPriv        []byte                    // Ed25519 Private Key for identity
	SigningPub         []byte                    // Ed25519 Public Key for identity
	UserKeys           map[string][]byte          // Other users' Identity public keys
	IdentityStore      *IdentityStore
	RoomKey            []byte      // Centralized AES key (Content)
	VaultToken         string      // Server access token (Auth)
	ServerOnion        string      // Onion address of the hub
	ServerHTTPPort     int         // Port for Direct mode file transfers
	UseTor             bool
	SocksAddr          string
	syncLedger         map[string]time.Time      // filename -> last sync time
	activeDownloads    map[string]bool           // filename -> currently downloading
	Ctx                context.Context
	Cancel             context.CancelFunc
	autoSyncOnce       sync.Once
}


func loadIdentityKeys(nick string) (pub []byte, priv []byte) {
	keyFile := fmt.Sprintf("identity_%s.key", nick)
	if data, err := os.ReadFile(keyFile); err == nil && len(data) == 64 {
		return data[32:], data[:64]
	}
	p, s, _ := crypto.GenerateSigningKeyPair()
	_ = os.WriteFile(keyFile, s, 0600)
	return p, s
}

func Connect(nickname, address string, useTor bool, et *tor.EmbeddedTor) (*Client, error) {
	var conn net.Conn
	var err error

	if useTor {
		// Ensure Tor SOCKS5 is running before attempting connection.
		if torErr := ensureTorRunning(); torErr != nil {
			return nil, torErr
		}
		dialer, dialErr := proxy.SOCKS5("tcp", config.TorSocksAddr, nil, proxy.Direct)
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

	line := string(payload)
	if strings.HasPrefix(line, "ERROR:") {
		errStr := strings.TrimPrefix(line, "ERROR:")
		conn.Close()
		if strings.Contains(strings.ToLower(errStr), "ban") {
			return nil, ErrBanned
		}
		return nil, fmt.Errorf("server error: %s", errStr)
	}

	if !strings.HasPrefix(line, "KEY_EXCHANGE:") {
		conn.Close()
		return nil, fmt.Errorf("handshake: expected KEY_EXCHANGE, got: %s", line)
	}

	exchangeParts := strings.Split(strings.TrimPrefix(line, "KEY_EXCHANGE:"), "|")
	if len(exchangeParts) < 3 {
		conn.Close()
		return nil, fmt.Errorf("handshake: missing challenge nonce or difficulty")
	}

	serverPub, _ := crypto.Base64Decode(exchangeParts[0])
	nonce, _ := crypto.Base64Decode(exchangeParts[1])
	difficultyStr := exchangeParts[2]
	
	// PoW
	var difficulty int
	fmt.Sscanf(difficultyStr, "%d", &difficulty)
	powNonce := crypto.ComputePoW(nonce, difficulty)
	powNonceBytes := make([]byte, 8)
	importBinary := true // ensure binary package is used
	if importBinary {
		// we will encode big endian
		powNonceBytes[0] = byte(powNonce >> 56)
		powNonceBytes[1] = byte(powNonce >> 48)
		powNonceBytes[2] = byte(powNonce >> 40)
		powNonceBytes[3] = byte(powNonce >> 32)
		powNonceBytes[4] = byte(powNonce >> 24)
		powNonceBytes[5] = byte(powNonce >> 16)
		powNonceBytes[6] = byte(powNonce >> 8)
		powNonceBytes[7] = byte(powNonce)
	}

	priv, pub, _ := crypto.GenerateKeyPair()
	sessionKey, _ := crypto.DeriveSharedKey(priv, serverPub)

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

	hello := fmt.Sprintf("CLIENT_HELLO:%s:%s:%s:%s:%s:%s",
		crypto.Base64Encode(pub),
		nickname,
		identity,
		crypto.Base64Encode(idPub),
		crypto.Base64Encode(sig),
		crypto.Base64Encode(powNonceBytes))

	if err := protocol.SendFrame(conn, []byte(hello)); err != nil {
		return nil, fmt.Errorf("handshake: CLIENT_HELLO send failed: %w", err)
	}

	// 3. Receive HANDSHAKE_OK
	respPayload, err := protocol.ReadFrame(conn)
	if err != nil {
		return nil, fmt.Errorf("handshake: response read failed: %w", err)
	}
	respLine := string(respPayload)
	if !strings.HasPrefix(respLine, "HANDSHAKE_OK") {
		return nil, fmt.Errorf("handshake: expected HANDSHAKE_OK, got: %s", respLine)
	}

	var srvHTTPPort int
	partsOK := strings.Split(respLine, "|")
	if len(partsOK) >= 2 {
		fmt.Sscanf(partsOK[1], "%d", &srvHTTPPort)
	}

	// 1. Initialize P2P Service once
	client := &Client{
		Conn:               conn,
		SessionKey:         sessionKey,
		Messages:           []string{},
		Nickname:           nickname,
		SendChan:           make(chan *protocol.ChatMessage, 10),
		UpdateChan:         make(chan *protocol.ChatMessage, 200),
		SigningPub:         idPub,
		SigningPriv:        idPriv,
		UserKeys:           make(map[string][]byte),
		IdentityStore:      NewIdentityStore(fmt.Sprintf("identities_%s.json", nickname)),
		ServerOnion:        strings.Split(address, ":")[0],
		ServerHTTPPort:     srvHTTPPort,
		UseTor:             useTor,
		SocksAddr:          config.TorSocksAddr,
		syncLedger:         make(map[string]time.Time),
		activeDownloads:    make(map[string]bool),
	}
	client.Ctx, client.Cancel = context.WithCancel(context.Background())

	if et != nil {
		p2p, _ := StartP2PService(et)
		client.p2p = p2p
		client.SocksAddr = et.SocksAddr
	}

	go client.readPump()
	go client.writePump()

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

		// Unified Key Update: Calculate SHA256 hash for Vault Access
		if msg.Type == config.MsgTypeRoomKey {
			key, _ := crypto.Base64Decode(msg.Content)
			if len(key) == 32 {
				h := sha256.New()
				h.Write(key)
				hash := hex.EncodeToString(h.Sum(nil))

				c.mu.Lock()
				crypto.Wipe(c.RoomKey)
				c.RoomKey = key
				c.VaultToken = hash
				if c.p2p != nil {
					c.p2p.UpdateToken(key)
				}
				c.mu.Unlock()
			}
			continue
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

		// Decrypt Group Messages (Chat / FileOffer)
		if (msg.Type == config.MsgTypeChat || msg.Type == config.MsgTypeFileOffer) && msg.Sender != c.Nickname {
			c.mu.Lock()
			rk := c.RoomKey
			c.mu.Unlock()
			
			if len(rk) == 32 {
				encContent, _ := crypto.Base64Decode(msg.Content)
				decContent, err := crypto.Decrypt(rk, encContent)
				if err == nil {
					msg.Content = string(decContent)
				} else {
					msg.Content = "[Decryption Failed - Waiting for new Room Key...]"
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

					var err error
					if id == "VAULT" {
						err = c.DownloadSharedFile(f, addr, "downloads")
					} else {
						err = c.DownloadP2PFile(f, addr, id, "downloads")
					}
					
					if err == nil && expectedSum != "" {
						actual, _ := calculateSHA256(filepath.Join("downloads", f))
						if actual != expectedSum {
							os.Remove(filepath.Join("downloads", f))
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
			for _, p := range strings.Split(msg.Content, ",") {
				parts := strings.Split(p, "|")
				if len(parts) >= 2 {
					nick := parts[0]
					signingB64 := parts[1]
					
					signingPub, _ := crypto.Base64Decode(signingB64)

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
	if !seen { return false }
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
		os.MkdirAll("uploads", 0755)
		os.MkdirAll("downloads", 0755)
		
		tempDir := filepath.Join(".conner_data", "temp_shares")
		os.RemoveAll(tempDir)
		os.MkdirAll(tempDir, 0700)

		go func() {
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-c.Ctx.Done():
					return
				case <-ticker.C:
					files, err := os.ReadDir("uploads")
					if err != nil {
						continue
					}

					for _, file := range files {
						name := file.Name()
						info, err := file.Info()
						if err != nil {
							continue
						}

						if c.isDownloading(name) || c.isSynced(name, info.ModTime()) {
							continue
						}

						c.markSynced(name, info.ModTime())

						c.sendUpdate(&protocol.ChatMessage{
							Type:    config.MsgTypeSystem,
							Sender:  "SYNC",
							Content: "📤 Auto-uploading: " + name,
						})
						
						go func(path string) {
							var err error
							if c.UseTor && c.p2p != nil && c.p2p.GetOnionAddr() != "" {
								// Tor Mode: P2P Share
								targetPath := path
								displayPath := filepath.Base(path)
								if info.IsDir() {
									tempDir := filepath.Join(".conner_data", "temp_shares")
									zipPath := filepath.Join(tempDir, displayPath+".zip")
									if err := CreateZip(path, zipPath); err != nil {
										c.sendUpdate(&protocol.ChatMessage{Type: config.MsgTypeSystem, Sender: "SYNC", Content: "❌ Zip failed: " + err.Error()})
										return
									}
									targetPath = zipPath
									displayPath = displayPath + ".zip"
									// Do NOT remove yet - it needs to be served to peers
								}

								fileID := fmt.Sprintf("SYNC_%d_%s", time.Now().Unix(), c.Nickname)
								c.p2p.AddFile(fileID, targetPath)
								checksum, _ := calculateSHA256(targetPath)
								metadata := fmt.Sprintf("%s|%s|%s|%s", displayPath, c.p2p.GetOnionAddr(), fileID, checksum)
								shareMsg := protocol.CreateMessage(config.MsgTypeFileOffer, metadata, c.Nickname)
								c.SendChan <- shareMsg
								
								c.sendUpdate(&protocol.ChatMessage{
									Type:    config.MsgTypeSystem,
									Sender:  "P2P",
									Content: "📡 P2P Share Active: " + displayPath + " (ID: " + fileID + ")",
								})
							} else {
								// Direct Mode: Upload to Server Vault
								err = c.UploadToServer(path)
								if err == nil {
									c.sendUpdate(&protocol.ChatMessage{
										Type:    config.MsgTypeSystem,
										Sender:  "SYNC",
										Content: "✅ Uploaded to Vault: " + filepath.Base(path),
									})
									// Announce to everyone
									metadata := fmt.Sprintf("%s|%s|VAULT", filepath.Base(path), c.ServerOnion)
									c.SendChan <- protocol.CreateMessage(config.MsgTypeFileOffer, metadata, c.Nickname)
								}
							}

							if err != nil {
								c.sendUpdate(&protocol.ChatMessage{
									Type:    config.MsgTypeSystem,
									Sender:  "SYNC",
									Content: "❌ Sync failed: " + filepath.Base(path) + " - " + err.Error(),
								})
							}
						}(filepath.Join("uploads", name))
					}
				}
			}
		}()
	})
}

func (c *Client) UploadToServer(localPath string) error {
	info, err := os.Stat(localPath)
	if err != nil {
		return err
	}

	if info.Size() > 100*1024*1024 {
		return fmt.Errorf("file too large for auto-sync (>100MB)")
	}

	targetFile := localPath
	isDir := info.IsDir()
	if isDir {
		zipPath := localPath + ".zip"
		if err := CreateZip(localPath, zipPath); err != nil {
			return err
		}
		targetFile = zipPath
		defer os.Remove(zipPath)
	}

	file, err := os.Open(targetFile)
	if err != nil {
		return err
	}
	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", filepath.Base(targetFile))
	if err != nil {
		return err
	}
	_, err = io.Copy(part, file)
	writer.Close()

	c.mu.Lock()
	token := c.VaultToken
	c.mu.Unlock()

	url := fmt.Sprintf("http://%s/upload?u=%s&t=%s", c.ServerOnion, c.Nickname, token)
	if c.UseTor {
		url = fmt.Sprintf("http://%s:80/upload?u=%s&t=%s", c.ServerOnion, c.Nickname, token)
	} else if !strings.Contains(c.ServerOnion, ":") {
		if c.ServerHTTPPort > 0 {
			url = fmt.Sprintf("http://%s:%d/upload?u=%s&t=%s", c.ServerOnion, c.ServerHTTPPort, c.Nickname, token)
		} else {
			url = fmt.Sprintf("http://%s:6666/upload?u=%s&t=%s", c.ServerOnion, c.Nickname, token)
		}
	}

	req, _ := http.NewRequest("POST", url, body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)

	var client *http.Client
	if c.UseTor {
		dialer, _ := proxy.SOCKS5("tcp", c.SocksAddr, nil, proxy.Direct)
		transport := &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return dialer.Dial(network, addr)
			},
		}
		client = &http.Client{Transport: transport, Timeout: 10 * time.Minute}
	} else {
		client = &http.Client{Timeout: 10 * time.Minute}
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server error: %d", resp.StatusCode)
	}

	return nil
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

	var proxyURL *url.URL
	if c.UseTor {
		proxyURL, _ = url.Parse("socks5://" + c.SocksAddr)
	}

	client := &http.Client{
		Timeout: 60 * time.Minute,
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
			DialContext: (&net.Dialer{
				Timeout:   60 * time.Second,
				KeepAlive: 60 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout: 60 * time.Second,
		},
	}

	req, _ := http.NewRequest("GET", urlStr, nil)
	req.Header.Set("Authorization", "Bearer "+myHash)

	var resp *http.Response
	var err error
	for i := 0; i < 12; i++ {
		resp, err = client.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			break
		}
		if resp != nil { resp.Body.Close() }
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

	_, err = io.Copy(out, resp.Body)
	out.Close() // Close before extraction
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

	var client *http.Client
	if c.UseTor {
		dialer, _ := proxy.SOCKS5("tcp", c.SocksAddr, nil, proxy.Direct)
		transport := &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return dialer.Dial(network, addr)
			},
		}
		client = &http.Client{Transport: transport, Timeout: 10 * time.Minute}
	} else {
		client = &http.Client{Timeout: 10 * time.Minute}
	}

	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+token)

	var resp *http.Response
	var err error
	for i := 0; i < 3; i++ {
		resp, err = client.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			break
		}
		if resp != nil { resp.Body.Close() }
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

	_, err = io.Copy(out, resp.Body)
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

func (c *Client) writePump() {
	for chatMsg := range c.SendChan {
		// Encrypt with RoomKey if it's a chat message
		if chatMsg.Type == config.MsgTypeChat || chatMsg.Type == config.MsgTypeFileOffer {
			c.mu.Lock()
			rk := c.RoomKey
			c.mu.Unlock()
			
			if len(rk) == 32 {
				encContent, _ := crypto.Encrypt(rk, []byte(chatMsg.Content))
				chatMsg.Content = crypto.Base64Encode(encContent)
			}
		}

		jsonBytes, _ := chatMsg.Encode()
		enc, _ := crypto.Encrypt(c.SessionKey, jsonBytes)
		protocol.SendFrame(c.Conn, []byte(crypto.Base64Encode(enc)))
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


