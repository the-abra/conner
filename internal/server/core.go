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
	Time  time.Time
	Icon  string
	Text  string
}

type Server struct {
	ClientManager     *ClientManager
	DBManager         *MemoryManager
	BlacklistDB       *MemoryManager
	MediaDB           sync.Map // map[string]*MediaEntry
	BroadcastKey      []byte
	Running           bool
	mu                sync.RWMutex
	Listener          net.Listener
	ConsoleHistory    []string
	Stats             ServerStats
	EventLog          []EventEntry // ring buffer of real-time notifications
	BlacklistMap      map[string]bool
}

type ServerStats struct {
	TotalConnections int
	MessagesSent     int
	CommandsExecuted int
	KicksIssued      int
	StartTime        time.Time
	TorAddress       string
}

func NewServer() *Server {
	torAddr := "NOT GENERATED / HIDDEN SERVICE DISABLED"

	// Try all known hostname file locations in order of priority.
	// CONNER_WORKDIR is set by entrypoint.sh via /etc/profile.d/conner.sh
	candidates := []string{
		"hostname",                                    // relative to CWD (dev)
		"/var/lib/tor/conner_chat/hostname",           // Alpine install (entrypoint.sh)
		"/var/lib/tor/hidden_service/hostname",        // generic Tor install
		"/var/lib/tor/onion/hostname",                 // alternate name
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
		BlacklistDB:    NewMemoryManager(config.MessageHistoryLimit, config.MessageTTL),
		BroadcastKey:   crypto.GenerateRandomKey(),
		Running:        true,
		ConsoleHistory: make([]string, 0),
		EventLog:       make([]EventEntry, 0, 200),
		Stats:          ServerStats{StartTime: time.Now(), TorAddress: torAddr},
		BlacklistMap:   make(map[string]bool),
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

	// Background TTL loop
	go func() {
		for s.Running {
			time.Sleep(1 * time.Minute)
			p1 := s.DBManager.CleanupOldMessages()
			p2 := s.BlacklistDB.CleanupOldMessages()
			if p1+p2 > 0 {
				s.Log(fmt.Sprintf("TTL: Purged %d expired messages", p1+p2))
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

	// Key Exchange
	priv, pub, err := crypto.GenerateKeyPair()
	if err != nil {
		conn.Close()
		return
	}
	
	protocol.SendFrame(conn, []byte(fmt.Sprintf("KEY_EXCHANGE:%s", crypto.Base64Encode(pub))))

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
	
	parts := strings.SplitN(line, ":", 3)
	if len(parts) != 3 {
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
		JoinTime:      time.Now(),
		EncryptionKey: sessionKey,
		State:         "PENDING",
		SendChan:      make(chan string, 100),
	}

	// Check shadow blacklist
	s.mu.RLock()
	isBlacklisted := s.BlacklistMap[nickname] || s.BlacklistMap[remoteAddr]
	s.mu.RUnlock()

	if isBlacklisted {
		client.State = "BLACKLISTED"
		s.Log(fmt.Sprintf("Shadow-blocked user joined: %s (%s)", nickname, remoteAddr))
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
	if client.State == "BLACKLISTED" {
		s.SendSystemMessage(client, "✅ Approved by bot. Welcome!")
	} else {
		s.SendSystemMessage(client, "Connecting... Waiting for admin approval.")
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
	close(client.SendChan)
	
	if client.State == "WHITELISTED" || client.State == "BLACKLISTED" {
		leaveMsg := protocol.CreateMessage(config.MsgTypeJoin, fmt.Sprintf("%s left the chat", client.Nickname), "SERVER")
		s.BroadcastToState(client.State, leaveMsg, client.Address)
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
	case "BLACKLISTED":
		s.BlacklistDB.SaveMessage(config.MsgTypeChat, msg.Content, client.Nickname)
		s.Log(fmt.Sprintf("[BLACKLIST] %s: %s", client.Nickname, msg.Content))
	}

	broadcastMsg := protocol.CreateMessage(config.MsgTypeChat, msg.Content, client.Nickname)
	broadcastMsg.IsAdmin = client.IsAdmin
	s.BroadcastToState(client.State, broadcastMsg, client.Address)
}

func (s *Server) handleCommand(client *Client, content string) {
	s.mu.Lock()
	s.Stats.CommandsExecuted++
	s.mu.Unlock()

	parts := strings.SplitN(content, " ", 3)
	if len(parts) == 0 {
		return
	}
	cmd := parts[0]

	switch cmd {
	case "/list":
		users := []string{}
		for _, c := range s.ClientManager.GetAllClients() {
			if c.State == client.State {
				users = append(users, c.Nickname)
			}
		}
		s.SendSystemMessage(client, "Online: "+strings.Join(users, ", "))
		// Also send structured user list for sidebar
		listMsg := protocol.CreateMessage(config.MsgTypeUserList, strings.Join(users, ","), "SERVER")
		s.SendMessage(client, listMsg)

	case "/private":
		if len(parts) >= 3 {
			target := parts[1]
			privMsg := parts[2]
			targetClient := s.ClientManager.GetClientByNickname(target)
			if targetClient != nil && targetClient.State == client.State {
				pm := protocol.CreateMessage(config.MsgTypePrivate, "[PM from "+client.Nickname+"]: "+privMsg, client.Nickname)
				pmJSON, _ := pm.ToJSON()
				enc, _ := crypto.Encrypt(s.BroadcastKey, []byte(pmJSON))
				select {
				case targetClient.SendChan <- crypto.Base64Encode(enc):
				default:
				}
				s.SendSystemMessage(client, "PM sent to "+target+".")
			} else {
				s.SendSystemMessage(client, "User not found or not in your room.")
			}
		}

	case "/ann":
		if client.IsAdmin && len(parts) >= 2 {
			annText := strings.Join(parts[1:], " ")
			annMsg := protocol.CreateMessage(config.MsgTypeSystem,
				fmt.Sprintf("📢 ANNOUNCEMENT: %s", annText), "ADMIN")
			s.BroadcastToState(client.State, annMsg, "")
			s.Log(fmt.Sprintf("[ANN] %s: %s", client.Nickname, annText))
		} else {
			s.SendSystemMessage(client, "Unauthorized. Admin only.")
		}

	case "/op":
		if client.IsAdmin && len(parts) >= 2 {
			target := s.ClientManager.GetClientByNickname(parts[1])
			if target != nil {
				target.IsAdmin = true
				s.SendSystemMessage(client, parts[1]+" is now an admin.")
				s.SendSystemMessage(target, "You have been granted admin privileges.")
			}
		} else {
			s.SendSystemMessage(client, "Unauthorized or missing argument.")
		}

	case "/connect":
		if client.IsAdmin && len(parts) >= 2 {
			target := s.ClientManager.GetClientByNickname(parts[1])
			if target != nil {
				target.State = "WHITELISTED"
				s.SendSystemMessage(client, parts[1]+" moved to whitelist.")
				s.SendSystemMessage(target, "You have been approved by an admin.")
				s.Log(fmt.Sprintf("Admin %s approved %s", client.Nickname, parts[1]))
				s.AddEvent("✅", fmt.Sprintf("%s approved %s → WHITELIST", client.Nickname, parts[1]))
				s.BroadcastUserList()
			}
		}

	case "/block", "/blacklist":
		if client.IsAdmin && len(parts) >= 2 {
			target := s.ClientManager.GetClientByNickname(parts[1])
			s.mu.Lock()
			s.BlacklistMap[parts[1]] = true
			if target != nil {
				s.BlacklistMap[target.Address] = true
				target.State = "BLACKLISTED"
				s.SendSystemMessage(target, "✅ Approved by bot. Welcome!")
			}
			s.mu.Unlock()
			s.SendSystemMessage(client, parts[1]+" moved to shadow room and blacklisted.")
			s.Log(fmt.Sprintf("Admin %s blacklisted %s (shadow)", client.Nickname, parts[1]))
			s.AddEvent("⛔", fmt.Sprintf("%s blacklisted %s → SHADOW ROOM", client.Nickname, parts[1]))
			s.BroadcastUserList()
		}

	case "/kick":
		if client.IsAdmin && len(parts) >= 2 {
			target := s.ClientManager.GetClientByNickname(parts[1])
			if target != nil {
				s.AddEvent("⚡", fmt.Sprintf("%s kicked %s", client.Nickname, parts[1]))
				s.SendSystemMessage(target, "⚡ You have been kicked from the server.")
				s.removeClient(target)
				s.SendSystemMessage(client, parts[1]+" has been kicked.")
				s.Log(fmt.Sprintf("Admin %s kicked %s", client.Nickname, parts[1]))
				s.BroadcastUserList()
			}
		}
	}
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
	s.BroadcastToState("WHITELISTED", msg, "")
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

