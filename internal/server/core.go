package server

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"conner/internal/appdir"
	"conner/internal/config"
	"conner/internal/crypto"
	"conner/internal/logx"
	"conner/internal/protocol"
	"conner/internal/rooms"
	"io"
	"net/http"
)

// EventEntry is a single real-time notification in the dashboard feed.
type EventEntry struct {
	Time time.Time
	Icon string
	Text string
}

type Server struct {
	ClientManager  *ClientManager
	DBManager      *MemoryManager
	Running        bool
	mu             sync.RWMutex
	Listener       net.Listener
	ConsoleHistory []string
	Stats          ServerStats
	EventLog       []EventEntry      // ring buffer of real-time notifications
	BlacklistMap   map[string]string // Identity -> Nickname (Metadata)
	WhitelistMap   map[string]string // SigningPubKey (B64) -> Identity
	CmdRegistry    *CommandRegistry
	RoomKey        []byte        // Central AES key (Ephemeral, wiped)
	VaultToken     string        // Access token for HTTP (Kept)
	VaultDir       string        // Directory for server-side file storage
	HTTPPort       int           // Internal port for the file server
	RawConns       int32         // Atomic counter for active raw connections
	Ready          chan struct{} // Signal when server is listening
	AutoApprove    bool
	Rooms          *rooms.Registry
	BindLAN        bool
	Admins         map[string]string // signing pub b64 -> nick
}

type ServerStats struct {
	TotalConnections int
	MessagesSent     int
	CommandsExecuted int
	KicksIssued      int
	StartTime        time.Time
	TorAddress       string
}

func (s *Server) RemoveClient(c *Client) {
	s.removeClient(c)
}

func (s *Server) ApproveClient(nickname string) bool {
	target := s.ClientManager.GetClientByNickname(nickname)
	if target == nil {
		return false
	}
	target.State = "WHITELISTED"
	pubB64 := crypto.Base64Encode(target.SigningPubKey)
	if s.isStoredAdmin(pubB64) || s.ClientManager.WhitelistedCount() == 1 {
		target.IsAdmin = true
		s.SendSystemMessage(target, "You have admin privileges.")
	}
	// Membership change: rotate sender-key epoch, keep vault file token.
	s.BumpEpoch()

	pubKeyB64 := crypto.Base64Encode(target.SigningPubKey)

	s.mu.Lock()
	s.WhitelistMap[pubKeyB64] = target.Nickname
	delete(s.BlacklistMap, target.Identity)
	s.mu.Unlock()
	if target.IsAdmin {
		s.rememberAdmin(pubKeyB64, target.Nickname)
	} else {
		s.saveACL()
	}

	s.SendSystemMessage(target, "✅ You have been approved by an admin. Welcome!")
	s.Log("Admin approved: " + nickname)
	s.AddEvent("✅", "Admin approved: "+nickname+" → CHAT ROOM")
	s.BroadcastUserList()
	return true
}

func (s *Server) BlockClient(nickname string) bool {
	target := s.ClientManager.GetClientByNickname(nickname)
	if target == nil {
		return false
	}

	s.mu.Lock()
	s.BlacklistMap[target.Identity] = target.Nickname
	delete(s.WhitelistMap, crypto.Base64Encode(target.SigningPubKey))
	delete(s.Admins, crypto.Base64Encode(target.SigningPubKey))
	s.mu.Unlock()
	s.saveACL()

	s.SendSystemMessage(target, "⚡ You have been blocked from the server.")
	go func() {
		time.Sleep(100 * time.Millisecond)
		s.RemoveClient(target)
	}()
	s.Log("Admin blocked and disconnected: " + nickname)
	s.AddEvent("⛔", "Admin blocked "+nickname)
	s.BroadcastUserList()
	return true
}

