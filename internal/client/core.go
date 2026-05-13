package client

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/proxy"

	"conner/internal/config"
	"conner/internal/crypto"
	"conner/internal/protocol"
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
	mu                 sync.Mutex
	UpdateChan         chan *protocol.ChatMessage // typed messages for rich TUI rendering
	// Security items
	SigningPriv        []byte                    // Ed25519 Private Key for identity
	SigningPub         []byte                    // Ed25519 Public Key for identity
	UserKeys           map[string][]byte          // Other users' Identity public keys
	IdentityStore      *IdentityStore
	RoomKey            []byte      // Centralized AES key (Content)
	VaultToken         string      // Server access token (Auth)
	ServerOnion        string      // Onion address of the hub
	UseTor             bool
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

func Connect(nickname, address string, useTor bool) (*Client, error) {
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
		UseTor:             useTor,
	}

	go client.readPump()
	go client.writePump()

	// 5. Heartbeat loop (30s) to keep Tor connection alive
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			c := client // use local closure
			c.mu.Lock()
			conn := c.Conn
			c.mu.Unlock()

			if conn == nil {
				return
			}

			ping := &protocol.ChatMessage{
				Type:   config.MsgTypePing,
				Sender: c.Nickname,
			}
			c.SendChan <- ping
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
				c.RoomKey = key
				c.VaultToken = hash
				c.mu.Unlock()
				c.sendUpdate(&protocol.ChatMessage{
					Type:    config.MsgTypeSystem,
					Sender:  "SECURITY",
					Content: "🔐 New Room Key synced. Access Token rotated.",
				})
			}
			continue
		}

		// Decrypt Group Messages (Chat)
		if (msg.Type == config.MsgTypeChat) && msg.Sender != c.Nickname {
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
		if chatMsg.Type == config.MsgTypeChat {
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


