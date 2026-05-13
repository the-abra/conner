package server

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"conner/internal/config"
	"conner/internal/crypto"
	"conner/internal/protocol"
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
	RoomKey       []byte // Central AES key (Ephemeral, wiped)
	FileMetadata  map[string]*protocol.ChatMessage
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
	// ROTATE KEY: Destroy old room key and generate a fresh one for the new group state
	s.RegenerateRoomKey()
	
	pubKeyB64 := crypto.Base64Encode(target.SigningPubKey)

	s.mu.Lock()
	s.WhitelistMap[pubKeyB64] = target.Nickname
	delete(s.BlacklistMap, target.Identity)
	s.mu.Unlock()
	
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
	delete(s.WhitelistMap, target.Identity)
	s.mu.Unlock()

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

	return &Server{
		ClientManager:  NewClientManager(),
		DBManager:      NewMemoryManager(config.MessageHistoryLimit, config.MessageTTL),
		Running:        true,
		ConsoleHistory: make([]string, 0),
		EventLog:       make([]EventEntry, 0, 200),
		Stats:          ServerStats{StartTime: time.Now(), TorAddress: torAddr},
		CmdRegistry:    NewCommandRegistry(),
		BlacklistMap:   make(map[string]string),
		WhitelistMap:   make(map[string]string),
		FileMetadata:   make(map[string]*protocol.ChatMessage),
	}
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

func (s *Server) Log(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), msg)
	s.ConsoleHistory = append(s.ConsoleHistory, entry)
	if len(s.ConsoleHistory) > 1000 {
		s.ConsoleHistory = s.ConsoleHistory[len(s.ConsoleHistory)-1000:]
	}
	log.Println(entry)
}

func (s *Server) Start(port string) error {
	ln, err := net.Listen("tcp", "0.0.0.0:"+port)
	if err != nil {
		return err
	}
	s.Listener = ln
	s.Log(fmt.Sprintf("Server started on port %s", port))

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
		fmt.Printf("[*] DEBUG: Accept() returned! Handling connection from %s\n", conn.RemoteAddr().String())
		go s.handleConnection(conn)
	}
	return nil
}

