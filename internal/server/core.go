package server

import (
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

// MediaEntry stores metadata about uploaded files
type MediaEntry struct {
	ID         string
	Filename   string
	Uploader   string
	UploadedAt time.Time
	Metadata   string // client address or other info
}

// EventEntry is a single real-time notification in the dashboard feed.
type EventEntry struct {
	Time time.Time
	Icon string
	Text string
}

type Server struct {
	ClientManager  *ClientManager
	DBManager      *MemoryManager
	MediaDB        sync.Map // map[string]*MediaEntry
	BroadcastKey   []byte
	Running        bool
	mu             sync.RWMutex
	Listener       net.Listener
	ConsoleHistory []string
	Stats          ServerStats
	EventLog       []EventEntry      // ring buffer of real-time notifications
	BlacklistMap   map[string]string // Identity -> Nickname (Metadata)
	WhitelistMap   map[string]bool   // Identity -> bool
	CmdRegistry    *CommandRegistry
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
	s.mu.Lock()
	s.WhitelistMap[target.Identity] = true
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
		BroadcastKey:   crypto.GenerateRandomKey(),
		Running:        true,
		ConsoleHistory: make([]string, 0),
		EventLog:       make([]EventEntry, 0, 200),
		Stats:          ServerStats{StartTime: time.Now(), TorAddress: torAddr},
		CmdRegistry:    NewCommandRegistry(),
		BlacklistMap:   make(map[string]string),
		WhitelistMap:   make(map[string]bool),
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

// GetMediaList returns all media entries as a slice
func (s *Server) GetMediaList() []*MediaEntry {
	var entries []*MediaEntry
	s.MediaDB.Range(func(key, value any) bool {
		entries = append(entries, value.(*MediaEntry))
		return true
	})
	return entries
}

// DeleteMedia removes a media entry from RAM
func (s *Server) DeleteMedia(id string) bool {
	_, ok := s.MediaDB.Load(id)
	if ok {
		s.MediaDB.Delete(id)
	}
	return ok
}

func (s *Server) Start(port string) error {
	ln, err := net.Listen("tcp", ":"+port)
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
			encoded, _ := pingMsg.ToJSON()

			for _, c := range s.ClientManager.GetAllClients() {
				if time.Since(c.LastSeen) > 90*time.Second {
					s.Log(fmt.Sprintf("Heartbeat timeout: %s (%s)", c.Nickname, c.Address))
					c.Conn.Close()
					continue
				}
				// Encrypt ping with broadcast key (all server broadcasts use this)
				enc, _ := crypto.Encrypt(s.BroadcastKey, []byte(encoded))
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
		go s.handleConnection(conn)
	}
	return nil
}

func (s *Server) handleConnection(conn net.Conn) {
	remoteAddr := conn.RemoteAddr().String()
	s.Log(fmt.Sprintf("New connection from %s", remoteAddr))

	// Set handshake deadline — prevents a connecting client from holding
	// a goroutine forever by never completing the exchange.
	conn.SetDeadline(time.Now().Add(30 * time.Second)) //nolint:errcheck

	// Key Exchange + Challenge
	priv, pub, err := crypto.GenerateKeyPair()
	if err != nil {
		conn.Close()
		return
	}

	nonce := crypto.GenerateRandomKey() // 32-byte nonce
	protocol.SendFrame(conn, []byte(fmt.Sprintf("KEY_EXCHANGE:%s|%s",
		crypto.Base64Encode(pub),
		crypto.Base64Encode(nonce))))

	payload, err := protocol.ReadFrame(conn)
	if err != nil {
		conn.Close()
		return
	}

	line := string(payload)
	if !strings.HasPrefix(line, "CLIENT_HELLO:") {
		conn.Close()
		return
	}

	parts := strings.SplitN(line, ":", 6)
	if len(parts) != 6 {
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

	// Check persistent maps BEFORE creating client
	s.mu.RLock()
	banNick, isBanned := s.BlacklistMap[identity]
	isAutoApproved := s.WhitelistMap[identity]
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

	if isAutoApproved {
		client.State = "WHITELISTED"
		s.Log("Auto-approved returning user: " + nickname)
	}

	s.ClientManager.AddClient(remoteAddr, client)

	// Send the shared BroadcastKey to the client, encrypted with their
	// unique session key. All group messages are encrypted with BroadcastKey;
	// the client must have it to decrypt any broadcast or announcement.
	encBK, err := crypto.Encrypt(sessionKey, s.BroadcastKey)
	if err != nil {
		conn.Close()
		return
	}
	if err := protocol.SendFrame(conn, []byte("BROADCAST_KEY:"+crypto.Base64Encode(encBK))); err != nil {
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
		s.AddEvent("✅", fmt.Sprintf("Auto-approved: %s (%s)", nickname, remoteAddr))

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
	} else {
		s.Log(fmt.Sprintf("Pending client disconnected: %s", client.Address))
		s.AddEvent("🔌", fmt.Sprintf("Pending client dropped: %s", client.Address))
	}
}

func (s *Server) SendSystemMessage(client *Client, content string) {
	msg := protocol.CreateMessage(config.MsgTypeSystem, content, "SERVER")
	msgJSON, _ := msg.ToJSON()
	enc, _ := crypto.Encrypt(s.BroadcastKey, []byte(msgJSON))

	select {
	case client.SendChan <- crypto.Base64Encode(enc):
	default:
		s.Log(fmt.Sprintf("Dropped system message for %s (buffer full)", client.Nickname))
	}
}

func (s *Server) SendMessage(client *Client, msg protocol.ChatMessage) {
	msgJSON, _ := msg.ToJSON()
	enc, _ := crypto.Encrypt(s.BroadcastKey, []byte(msgJSON))

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

	msg, err := protocol.FromJSON(string(decryptedBytes))
	if err != nil {
		return
	}

	// PENDING users are blocked
	if client.State == "PENDING" {
		s.SendSystemMessage(client, "You are waiting for admin approval.")
		return
	}

	if msg.Type == config.MsgTypePong {
		return // LastSeen was already updated in the read pump
	}

	if msg.Type == config.MsgTypeMediaRegister {
		parts := strings.SplitN(msg.Content, "|", 3)
		if len(parts) == 3 {
			id := parts[1]
			filename := parts[0]
			metadata := parts[2]

			entry := &MediaEntry{
				ID:         id,
				Filename:   filename,
				Uploader:   client.Nickname,
				UploadedAt: time.Now(),
				Metadata:   metadata,
			}
			s.MediaDB.Store(id, entry)

			info := fmt.Sprintf("[FILE] %s shared: %s (ID: %s) — /download %s", client.Nickname, filename, id, id)
			bmsg := protocol.CreateMessage(config.MsgTypeMediaInfo, info, "SERVER")
			s.BroadcastToState(client.State, bmsg, "")
			s.Log(fmt.Sprintf("[MEDIA] %s registered %s -> %s", client.Nickname, filename, id))
			s.AddEvent("📎", fmt.Sprintf("%s shared: %s (ID: %s)", client.Nickname, filename, id))
		}
		return
	}

	if msg.Type == config.MsgTypeMediaData {
		parts := strings.SplitN(msg.Content, "|", 3)
		if len(parts) == 3 {
			filename := parts[0]
			data := parts[1]
			targetNick := parts[2]

			target := s.ClientManager.GetClientByNickname(targetNick)
			if target != nil {
				resp := protocol.CreateMessage(config.MsgTypeMediaData, filename+"|"+data, "SERVER")
				msgJSON, _ := resp.ToJSON()
				enc, _ := crypto.Encrypt(s.BroadcastKey, []byte(msgJSON))
				select {
				case target.SendChan <- crypto.Base64Encode(enc):
					s.Log(fmt.Sprintf("[PUSH] Forwarded %s to %s", filename, targetNick))
					s.AddEvent("⬇️", fmt.Sprintf("%s downloaded: %s", targetNick, filename))
				default:
				}
			}
		}
		return
	}

	if msg.Type == config.MsgTypeDownloadReq {
		val, ok := s.MediaDB.Load(msg.Content)
		if ok {
			entry := val.(*MediaEntry)
			// Find the owner
			owner := s.ClientManager.GetClientByNickname(entry.Uploader)
			if owner != nil {
				// Request the file from the owner
				// We send a system message or a special type to the owner
				// For simplicity, let's reuse MsgTypeDownloadReq but with the file ID
				req := protocol.CreateMessage(config.MsgTypeDownloadReq, entry.Filename+"|"+msg.Sender, "SERVER")
				reqJSON, _ := req.ToJSON()
				enc, _ := crypto.Encrypt(s.BroadcastKey, []byte(reqJSON))
				select {
				case owner.SendChan <- crypto.Base64Encode(enc):
					s.Log(fmt.Sprintf("[PULL] Requested %s from %s for %s", entry.Filename, owner.Nickname, client.Nickname))
				default:
					s.SendSystemMessage(client, "Owner is busy, try again later.")
				}
			} else {
				s.SendSystemMessage(client, "Owner is offline.")
			}
		} else {
			s.SendSystemMessage(client, "File not found or expired.")
		}
		return
	}

	if strings.HasPrefix(msg.Content, "/") {
		s.handleCommand(client, msg.Content)
		return
	}

	client.MessageCount++

	switch client.State {
	case "WHITELISTED":
		s.DBManager.SaveMessage(config.MsgTypeChat, msg.Content, client.Nickname)
		s.Log(fmt.Sprintf("[WHITELIST] %s: %s", client.Nickname, msg.Content))
	}

	broadcastMsg := protocol.CreateMessage(config.MsgTypeChat, msg.Content, client.Nickname)
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
	var users []string
	for _, c := range s.ClientManager.GetAllClients() {
		if c.State == "WHITELISTED" {
			users = append(users, c.Nickname)
		}
	}
	userList := strings.Join(users, ",")
	msg := protocol.CreateMessage(config.MsgTypeUserList, userList, "SERVER")

	// Broadcast to everyone so their sidebars update
	s.mu.RLock()
	clients := s.ClientManager.GetAllClients()
	s.mu.RUnlock()

	msgJSON, _ := msg.ToJSON()
	enc, _ := crypto.Encrypt(s.BroadcastKey, []byte(msgJSON))
	payload := crypto.Base64Encode(enc)

	for _, c := range clients {
		select {
		case c.SendChan <- payload:
		default:
		}
	}
}

func (s *Server) BroadcastToState(state string, msg protocol.ChatMessage, excludeID string) {
	msgJSON, _ := msg.ToJSON()
	enc, _ := crypto.Encrypt(s.BroadcastKey, []byte(msgJSON))
	b64 := crypto.Base64Encode(enc)

	sentCount := 0
	for _, c := range s.ClientManager.GetAllClients() {
		if c.State == state && c.Address != excludeID {
			select {
			case c.SendChan <- b64:
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