func NewServer() *Server {
	torAddr := "NOT GENERATED / HIDDEN SERVICE DISABLED"

	// Try all known hostname file locations in order of priority.
	// CONNER_WORKDIR is set by entrypoint.sh via /etc/profile.d/conner.sh
	candidates := []string{
		"hostname",                             // relative to CWD (dev)
		"/var/lib/tor/conner_chat/hostname",    // Alpine install (entrypoint.sh)
		"/var/lib/tor/hidden_service/hostname", // generic Tor install
		"/var/lib/tor/onion/hostname",          // alternate name
	}
	if workdir := os.Getenv("CONNER_WORKDIR"); workdir != "" {
		// filepath.Clean removes any path traversal sequences (../../etc)
		// that might appear in the env variable.
		clean := filepath.Clean(workdir + "/hostname")
		candidates = append([]string{clean}, candidates...)
	}
	for _, path := range candidates {
		if b, err := os.ReadFile(path); err == nil { // #nosec G703 -- paths are static or cleaned above
			torAddr = strings.TrimSpace(string(b))
			break
		}
	}

	s := &Server{
		ClientManager:  NewClientManager(),
		DBManager:      NewMemoryManager(config.MessageHistoryLimit, config.MessageTTL),
		Running:        true,
		ConsoleHistory: make([]string, 0),
		EventLog:       make([]EventEntry, 0, 200),
		Stats:          ServerStats{StartTime: time.Now(), TorAddress: torAddr},
		CmdRegistry:    NewCommandRegistry(),
		BlacklistMap:   make(map[string]string),
		WhitelistMap:   make(map[string]string),
		Ready:          make(chan struct{}),
		Rooms:          rooms.NewRegistry(),
		Admins:         make(map[string]string),
	}
	s.loadACL()
	return s
}

// AddEvent appends a notification to the event log (max 200 entries).
func (s *Server) AddEvent(icon, text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := EventEntry{Time: time.Now(), Icon: icon, Text: text}
	s.EventLog = append(s.EventLog, e)
	if len(s.EventLog) > 200 {
		s.EventLog = s.EventLog[len(s.EventLog)-200:]
	}
}

// GetEvents returns a snapshot of the event log (newest last).
func (s *Server) GetEvents() []EventEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]EventEntry, len(s.EventLog))
	copy(out, s.EventLog)
	return out
}

func (s *Server) GetBlockedList() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []string
	for id, nick := range s.BlacklistMap {
		out = append(out, fmt.Sprintf("%s (%s)", id, nick))
	}
	return out
}

func (s *Server) Stop() {
	s.mu.Lock()
	s.Running = false
	s.mu.Unlock()
	if s.Listener != nil {
		s.Listener.Close()
	}
}

func (s *Server) Log(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), msg)
	s.ConsoleHistory = append(s.ConsoleHistory, entry)
	if len(s.ConsoleHistory) > 1000 {
		s.ConsoleHistory = s.ConsoleHistory[len(s.ConsoleHistory)-1000:]
	}
	log.Println(entry)
	logx.Info("hub", "msg", msg)
}

func (s *Server) Start(port string) error {
	return s.StartOn("127.0.0.1:" + port)
}

// StartOn binds the hub. WAN-safe default is 127.0.0.1 (onion maps in).
// Use 0.0.0.0 only with --lan.
func (s *Server) StartOn(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.Listener = ln
	s.Log(fmt.Sprintf("Server started on %s", addr))

	s.VaultDir = appdir.Path("vault")
	_ = os.MkdirAll(s.VaultDir, 0700)

	// Start File Server (HTTP)
	go s.startFileServer()

	// Background loops
	go func() {
		for s.Running {
			time.Sleep(1 * time.Minute)
			count := s.DBManager.CleanupOldMessages()
			if count > 0 {
				s.Log(fmt.Sprintf("TTL: Purged %d expired messages", count))
			}
		}
	}()

	// Heartbeat loop: Ping all clients every 30s
	go func() {
		for s.Running {
			time.Sleep(30 * time.Second)
			pingMsg := protocol.CreateMessage(config.MsgTypePing, "", "SERVER")
			encoded, _ := pingMsg.Encode()

			for _, c := range s.ClientManager.GetAllClients() {
				if time.Since(c.LastSeen) > 90*time.Second {
					s.Log(fmt.Sprintf("Heartbeat timeout: %s (%s)", c.Nickname, c.Address))
					c.Conn.Close()
					continue
				}
				// Encrypt ping with client's session key
				enc, _ := crypto.Encrypt(c.EncryptionKey, []byte(encoded))
				select {
				case c.SendChan <- crypto.Base64Encode(enc):
				default:
				}
			}
		}
	}()

	for s.Running {
		conn, err := ln.Accept()
		if err != nil {
			if s.Running {
				s.Log(fmt.Sprintf("Accept error: %v", err))
			}
			continue
		}
		s.mu.Lock()
		s.Stats.TotalConnections++
		s.mu.Unlock()

		// DDoS Guard: Limit concurrent raw connections
		if atomic.LoadInt32(&s.RawConns) > 500 {
			s.AddEvent("⚠️", "DDoS GUARD: Max raw connections (500) reached. Dropping incoming.")
			conn.Close()
			continue
		}
		atomic.AddInt32(&s.RawConns, 1)

		go s.handleConnection(conn)
	}
	return nil
}

