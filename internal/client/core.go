package client

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/proxy"

	"conner/internal/config"
	"conner/internal/crypto"
	"conner/internal/protocol"
)

const torSOCKS5 = "127.0.0.1:9050"

var ErrBanned = fmt.Errorf("You are banned from this server")

// isTorRunning probes the SOCKS5 port without sending any data.
func isTorRunning() bool {
	c, err := net.DialTimeout("tcp", torSOCKS5, 2*time.Second)
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

	// Try to start tor; best-effort — ignore the error if tor isn't installed
	// (the connection attempt will fail with a clear message anyway).
	cmd := exec.Command("tor", "--quiet", "--RunAsDaemon", "1", // #nosec G204
		"--SocksPort", "9050",
		"--ControlPort", "9051",
		"--CookieAuthentication", "1",
		"--DataDirectory", "/tmp/tor-conner",
		"--Log", "err file /dev/null")
	_ = cmd.Start() // detach; don't wait

	// Wait up to 60 s for the port to open (Tor bootstraps slowly).
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(2 * time.Second)
		if isTorRunning() {
			return nil
		}
	}
	return fmt.Errorf("Tor SOCKS5 port not available after 60s — is Tor installed and running?")
}

type OnionInfo struct {
	Name  string
	Onion string
	Token string
}

type Client struct {
	Conn               net.Conn
	SessionKey         []byte
	Nickname           string
	Messages           []string
	SendChan           chan *protocol.ChatMessage
	mu                 sync.Mutex
	UpdateChan         chan *protocol.ChatMessage // typed messages for rich TUI rendering
	PendingDownloadDir chan string               // TUI pushes destDir before sending DOWNLOAD_REQ
	sharedFiles        map[string]string         // filename -> absolute path
	AvailableFiles     map[string]OnionInfo      // id -> OnionInfo
	SigningPriv        []byte                    // Ed25519 Private Key for identity
	SigningPub         []byte                    // Ed25519 Public Key for identity

	// E2E items
	E2EPriv            []byte
	E2EPub             []byte
	UserKeys           map[string][]byte          // Other users' E2E public keys
	MyRatchet          *crypto.Ratchet            // Used to encrypt outgoing group messages
	SenderRatchets     map[string]*crypto.Ratchet // Used to decrypt incoming group messages

	autoSyncOnce       sync.Once
}

func (c *Client) StartAutoSync() {
	c.autoSyncOnce.Do(func() {
		os.MkdirAll("uploads", 0755)
		os.MkdirAll("downloads", 0755)

		go func() {
			ticker := time.NewTicker(5 * time.Second)
			for range ticker.C {
				files, err := os.ReadDir("uploads")
				if err != nil {
					continue
				}

				for _, f := range files {
					if f.IsDir() {
						continue
					}
					path := filepath.Join("uploads", f.Name())
					absPath, _ := filepath.Abs(path)

					// Only upload if we haven't shared this specific path yet
					c.mu.Lock()
					if c.sharedFiles == nil {
						c.sharedFiles = make(map[string]string)
					}
					_, alreadyShared := c.sharedFiles[f.Name()]
					c.mu.Unlock()

					if !alreadyShared {
						c.mu.Lock()
						c.sharedFiles[f.Name()] = absPath
						c.mu.Unlock()

						go func(p string) {
							_ = c.SendFile(p)
						}(absPath)
					}
				}
			}
		}()
	})
}

func loadIdentityKeys() (pub []byte, priv []byte) {
	keyFile := "identity.key"
	if data, err := os.ReadFile(keyFile); err == nil && len(data) == 64 {
		return data[32:], data[:64]
	}
	// Generate new
	p, s, _ := crypto.GenerateSigningKeyPair()
	_ = os.WriteFile(keyFile, s, 0600)
	return p, s
}

