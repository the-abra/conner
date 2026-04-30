package server

import (
	"sync"
	"time"
)

type HistoryMessage struct {
	Type      string `json:"type"`
	Content   string `json:"content"`
	Sender    string `json:"sender"`
	Timestamp string `json:"timestamp"`
	ParsedTime time.Time `json:"-"`
}

type MemoryManager struct {
	messages []HistoryMessage
	mu       sync.RWMutex
	limit    int
	ttl      time.Duration
}

func NewMemoryManager(limit int, ttl time.Duration) *MemoryManager {
	return &MemoryManager{
		messages: make([]HistoryMessage, 0, limit),
		limit:    limit,
		ttl:      ttl,
	}
}

func (m *MemoryManager) SaveMessage(msgType, content, sender string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	msg := HistoryMessage{
		Type:       msgType,
		Content:    content,
		Sender:     sender,
		Timestamp:  time.Now().Format("2006-01-02 15:04:05"),
		ParsedTime: time.Now(),
	}

	m.messages = append(m.messages, msg)
	if len(m.messages) > m.limit {
		m.messages = m.messages[len(m.messages)-m.limit:]
	}
}

func (m *MemoryManager) GetHistory() []HistoryMessage {
	m.mu.RLock()
	defer m.mu.RUnlock()
	res := make([]HistoryMessage, len(m.messages))
	copy(res, m.messages)
	return res
}

func (m *MemoryManager) CleanupOldMessages() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	purged := 0
	var newMessages []HistoryMessage

	for _, msg := range m.messages {
		if now.Sub(msg.ParsedTime) <= m.ttl {
			newMessages = append(newMessages, msg)
		} else {
			purged++
		}
	}

	m.messages = newMessages
	return purged
}

func (m *MemoryManager) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = make([]HistoryMessage, 0, m.limit)
}