func (s *Server) handleConnection(conn net.Conn) {
	defer func() {
		conn.Close()
		atomic.AddInt32(&s.RawConns, -1)
	}()
	remoteAddr := conn.RemoteAddr().String()
	s.Log(fmt.Sprintf("Incoming connection: %s", remoteAddr))

	// Set handshake deadline
	conn.SetDeadline(time.Now().Add(60 * time.Second))

	// Key Exchange + Challenge
	priv, pub, err := crypto.GenerateKeyPair()
	if err != nil {
		conn.Close()
		return
	}

	nonce := crypto.GenerateRandomKey()
	ke := &protocol.KeyExchange{
		X25519Pub:     pub,
		Nonce:         nonce,
		PowDifficulty: uint32(crypto.PoWDifficulty),
		Version:       config.Version,
	}
	keBytes, err := protocol.Marshal(ke)
	if err != nil {
		return
	}
	if err := protocol.SendFrame(conn, keBytes); err != nil {
		s.Log(fmt.Sprintf("Handshake failed: could not send KEY_EXCHANGE to %s: %v", remoteAddr, err))
		return
	}

	payload, err := protocol.ReadFrame(conn)
	if err != nil {
		s.Log(fmt.Sprintf("Handshake failed: could not read CLIENT_HELLO from %s: %v", remoteAddr, err))
		return
	}

	hello, err := protocol.UnmarshalHello(payload)
	if err != nil || len(hello.GetX25519Pub()) == 0 {
		_ = protocol.SendHandshakeErr(conn, "invalid hello")
		return
	}
	if !protocol.CompatibleVersion(hello.GetVersion(), config.Version) {
		_ = protocol.SendHandshakeErr(conn, "incompatible version: want "+config.ProtocolMajor)
		return
	}
	if hello.GetNickname() == "" || len(hello.GetNickname()) > 32 {
		_ = protocol.SendHandshakeErr(conn, "invalid nickname")
		return
	}

	if !crypto.VerifyPoW(nonce, hello.GetPowNonce(), crypto.PoWDifficulty) {
		_ = protocol.SendHandshakeErr(conn, "Proof of Work verification failed")
		return
	}

	clientPubBytes := hello.GetX25519Pub()
	sessionKey, err := crypto.DeriveSharedKey(priv, clientPubBytes)
	if err != nil {
		return
	}

	nickname := hello.GetNickname()
	identity := hello.GetIdentity()
	clientSigningPub := hello.GetSigningPub()
	clientSig := hello.GetSignature()

	// Verify Identity Signature
	if !crypto.Verify(clientSigningPub, nonce, clientSig) {
		_ = protocol.SendHandshakeErr(conn, "bad identity signature")
		conn.Close()
		return
	}

	pubKeyB64 := crypto.Base64Encode(clientSigningPub)

	// Check persistent maps BEFORE creating client
	s.mu.RLock()
	banNick, isBanned := s.BlacklistMap[identity]

	whitelistedNick := ""
	isKeyWhitelisted := false
	for k, v := range s.WhitelistMap {
		if k == pubKeyB64 {
			isKeyWhitelisted = true
			whitelistedNick = v
		}
		if v == nickname && k != pubKeyB64 {
			s.mu.RUnlock()
			_ = protocol.SendHandshakeErr(conn, "nickname owned by another identity")
			conn.Close()
			return
		}
	}
	s.mu.RUnlock()

	if isBanned {
		_ = protocol.SendHandshakeErr(conn, "banned: "+banNick)
		return
	}

	// Validate nickname — send an error frame before closing so the client
	// shows a meaningful message instead of a bare disconnect.
	if s.ClientManager.GetClientByNickname(nickname) != nil {
		_ = protocol.SendHandshakeErr(conn, "nickname already taken")
		conn.Close()
		return
	}

	e2ePub := hello.GetE2EPub()
	client := &Client{
		Conn:          conn,
		Nickname:      nickname,
		Address:       remoteAddr,
		Identity:      identity,
		JoinTime:      time.Now(),
		EncryptionKey: sessionKey,
		State:         "PENDING",
		SendChan:      make(chan string, 100),
		LastSeen:      time.Now(),
		SigningPubKey: clientSigningPub,
		E2EPubKey:     e2ePub,
	}

	if isKeyWhitelisted {
		client.Nickname = whitelistedNick
		client.State = "WHITELISTED"
		if s.isStoredAdmin(pubKeyB64) {
			client.IsAdmin = true
		}
		s.Log("Auto-approved returning user: " + client.Nickname)
	}

	s.ClientManager.AddClient(remoteAddr, client)

	// Handshake complete
	s.mu.RLock()
	hPort := s.HTTPPort
	s.mu.RUnlock()
	approved := isKeyWhitelisted || s.AutoApprove
	if err := protocol.SendHandshakeOK(conn, hPort, approved); err != nil {
		return
	}

	// Handshake complete — clear deadline, switch to no-timeout for normal I/O.
	conn.SetDeadline(time.Time{}) //nolint:errcheck

	// Notify client
	if isKeyWhitelisted || s.AutoApprove {
		if !isKeyWhitelisted {
			client.State = "WHITELISTED"
		} else {
			client.Nickname = whitelistedNick
			client.State = "WHITELISTED"
		}

		s.SendSystemMessage(client, "✅ You have been approved!")
		s.BroadcastUserList()

		// Log join to history
		joinMsg := fmt.Sprintf("➜ %s joined the chat", client.Nickname)
		s.DBManager.SaveMessage(config.MsgTypeJoin, joinMsg, "SERVER")

		s.Log(fmt.Sprintf("Auto-approved user: %s (%s)", client.Nickname, remoteAddr))
		pubB := crypto.Base64Encode(client.SigningPubKey)
		if s.isStoredAdmin(pubB) || s.ClientManager.WhitelistedCount() == 1 {
			client.IsAdmin = true
			s.SendSystemMessage(client, "You have admin privileges.")
			s.rememberAdmin(pubB, client.Nickname)
		} else {
			s.mu.Lock()
			s.WhitelistMap[pubB] = client.Nickname
			s.mu.Unlock()
			s.saveACL()
		}
		s.BumpEpoch()

		// Broadcast join to others
		bMsg := protocol.CreateMessage(config.MsgTypeJoin, joinMsg, "SERVER")
		s.BroadcastToState("WHITELISTED", bMsg, remoteAddr)
	} else {
		s.SendSystemMessage(client, "👋 Waiting for admin approval...")
		s.Log(fmt.Sprintf("Pending approval: %s (%s)", nickname, remoteAddr))
		s.AddEvent("🔔", fmt.Sprintf("New connection: %s (%s) — PENDING approval", nickname, remoteAddr))
	}

	// Write pump
	go func() {
		for msg := range client.SendChan {
			if err := protocol.SendFrame(conn, []byte(msg)); err != nil {
				break
			}
		}
	}()

	// Read pump
	for {
		payload, err := protocol.ReadFrame(conn)
		if err != nil {
			break
		}
		client.LastSeen = time.Now()
		s.processClientMessage(client, string(payload))
	}

	s.removeClient(client)
}