func Connect(nickname, address string) (*Client, error) {
	var conn net.Conn
	var err error

	if strings.Contains(address, ".onion") {
		// Ensure Tor SOCKS5 is running before attempting connection.
		// ensureTorRunning() is a no-op if already up; starts tor if not.
		if torErr := ensureTorRunning(); torErr != nil {
			return nil, torErr
		}
		// Route through Tor — passes domain name directly to the proxy,
		// ensuring zero local DNS leaks for .onion addresses.
		dialer, dialErr := proxy.SOCKS5("tcp", torSOCKS5, nil, proxy.Direct)
		if dialErr != nil {
			return nil, fmt.Errorf("failed to create Tor SOCKS5 dialer: %w", dialErr)
		}
		conn, err = dialer.Dial("tcp", address) // err is the outer var — no shadowing
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
	idPub, idPriv := loadIdentityKeys()
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

	e2ePriv, e2ePub, _ := crypto.GenerateKeyPair()

	hello := fmt.Sprintf("CLIENT_HELLO:%s:%s:%s:%s:%s:%s:%s",
		crypto.Base64Encode(pub),
		nickname,
		identity,
		crypto.Base64Encode(idPub),
		crypto.Base64Encode(sig),
		crypto.Base64Encode(powNonceBytes),
		crypto.Base64Encode(e2ePub))

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

	client := &Client{
		Conn:               conn,
		SessionKey:         sessionKey,
		Messages:           []string{},
		Nickname:           nickname,
		SendChan:           make(chan *protocol.ChatMessage, 10),
		UpdateChan:         make(chan *protocol.ChatMessage, 200),
		PendingDownloadDir: make(chan string, 10), // buffered so TUI never blocks
		sharedFiles:        make(map[string]string),
		AvailableFiles:     make(map[string]OnionInfo),
		SigningPub:         idPub,
		SigningPriv:        idPriv,
		E2EPriv:            e2ePriv,
		E2EPub:             e2ePub,
		UserKeys:           make(map[string][]byte),
		MyRatchet:          crypto.NewRatchet(crypto.GenerateRandomKey()),
		SenderRatchets:     make(map[string]*crypto.Ratchet),
	}

	go client.readPump()
	go client.writePump()

	return client, nil
}

func (c *Client) RegisterFile(path string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return err
	}
	name := info.Name()
	
	c.sendUpdate(&protocol.ChatMessage{
		Type:    config.MsgTypeSystem,
		Sender:  "SYSTEM",
		Content: "⏳ Creating Tor Ephemeral Service for " + name + "...",
	})

	fn, b64, err := CreateMediaTarBase64(absPath)
	if err != nil {
		return err
	}
	
	tmpFile := filepath.Join(os.TempDir(), fn+".b64")
	err = os.WriteFile(tmpFile, []byte(b64), 0644)
	if err != nil {
		return err
	}
	
	token := GenerateToken()
	onion, err := StartEphemeralService(tmpFile, token)
	if err != nil {
		return err
	}
	
	content := fmt.Sprintf("%s|%s|%s", name, onion, token)
	msg := protocol.CreateMessage(config.MsgTypeMediaInfo, content, c.Nickname)
	c.SendChan <- msg
	
	c.sendUpdate(&protocol.ChatMessage{
		Type:    config.MsgTypeSystem,
		Sender:  "SYSTEM",
		Content: "✅ File hosted at " + onion,
	})
	return nil
}

func (c *Client) SendFile(filePath string) error {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return err
	}
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return fmt.Errorf("file not found: %s", filePath)
	}

	token := GenerateToken()
	onion, err := StartEphemeralService(absPath, token)
	if err != nil {
		return fmt.Errorf("P2P service failed: %w", err)
	}

	// Broadcast offer to the group
	fileName := filepath.Base(absPath)
	offerMsg := protocol.CreateMessage(config.MsgTypeFileOffer, fmt.Sprintf("%s|%s|%s", fileName, onion, token), c.Nickname)
	offerMsg.IsE2Ee = true
	c.SendChan <- offerMsg

	c.sendUpdate(&protocol.ChatMessage{
		Type:    config.MsgTypeSystem,
		Sender:  "SYSTEM",
		Content: fmt.Sprintf("🚀 Hosting %s at %s. Service will close when client exits.", fileName, onion),
	})

	return nil
}

func (c *Client) DownloadSharedFile(id string, destDir string) error {
	info, ok := c.AvailableFiles[id]
	if !ok {
		return fmt.Errorf("file ID %s not found", id)
	}

	c.sendUpdate(&protocol.ChatMessage{
		Type:    config.MsgTypeSystem,
		Sender:  "SYSTEM",
		Content: "⏳ Downloading " + info.Name + " from " + info.Onion + "...",
	})

	destPath := filepath.Join(destDir, info.Name)
	os.MkdirAll(destDir, 0755)

	err := DownloadFile(info.Onion, info.Token, destPath)
	if err != nil {
		return err
	}

	c.sendUpdate(&protocol.ChatMessage{
		Type:    config.MsgTypeSystem,
		Sender:  "SYSTEM",
		Content: "✅ Download complete: " + destPath,
	})

	c.sendUpdate(&protocol.ChatMessage{
		Type:    config.MsgTypeMediaData,
		Sender:  "SYSTEM",
		Content: destPath,
	})
	return nil
}

