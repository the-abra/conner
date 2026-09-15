package client

import (
	"testing"

	"conner/internal/config"
	"conner/internal/protocol"
)

func TestDiskQueueRoundTrip(t *testing.T) {
	t.Setenv("CONNER_HOME", t.TempDir())
	q := NewDiskQueue("alice")
	m := protocol.CreateMessage(config.MsgTypeChat, "hello", "alice")
	if err := q.Enqueue(m); err != nil {
		t.Fatal(err)
	}
	got, err := q.Drain()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Content != "hello" {
		t.Fatalf("%+v", got)
	}
	got2, err := q.Drain()
	if err != nil || len(got2) != 0 {
		t.Fatalf("second drain %v %v", got2, err)
	}
}