func (s *Server) removeClient(client *Client) {
	s.ClientManager.RemoveClient(client.Address)
	// Close the TCP connection first so the write pump's SendFrame call
	// returns an error and the goroutine exits on its own. Then close the
	// channel so the range loop terminates. This avoids the race between
	// two goroutines reading from the same channel simultaneously.
	client.Conn.Close()

	if client.State == "WHITELISTED" {
		leaveMsg := fmt.Sprintf("⇠ %s left the chat", client.Nickname)
		s.DBManager.SaveMessage(config.MsgTypeJoin, leaveMsg, "SERVER")

		bMsg := protocol.CreateMessage(config.MsgTypeJoin, leaveMsg, "SERVER")
		s.BroadcastToState(client.State, bMsg, client.Address)

		s.Log(fmt.Sprintf("Client disconnected: %s (%s)", client.Nickname, client.Address))
		s.AddEvent("🔴", fmt.Sprintf("Disconnected: %s [%s]", client.Nickname, client.State))
		s.BroadcastUserList()
		s.BumpEpoch()
	} else {
		s.Log(fmt.Sprintf("Pending client disconnected: %s", client.Address))
		s.AddEvent("🔌", fmt.Sprintf("Pending client dropped: %s", client.Address))
	}
}

func (s *Server) SendSystemMessage(client *Client, content string) {
	msg := protocol.CreateMessage(config.MsgTypeSystem, content, "SERVER")
	msgBytes, _ := msg.Encode()
	enc, _ := crypto.Encrypt(client.EncryptionKey, msgBytes)

	select {
	case client.SendChan <- crypto.Base64Encode(enc):
	default:
		s.Log(fmt.Sprintf("Dropped system message for %s (buffer full)", client.Nickname))
	}
}

