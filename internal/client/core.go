package client

import (
	"fmt"
	"net"
	"os/exec"
	"strings"
	"time"

	"golang.org/x/net/proxy"

	"conner/internal/config"
	"conner/internal/crypto"
	"conner/internal/protocol"
)

const torSOCKS5 = "127.0.0.1:9050"

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

type Client struct {
	Conn               net.Conn
	SessionKey         []byte
	BroadcastKey       []byte // shared group key, received from server after handshake
	Messages           []string
	SendChan           chan protocol.ChatMessage
	UpdateChan         chan protocol.ChatMessage // typed messages for rich TUI rendering
	PendingDownloadDir chan string               // TUI pushes destDir before sending DOWNLOAD_REQ
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
		return nil, fmt.Errorf("handshake: KEY_EXCHANGE read failed: %w", err)
	}
	line := string(payload)
	if !strings.HasPrefix(line, "KEY_EXCHANGE:") {
		return nil, fmt.Errorf("handshake: expected KEY_EXCHANGE, got: %s", line)
	}
	serverPub, _ := crypto.Base64Decode(strings.TrimSpace(strings.Split(line, ":")[1]))
	priv, pub, _ := crypto.GenerateKeyPair()
	sessionKey, _ := crypto.DeriveSharedKey(priv, serverPub)

	// 2. Send CLIENT_HELLO
	hello := fmt.Sprintf("CLIENT_HELLO:%s:%s", crypto.Base64Encode(pub), nickname)
	if err := protocol.SendFrame(conn, []byte(hello)); err != nil {
		return nil, fmt.Errorf("handshake: CLIENT_HELLO send failed: %w", err)
	}

	// 3. Receive BROADCAST_KEY (encrypted with our session key)
	bkPayload, err := protocol.ReadFrame(conn)
	if err != nil {
		return nil, fmt.Errorf("handshake: BROADCAST_KEY read failed: %w", err)
	}
	bkLine := string(bkPayload)
	if !strings.HasPrefix(bkLine, "BROADCAST_KEY:") {
		return nil, fmt.Errorf("handshake: expected BROADCAST_KEY, got: %s", bkLine)
	}
	encBK, err := crypto.Base64Decode(strings.SplitN(bkLine, ":", 2)[1])
	if err != nil {
		return nil, fmt.Errorf("handshake: BROADCAST_KEY decode failed: %w", err)
	}
	broadcastKey, err := crypto.Decrypt(sessionKey, encBK)
	if err != nil {
		return nil, fmt.Errorf("handshake: BROADCAST_KEY decrypt failed: %w", err)
	}

	client := &Client{
		Conn:               conn,
		SessionKey:         sessionKey,
		BroadcastKey:       broadcastKey,
		Messages:           []string{},
		SendChan:           make(chan protocol.ChatMessage, 10),
		UpdateChan:         make(chan protocol.ChatMessage, 200),
		PendingDownloadDir: make(chan string, 10), // buffered so TUI never blocks
	}

	go client.readPump()
	go client.writePump()

	return client, nil
}

func (c *Client) readPump() {
	defer func() {
		// Always notify the TUI when the connection ends.
		c.sendUpdate(protocol.ChatMessage{
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
			c.sendUpdate(protocol.ChatMessage{
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

		// Decrypt with BroadcastKey — all server broadcasts use this shared key.
		dec, err := crypto.Decrypt(c.BroadcastKey, enc)
		if err != nil {
			continue
		}

		msg, err := protocol.FromJSON(string(dec))
		if err != nil {
			continue
		}

		if msg.Type == config.MsgTypeMediaData {
			parts := strings.SplitN(msg.Content, "|", 2)
			if len(parts) == 2 {
				// Use the dir the TUI pushed for this request;
				// fall back to "downloads" if nothing was queued.
				destDir := "downloads"
				select {
				case d := <-c.PendingDownloadDir:
					destDir = d
				default:
				}
				savedPath, extractErr := ExtractMediaTarBase64(parts[1], destDir)
				if extractErr == nil {
					c.sendUpdate(protocol.ChatMessage{
						Type:    config.MsgTypeSystem,
						Sender:  "SERVER",
						Content: "✅ File saved: " + savedPath,
					})
				} else {
					c.sendUpdate(protocol.ChatMessage{
						Type:    config.MsgTypeSystem,
						Sender:  "SERVER",
						Content: "extract failed: " + extractErr.Error(),
					})
				}
			}
			continue
		}

		c.sendUpdate(msg)
	}
}

// sendUpdate sends to UpdateChan without blocking — drops if channel is full.
func (c *Client) sendUpdate(msg protocol.ChatMessage) {
	select {
	case c.UpdateChan <- msg:
	default:
		// TUI is too slow; discard rather than block the read loop.
	}
}

func (c *Client) writePump() {
	for chatMsg := range c.SendChan {
		jsonStr, _ := chatMsg.ToJSON()
		// Outgoing messages are encrypted with SessionKey (server decrypts per-client)
		enc, _ := crypto.Encrypt(c.SessionKey, []byte(jsonStr))
		protocol.SendFrame(c.Conn, []byte(crypto.Base64Encode(enc)))
	}
}