func (c *Client) readPump() {
	defer func() {
		// Always notify the TUI when the connection ends.
		c.sendUpdate(&protocol.ChatMessage{
			Type:    config.MsgTypeSystem,
			Sender:  "SERVER",
			Content: "❌ Connection closed by server.",
		})
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

		if msg.Type == config.MsgTypeKeyShare && msg.IsE2Ee {
			parts := strings.SplitN(msg.Content, "|", 2)
			if len(parts) == 2 {
				// Decrypt the shared Sender Key
				senderE2E, ok := c.UserKeys[msg.Sender]
				if ok {
					shared, _ := crypto.DeriveSharedKey(c.E2EPriv, senderE2E)
					encBlob, _ := crypto.Base64Decode(parts[1])
					decKey, err := crypto.Decrypt(shared, encBlob)
					if err == nil && len(decKey) == 32 {
						c.SenderRatchets[msg.Sender] = crypto.NewRatchet(decKey)
					}
				}
			}
			continue
		}

		if msg.Type == config.MsgTypeChat && msg.Sender != c.Nickname {
			senderRatchet, ok := c.SenderRatchets[msg.Sender]
			if ok {
				msgKey, err := senderRatchet.GetMessageKey(msg.RatchetStep)
				if err == nil {
					encContent, _ := crypto.Base64Decode(msg.Content)
					decContent, err := crypto.Decrypt(msgKey, encContent)
					if err == nil {
						msg.Content = string(decContent)
					} else {
						msg.Content = "[E2EE Decryption Failed]"
					}
				} else {
					msg.Content = "[E2EE Ratchet Desync]"
				}
			} else {
				msg.Content = "[E2EE Missing Sender Key]"
			}
		}

		if msg.Type == config.MsgTypeUserList {
			for _, p := range strings.Split(msg.Content, ",") {
				parts := strings.SplitN(p, "|", 2)
				if len(parts) == 2 {
					nick := parts[0]
					pub, err := crypto.Base64Decode(parts[1])
					if err == nil && len(pub) == 32 {
						c.UserKeys[nick] = pub
						if nick != c.Nickname {
							// Share our key if we haven't received theirs or shared ours
							// Actually, let's just share it to be safe, it's idempotent for the UI.
							c.ShareKeyWith(nick)
						}
					}
				}
			}
		}

		if msg.Type == config.MsgTypePing {
			pongMsg := protocol.CreateMessage(config.MsgTypePong, "", c.Nickname)
			c.SendChan <- pongMsg
			continue
		}

		if msg.Type == config.MsgTypeMediaInfo || msg.Type == config.MsgTypeFileOffer {
			parts := strings.SplitN(msg.Content, "|", 3)
			if len(parts) == 3 {
				name := parts[0]
				onion := parts[1]
				token := parts[2]
				
				c.mu.Lock()
				if c.AvailableFiles == nil {
					c.AvailableFiles = make(map[string]OnionInfo)
				}
				id := fmt.Sprintf("%d", len(c.AvailableFiles))
				c.AvailableFiles[id] = OnionInfo{
					Name: name,
					Onion: onion,
					Token: token,
				}
				c.mu.Unlock()
				
				c.sendUpdate(&protocol.ChatMessage{
					Type:    config.MsgTypeSystem,
					Sender:  "SYSTEM",
					Content: fmt.Sprintf("📎 %s shared a file: %s (ID: %s) - Type /download %s to get it.", msg.Sender, name, id, id),
				})

				// Auto-Download if not sender
				if msg.Sender != c.Nickname {
					go func(fileID string) {
						time.Sleep(2 * time.Second) // Small delay to let Tor stabilize
						_ = c.DownloadSharedFile(fileID, "downloads")
					}(id)
				}
			}
			continue
		}

		c.sendUpdate(msg)
	}
}

// sendUpdate sends to UpdateChan without blocking — drops if channel is full.
func (c *Client) sendUpdate(msg *protocol.ChatMessage) {
	select {
	case c.UpdateChan <- msg:
	default:
		// TUI is too slow; discard rather than block the read loop.
	}
}

func (c *Client) ShareKeyWith(targetNick string) {
	targetE2E, ok := c.UserKeys[targetNick]
	if ok {
		shared, _ := crypto.DeriveSharedKey(c.E2EPriv, targetE2E)
		initialKey := c.MyRatchet.GetInitialKey()
		enc, _ := crypto.Encrypt(shared, initialKey)
		
		msg := protocol.CreateMessage(config.MsgTypeKeyShare, targetNick+"|"+crypto.Base64Encode(enc), c.Nickname)
		msg.IsE2Ee = true
		c.SendChan <- msg
	}
}

func (c *Client) writePump() {
	for chatMsg := range c.SendChan {
		if chatMsg.Type == config.MsgTypeChat {
			msgKey, step := c.MyRatchet.Next()
			encContent, _ := crypto.Encrypt(msgKey, []byte(chatMsg.Content))
			chatMsg.Content = crypto.Base64Encode(encContent)
			chatMsg.RatchetStep = step
		}

		jsonBytes, _ := chatMsg.Encode()
		enc, _ := crypto.Encrypt(c.SessionKey, jsonBytes)
		protocol.SendFrame(c.Conn, []byte(crypto.Base64Encode(enc)))
	}
}