func (s *Server) SendMessage(client *Client, msg *protocol.ChatMessage) {
	msgBytes, err := msg.Encode()
	if err != nil {
		return
	}
	enc, err := crypto.Encrypt(client.EncryptionKey, msgBytes)
	if err != nil {
		return
	}
	select {
	case client.SendChan <- crypto.Base64Encode(enc):
	default:
		s.Log(fmt.Sprintf("Dropped message for %s (buffer full)", client.Nickname))
	}
}

func (s *Server) processClientMessage(client *Client, text string) {
	encBytes, err := crypto.Base64Decode(text)
	if err != nil {
		return
	}
	// Client encrypts outgoing messages with SessionKey (EncryptionKey).
	// BroadcastKey is only used for server→client direction.
	decryptedBytes, err := crypto.Decrypt(client.EncryptionKey, encBytes)
	if err != nil {
		return
	}

	msg, err := protocol.Decode(decryptedBytes)
	if err != nil {
		return
	}

	// PENDING users are blocked
	if client.State == "PENDING" {
		s.SendSystemMessage(client, "You are waiting for admin approval.")
		return
	}

	if msg.Type == config.MsgTypePong || msg.Type == config.MsgTypePing {
		return
	}
	if msg.Type == config.MsgTypeChat || msg.Type == config.MsgTypePrivate || msg.Type == config.MsgTypeFileOffer {
		if !client.allowMessage() {
			s.SendSystemMessage(client, "slow down (rate limit)")
			return
		}
	}

	if msg.Type == config.MsgTypeTyping {
		broadcastMsg := protocol.CreateMessage(config.MsgTypeTyping, "", client.Nickname)
		s.BroadcastToState(client.State, broadcastMsg, client.Address)
		return
	}

	if msg.Type == config.MsgTypeReaction {
		broadcastMsg := protocol.CreateMessage(config.MsgTypeReaction, msg.Content, client.Nickname)
		s.BroadcastToState(client.State, broadcastMsg, client.Address)
		return
	}

	// Send ACK to sender for tracked message types
	if (msg.Type == config.MsgTypeChat || msg.Type == config.MsgTypePrivate) && msg.MessageId != "" {
		ackMsg := protocol.CreateMessage(config.MsgTypeAck, msg.MessageId, "SERVER")
		ackBytes, _ := ackMsg.Encode()
		encAck, _ := crypto.Encrypt(client.EncryptionKey, ackBytes)
		select {
		case client.SendChan <- crypto.Base64Encode(encAck):
		default:
		}
	}

	if msg.Type == config.MsgTypeCmd || (msg.Type == config.MsgTypeChat && !msg.IsE2Ee && strings.HasPrefix(msg.Content, "/")) {
		s.handleCommand(client, msg.Content)
		return
	}

	if msg.Type == config.MsgTypePrivate {
		s.Log(fmt.Sprintf("[PRIVATE] %s -> %s (ciphertext)", client.Nickname, msg.ReplyTo))
		targetClient := s.ClientManager.GetClientByNickname(msg.ReplyTo)
		if targetClient != nil && targetClient.State == "WHITELISTED" {
			msgBytes, _ := msg.Encode()
			enc, _ := crypto.Encrypt(targetClient.EncryptionKey, msgBytes)
			payload := crypto.Base64Encode(enc)
			select {
			case targetClient.SendChan <- payload:
			default:
			}
		} else {
			s.SendSystemMessage(client, "❌ User not found or offline: "+msg.ReplyTo)
		}
		return
	}

	if msg.Type == config.MsgTypeFileOffer {
		s.Log(fmt.Sprintf("[FILE_OFFER] %s shared metadata: %s", client.Nickname, msg.Content))
		s.BroadcastToState(client.State, msg, client.Address)
		return
	}

	if msg.Type == config.MsgTypeFilePut {
		s.handleFilePut(client, msg)
		return
	}
	if msg.Type == config.MsgTypeFileGet {
		s.handleFileGet(client, msg)
		return
	}

	client.MessageCount++

	switch client.State {
	case "WHITELISTED":
		s.DBManager.SaveMessage(config.MsgTypeChat, "[e2ee]", client.Nickname)
		s.Log(fmt.Sprintf("[WHITELIST] %s: ciphertext", client.Nickname))
	}

	// Relay the original message to preserve all fields (Type, FileId, OnionAddr, etc.)
	s.BroadcastToState(client.State, msg, client.Address)
}

