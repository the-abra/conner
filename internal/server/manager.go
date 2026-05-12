package server

import (
	"net"
	"sync"
	"time"
)

type Client struct {
	Conn          net.Conn
	Nickname      string
	Address       string
	Identity      string // Self-reported Onion or IP address
	JoinTime      time.Time
	EncryptionKey []byte
	State         string // PENDING, WHITELISTED, BLACKLISTED
	MessageCount  int
	SendChan      chan string
	IsAdmin       bool
	LastSeen      time.Time
	SigningPubKey []byte
	E2EPubKey     []byte
}

type ClientManager struct {
	clients map[string]*Client // key is Address or UUID
	mu      sync.RWMutex
}

func NewClientManager() *ClientManager {
	return &ClientManager{
		clients: make(map[string]*Client),
	}
}

func (cm *ClientManager) AddClient(id string, client *Client) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.clients[id] = client
}

func (cm *ClientManager) RemoveClient(id string) *Client {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	client, exists := cm.clients[id]
	if exists {
		delete(cm.clients, id)
	}
	return client
}

func (cm *ClientManager) GetClient(id string) *Client {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.clients[id]
}

func (cm *ClientManager) GetClientByNickname(nickname string) *Client {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	for _, c := range cm.clients {
		if c.Nickname == nickname {
			return c
		}
	}
	return nil
}

func (cm *ClientManager) GetAllClients() []*Client {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	list := make([]*Client, 0, len(cm.clients))
	for _, c := range cm.clients {
		list = append(list, c)
	}
	return list
}

func (cm *ClientManager) Count() int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return len(cm.clients)
}