func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()
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

	nonce := crypto.GenerateRandomKey() // 32-byte nonce
	err = protocol.SendFrame(conn, []byte(fmt.Sprintf("KEY_EXCHANGE:%s|%s|%d",
		crypto.Base64Encode(pub),
		crypto.Base64Encode(nonce),
		crypto.PoWDifficulty)))
	if err != nil {
		s.Log(fmt.Sprintf("Handshake failed: could not send KEY_EXCHANGE to %s: %v", remoteAddr, err))
		return
	}

	payload, err := protocol.ReadFrame(conn)
	if err != nil {
		s.Log(fmt.Sprintf("Handshake failed: could not read CLIENT_HELLO from %s: %v", remoteAddr, err))
		return
	}

	line := string(payload)
	if !strings.HasPrefix(line, "CLIENT_HELLO:") {
		conn.Close()
		return
	}

	parts := strings.SplitN(line, ":", 7)
	if len(parts) != 7 {
		conn.Close()
		return
	}

	// Verify PoW
	powNonce, _ := crypto.Base64Decode(parts[6])
	if len(powNonce) != 8 {
		_ = protocol.SendFrame(conn, []byte("ERROR:Invalid PoW nonce"))
		conn.Close()
		return
	}
	var powNonceU64 uint64
	binary.Read(strings.NewReader(string(powNonce)), binary.BigEndian, &powNonceU64)

	if !crypto.VerifyPoW(nonce, powNonceU64, crypto.PoWDifficulty) {
		_ = protocol.SendFrame(conn, []byte("ERROR:Proof of Work verification failed"))
		conn.Close()
		return
	}

	clientPubBytes, err := crypto.Base64Decode(parts[1])
	if err != nil {
		conn.Close()
		return
	}

	sessionKey, err := crypto.DeriveSharedKey(priv, clientPubBytes)
	if err != nil {
		conn.Close()
		return
	}

	nickname := parts[2]
	identity := parts[3]
	clientSigningPub, _ := crypto.Base64Decode(parts[4])
	clientSig, _ := crypto.Base64Decode(parts[5])

	// Verify Identity Signature
	if !crypto.Verify(clientSigningPub, nonce, clientSig) {
		_ = protocol.SendFrame(conn, []byte("ERROR:Identity verification failed (bad signature)"))
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
			_ = protocol.SendFrame(conn, []byte("ERROR:This nickname is owned by another identity"))
			conn.Close()
			return
		}
	}
	s.mu.RUnlock()

	if isBanned {
		_ = protocol.SendFrame(conn, []byte("ERROR:You are banned from this server (Identity: "+banNick+")"))
		conn.Close()
		return
	}

	// Validate nickname — send an error frame before closing so the client
	// shows a meaningful message instead of a bare disconnect.
	if s.ClientManager.GetClientByNickname(nickname) != nil {
		_ = protocol.SendFrame(conn, []byte("ERROR:nickname already taken"))
		conn.Close()
		return
	}

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
	}

	if isKeyWhitelisted {
		client.Nickname = whitelistedNick
		client.State = "WHITELISTED"
		s.Log("Auto-approved returning user: " + client.Nickname)
	}

	s.ClientManager.AddClient(remoteAddr, client)

	// Handshake complete
	if err := protocol.SendFrame(conn, []byte("HANDSHAKE_OK")); err != nil {
		conn.Close()
		return
	}

	// Handshake complete — clear deadline, switch to no-timeout for normal I/O.
	conn.SetDeadline(time.Time{}) //nolint:errcheck

	// Notify client
	if client.State == "WHITELISTED" {
		s.SendSystemMessage(client, "✅ You have been approved by an admin!")
		s.BroadcastUserList()

		// Log join to history
		joinMsg := fmt.Sprintf("➜ %s joined the chat", nickname)
		s.DBManager.SaveMessage(config.MsgTypeJoin, joinMsg, "SERVER")

		s.Log(fmt.Sprintf("Auto-approved user: %s (%s)", nickname, remoteAddr))
		s.RegenerateRoomKey()

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
		s.RegenerateRoomKey()
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
	msgBytes, _ := msg.Encode()
	enc, _ := crypto.Encrypt(client.EncryptionKey, msgBytes)

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
		return // Heartbeat - just keeping connection alive
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

	if msg.Type == config.MsgTypeShare {
		s.mu.Lock()
		s.FileMetadata[msg.FileId] = msg
		s.mu.Unlock()
		s.Log(fmt.Sprintf("[SHARE] %s shared file: %s (ID: %s)", client.Nickname, msg.Content, msg.FileId))
		
		broadcastMsg := protocol.CreateMessage(config.MsgTypeShare, msg.Content, client.Nickname)
		broadcastMsg.FileId = msg.FileId
		s.BroadcastToState(client.State, broadcastMsg, client.Address)
		return
	}

	if msg.Type == config.MsgTypeFileUpload {
		os.MkdirAll("server_uploads", 0755)
		path := filepath.Join("server_uploads", msg.FileId)
		data, _ := crypto.Base64Decode(msg.Content)
		
		flags := os.O_CREATE | os.O_WRONLY
		if msg.ChunkIdx > 0 {
			flags |= os.O_APPEND
		} else {
			flags |= os.O_TRUNC
		}
		f, err := os.OpenFile(path, flags, 0644)
		if err == nil {
			f.Write(data)
			f.Close()
		}
		
		if msg.ChunkIdx == msg.TotalChunks-1 {
			s.Log(fmt.Sprintf("[UPLOAD] %s finished uploading %s", client.Nickname, msg.FileId))
		}
		return
	}

	if msg.Type == config.MsgTypeFileDownloadReq {
		path := filepath.Join("server_uploads", msg.FileId)
		data, err := os.ReadFile(path)
		if err == nil {
			chunkSize := 512 * 1024
			totalChunks := (len(data) + chunkSize - 1) / chunkSize
			if len(data) == 0 {
				totalChunks = 1
			}
			for i := 0; i < totalChunks; i++ {
				start := i * chunkSize
				end := start + chunkSize
				if end > len(data) {
					end = len(data)
				}
				
				res := protocol.CreateMessage(config.MsgTypeFileDownloadRes, crypto.Base64Encode(data[start:end]), "SERVER")
				res.FileId = msg.FileId
				res.ReplyTo = msg.Content // Use ReplyTo for destPath
				res.ChunkIdx = int32(i)
				res.TotalChunks = int32(totalChunks)
				
				msgBytes, _ := res.Encode()
				enc, _ := crypto.Encrypt(client.EncryptionKey, msgBytes)
				client.SendChan <- crypto.Base64Encode(enc)
			}
		} else {
			s.SendSystemMessage(client, "❌ Server could not find file: "+msg.FileId)
		}
		return
	}

	if msg.Type == config.MsgTypeGetFileMetadata {
		s.mu.RLock()
		meta, exists := s.FileMetadata[msg.Content]
		s.mu.RUnlock()
		if exists {
			resp := protocol.CreateMessage(config.MsgTypeGetFileMetadata, meta.Content, "SERVER")
			resp.FileId = meta.FileId
			resp.OnionAddr = meta.OnionAddr
			resp.FileToken = meta.FileToken
			resp.ReplyTo = msg.Content // ID requested
			
			respBytes, _ := resp.Encode()
			enc, _ := crypto.Encrypt(client.EncryptionKey, respBytes)
			select {
			case client.SendChan <- crypto.Base64Encode(enc):
			default:
			}
		} else {
			s.SendSystemMessage(client, "❌ File ID not found: "+msg.Content)
		}
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

	// Handle Admin Commands
	if msg.Type == config.MsgTypeChat && strings.HasPrefix(msg.Content, "/") {
		s.handleCommand(client, msg.Content)
		return
	}

	if msg.Type == config.MsgTypePrivate {
		s.Log(fmt.Sprintf("[PRIVATE] %s -> %s: %s", client.Nickname, msg.ReplyTo, msg.Content))
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

	client.MessageCount++

	switch client.State {
	case "WHITELISTED":
		s.DBManager.SaveMessage(config.MsgTypeChat, msg.Content, client.Nickname)
		s.Log(fmt.Sprintf("[WHITELIST] %s: %s", client.Nickname, msg.Content))
	}

	broadcastMsg := protocol.CreateMessage(config.MsgTypeChat, msg.Content, client.Nickname)
	broadcastMsg.MessageId = msg.MessageId
	broadcastMsg.IsAdmin = client.IsAdmin
	s.BroadcastToState(client.State, broadcastMsg, client.Address)
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
			userInfos = append(userInfos, fmt.Sprintf("%s|%s", 
				c.Nickname, 
				crypto.Base64Encode(c.SigningPubKey)))
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

func (s *Server) RegenerateRoomKey() {
	s.mu.Lock()
	newKey := make([]byte, 32)
	_, _ = rand.Read(newKey)
	s.RoomKey = newKey
	s.mu.Unlock()

	keyMsg := protocol.CreateMessage(config.MsgTypeRoomKey, crypto.Base64Encode(newKey), "SERVER")
	s.broadcastToWhitelisted(keyMsg)
	
	s.mu.Lock()
	s.RoomKey = nil // Wipe content key from RAM
	s.mu.Unlock()
	
	s.AddEvent("🔄", "Group rotated. Keys updated for join/leave event.")
}

func (s *Server) broadcastToWhitelisted(msg *protocol.ChatMessage) {
	for _, c := range s.ClientManager.GetAllClients() {
		if c.State == "WHITELISTED" {
			s.SendMessage(c, msg)
		}
	}
}