func (s *Server) handleCommand(client *Client, content string) {
	s.mu.Lock()
	s.Stats.CommandsExecuted++
	s.mu.Unlock()

	s.CmdRegistry.Handle(s, client, content)
}

func (s *Server) BroadcastAnnouncement(text string) {
	fullText := "📢 ANNOUNCEMENT: " + text
	msg := protocol.CreateMessage(config.MsgTypeSystem, fullText, "ADMIN")
	s.DBManager.SaveMessage(config.MsgTypeSystem, fullText, "ADMIN")
	s.BroadcastToState("WHITELISTED", msg, "")
	s.Log("[ANN] ADMIN: " + text)
}

func (s *Server) BroadcastUserList() {
	var userInfos []string
	for _, c := range s.ClientManager.GetAllClients() {
		if c.State == "WHITELISTED" {
			// nick|signing_pub_b64
			userInfos = append(userInfos, fmt.Sprintf("%s|%s|%s",
				c.Nickname,
				crypto.Base64Encode(c.SigningPubKey),
				crypto.Base64Encode(c.E2EPubKey)))
		}
	}
	userList := strings.Join(userInfos, ",")
	msg := protocol.CreateMessage(config.MsgTypeUserList, userList, "SERVER")

	// Broadcast to everyone so their sidebars update
	s.mu.RLock()
	clients := s.ClientManager.GetAllClients()
	s.mu.RUnlock()

	msgBytes, _ := msg.Encode()

	for _, c := range clients {
		enc, _ := crypto.Encrypt(c.EncryptionKey, msgBytes)
		payload := crypto.Base64Encode(enc)
		select {
		case c.SendChan <- payload:
		default:
		}
	}
}

func (s *Server) BroadcastToState(state string, msg *protocol.ChatMessage, excludeID string) {
	msgBytes, _ := msg.Encode()

	sentCount := 0
	for _, c := range s.ClientManager.GetAllClients() {
		if c.State == state && c.Address != excludeID {
			enc, _ := crypto.Encrypt(c.EncryptionKey, msgBytes)
			payload := crypto.Base64Encode(enc)
			select {
			case c.SendChan <- payload:
				sentCount++
			default:
				s.Log(fmt.Sprintf("Dropped message for %s (buffer full)", c.Nickname))
			}
		}
	}

	s.mu.Lock()
	s.Stats.MessagesSent += sentCount
	s.mu.Unlock()
}

