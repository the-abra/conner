package server

import (
	"testing"
	"time"
)

func TestMemoryCapAndTTL(t *testing.T) {
	m := NewMemoryManager(3, time.Hour)
	m.SaveMessage("CHAT", "a", "u")
	m.SaveMessage("CHAT", "b", "u")
	m.SaveMessage("CHAT", "c", "u")
	m.SaveMessage("CHAT", "d", "u")
	h := m.GetHistory()
	if len(h) != 3 || h[0].Content != "b" {
		t.Fatalf("%+v", h)
	}
	short := NewMemoryManager(10, time.Nanosecond)
	short.SaveMessage("CHAT", "x", "u")
	time.Sleep(2 * time.Millisecond)
	if n := short.CleanupOldMessages(); n != 1 {
		t.Fatalf("purged %d", n)
	}
}
