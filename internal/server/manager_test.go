package server

import (
	"testing"
	"time"

	"conner/internal/config"
)

func TestRateLimit(t *testing.T) {
	c := &Client{}
	ok := 0
	for i := 0; i < config.RateLimitMessages+5; i++ {
		if c.allowMessage() {
			ok++
		}
	}
	if ok != config.RateLimitMessages {
		t.Fatalf("allowed %d want %d", ok, config.RateLimitMessages)
	}
	c.winStart = time.Now().Add(-2 * config.RateLimitWindow)
	if !c.allowMessage() {
		t.Fatal("window should reset")
	}
}

func TestWhitelistedCount(t *testing.T) {
	cm := NewClientManager()
	cm.AddClient("a", &Client{State: "PENDING"})
	cm.AddClient("b", &Client{State: "WHITELISTED"})
	if cm.WhitelistedCount() != 1 {
		t.Fatal(cm.WhitelistedCount())
	}
}