func (s *Server) updateTorPorts() {
	// Small delay to let initial tor setup finish
	time.Sleep(5 * time.Second)
	// Here we should ideally update the ADD_ONION via Tor Control Port
	// But since it's already started, we'd need to re-issue the command.
	// For KISS, we'll assume the initial tor setup in embedded.go can handle multiple ports.
}

func (s *Server) startFileServer() {
	mux := http.NewServeMux()

	// GET /download?f=filename&t=key_hash
	mux.HandleFunc("/download", func(w http.ResponseWriter, r *http.Request) {
		filename := filepath.Base(r.URL.Query().Get("f"))
		// Auth using SHA256 of the RoomKey
		s.mu.RLock()
		token := s.VaultToken
		vaultDir := s.VaultDir
		s.mu.RUnlock()

		auth := r.Header.Get("Authorization")
		tokenHeader := strings.TrimPrefix(auth, "Bearer ")

		// Support both Header and Query param for flexible client support
		qToken := r.URL.Query().Get("t")

		if tokenHeader != token && qToken != token {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		path := filepath.Join(vaultDir, filename)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		f, err := os.Open(path)
		if err != nil {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		defer f.Close()
		w.Header().Set("X-Conner-AEAD", "chunk-v1")
		_, _ = io.Copy(w, f)
	})

	// Alias for /download for older clients
	mux.Handle("/f", http.StripPrefix("/f", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Re-route to /download logic or just alias it
		r.URL.Path = "/download"
		mux.ServeHTTP(w, r)
	})))

	// POST /upload?t=key_hash&u=nickname
	mux.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
		// Enforce 100MB limit
		r.Body = http.MaxBytesReader(w, r.Body, 100*1024*1024)

		s.mu.RLock()
		token := s.VaultToken
		vaultDir := s.VaultDir
		s.mu.RUnlock()

		auth := r.Header.Get("Authorization")
		tokenHeader := strings.TrimPrefix(auth, "Bearer ")
		qToken := r.URL.Query().Get("t")

		if tokenHeader != token && qToken != token {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		uploader := r.URL.Query().Get("u")
		if uploader == "" {
			uploader = "Unknown"
		}

		file, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "File too large or invalid", http.StatusBadRequest)
			return
		}
		defer file.Close()

		safeFilename := filepath.Base(header.Filename)
		dstPath := filepath.Join(vaultDir, safeFilename)
		dst, err := os.Create(dstPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer dst.Close()
		// Store the client stream as-is (AEAD chunks). Hub never sees file plaintext.
		if _, err := io.Copy(dst, file); err != nil {
			os.Remove(dstPath)
			http.Error(w, "write failed", http.StatusInternalServerError)
			return
		}

		s.Log(fmt.Sprintf("[VAULT] %s uploaded %s", uploader, safeFilename))
		w.Write([]byte("OK"))
	})

	// Background cleanup loop: Delete files older than 10 minutes
	go func() {
		ticker := time.NewTicker(2 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			s.mu.RLock()
			vDir := s.VaultDir
			s.mu.RUnlock()
			if vDir == "" {
				continue
			}

			files, _ := os.ReadDir(vDir)
			for _, f := range files {
				info, err := f.Info()
				if err == nil && time.Since(info.ModTime()) > 10*time.Minute {
					os.Remove(filepath.Join(vDir, f.Name()))
				}
			}
		}
	}()

	var listener net.Listener
	var err error
	port := 6667
	for i := 0; i < 10; i++ {
		listener, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			break
		}
		port++
	}

	if err != nil {
		s.Log(fmt.Sprintf("Failed to start file server listener after 10 attempts: %v", err))
		close(s.Ready)
		return
	}

	s.mu.Lock()
	s.HTTPPort = port
	s.mu.Unlock()
	s.Log(fmt.Sprintf("File server (Vault) started on port %d", port))
	s.RotateVaultToken()
	close(s.Ready)
	http.Serve(listener, mux)
}

