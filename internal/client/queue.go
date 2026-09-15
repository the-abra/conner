package client

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"conner/internal/appdir"
	"conner/internal/protocol"

	"google.golang.org/protobuf/encoding/protojson"
)

// DiskQueue persists outbound ChatMessages as protojson lines (best-effort).
type DiskQueue struct {
	mu   sync.Mutex
	path string
}

func NewDiskQueue(nick string) *DiskQueue {
	return &DiskQueue{path: appdir.QueueFile(nick)}
}

func (q *DiskQueue) Enqueue(msg *protocol.ChatMessage) error {
	if q == nil || msg == nil {
		return nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	_ = os.MkdirAll(filepath.Dir(q.path), 0o700)
	b, err := protojson.Marshal(msg)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(q.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(b, '\n'))
	return err
}

func (q *DiskQueue) Drain() ([]*protocol.ChatMessage, error) {
	if q == nil {
		return nil, nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	raw, err := os.ReadFile(q.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	_ = os.Remove(q.path)
	var out []*protocol.ChatMessage
	for _, line := range splitLines(raw) {
		if len(line) == 0 {
			continue
		}
		m := &protocol.ChatMessage{}
		if err := protojson.Unmarshal(line, m); err != nil {
			// skip corrupt
			_ = json.Unmarshal(line, m)
			continue
		}
		out = append(out, m)
	}
	return out, nil
}

func splitLines(b []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, c := range b {
		if c == '\n' {
			lines = append(lines, b[start:i])
			start = i + 1
		}
	}
	if start < len(b) {
		lines = append(lines, b[start:])
	}
	return lines
}