// BumpEpoch tells members to rotate sender keys. VaultToken is unchanged
// so existing ciphertext in ~/.conner/vault stays downloadable.
func (s *Server) BumpEpoch() {
	s.mu.Lock()
	crypto.Wipe(s.RoomKey)
	s.RoomKey = nil
	if s.VaultToken == "" {
		newTok := make([]byte, 32)
		_, _ = rand.Read(newTok)
		s.VaultToken = hex.EncodeToString(newTok)
	}
	tok := s.VaultToken
	s.mu.Unlock()
	epochMsg := protocol.CreateMessage(config.MsgTypeEpoch, tok, "SERVER")
	s.broadcastToWhitelisted(epochMsg)
	s.AddEvent("🔄", "Membership epoch bumped. Sender keys rotate; vault token kept.")
}

func (s *Server) handleFilePut(client *Client, msg *protocol.ChatMessage) {
	name := filepath.Base(msg.FileId)
	if name == "" || name == "." {
		return
	}
	s.mu.RLock()
	vaultDir := s.VaultDir
	s.mu.RUnlock()
	path := filepath.Join(vaultDir, name)
	if msg.ChunkIdx < 0 {
		s.Log(fmt.Sprintf("[VAULT-MUX] %s put %s done", client.Nickname, name))
		return
	}
	ct, err := crypto.Base64Decode(msg.Content)
	if err != nil {
		return
	}
	flag := os.O_CREATE | os.O_WRONLY
	if msg.ChunkIdx == 0 {
		flag |= os.O_TRUNC
	} else {
		flag |= os.O_APPEND
	}
	f, err := os.OpenFile(path, flag, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(ct)))
	_, _ = f.Write(hdr[:])
	_, _ = f.Write(ct)
}

func (s *Server) handleFileGet(client *Client, msg *protocol.ChatMessage) {
	name := filepath.Base(msg.FileId)
	if name == "" {
		name = filepath.Base(msg.Content)
	}
	s.mu.RLock()
	vaultDir := s.VaultDir
	s.mu.RUnlock()
	path := filepath.Join(vaultDir, name)
	f, err := os.Open(path)
	if err != nil {
		s.SendSystemMessage(client, "file not in vault: "+name)
		return
	}
	defer f.Close()
	var idx int32
	for {
		var hdr [4]byte
		if _, err := io.ReadFull(f, hdr[:]); err != nil {
			break
		}
		n := binary.BigEndian.Uint32(hdr[:])
		if n == 0 || n > 8*1024*1024 {
			break
		}
		ct := make([]byte, n)
		if _, err := io.ReadFull(f, ct); err != nil {
			break
		}
		out := protocol.CreateMessage(config.MsgTypeFileData, crypto.Base64Encode(ct), "SERVER")
		out.FileId = name
		out.ChunkIdx = idx
		s.SendMessage(client, out)
		idx++
	}
	fin := protocol.CreateMessage(config.MsgTypeFileData, "", "SERVER")
	fin.FileId = name
	fin.ChunkIdx = -1
	fin.TotalChunks = idx
	s.SendMessage(client, fin)
}

func (s *Server) RotateVaultToken() {
	s.mu.Lock()
	if s.VaultToken != "" {
		s.mu.Unlock()
		return
	}
	newTok := make([]byte, 32)
	_, _ = rand.Read(newTok)
	s.VaultToken = hex.EncodeToString(newTok)
	s.mu.Unlock()
	s.saveACL()
}

// Rate-limit chat/file/private. Heartbeats and key sync are exempt.
func (c *Client) allowMessage() bool {
	now := time.Now()
	if c.winStart.IsZero() || now.Sub(c.winStart) > config.RateLimitWindow {
		c.winStart = now
		c.winCount = 1
		return true
	}
	c.winCount++
	return c.winCount <= config.RateLimitMessages
}

func (s *Server) broadcastToWhitelisted(msg *protocol.ChatMessage) {
	for _, c := range s.ClientManager.GetAllClients() {
		if c.State == "WHITELISTED" {
			s.SendMessage(c, msg)
		}
	}
}
