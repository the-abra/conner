package crypto

import (
	"bytes"
	"testing"
)

func TestSenderKeysIndependent(t *testing.T) {
	seedA := GenerateRandomKey()
	a := NewSenderState("alice", 1, seedA)
	b := NewSenderState("bob", 1, GenerateRandomKey())
	b.AdoptPeer("alice", a.ExportMySeed())

	k1, s1, ok := a.NextSend()
	if !ok {
		t.Fatal("send")
	}
	got, err := b.Recv("alice", s1)
	if err != nil || !bytes.Equal(got, k1) {
		t.Fatalf("recv %v", err)
	}
	if _, err := b.Recv("carol", 0); err == nil {
		t.Fatal("missing chain")
	}
	if !b.HasPeer("alice") || b.HasPeer("carol") {
		t.Fatal("HasPeer")
	}
}

func TestSenderEpochResetDropsOldChain(t *testing.T) {
	a := NewSenderState("alice", 1, GenerateRandomKey())
	b := NewSenderState("bob", 1, GenerateRandomKey())
	b.AdoptPeer("alice", a.ExportMySeed())
	a.ResetEpoch(2, GenerateRandomKey())
	if a.Epoch() != 2 {
		t.Fatal(a.Epoch())
	}
	_, step, _ := a.NextSend()
	if _, err := b.Recv("alice", step); err == nil {
		// bob still has epoch-1 chain; keys must not match new sends
		// Recv may succeed on the *old* ratchet at a colliding step — compare bytes.
	}
	kNew, st, _ := a.NextSend()
	old, err := b.Recv("alice", st)
	if err == nil && bytes.Equal(old, kNew) {
		t.Fatal("old peer chain must not match post-epoch send")
	}
}

func TestExportSeedCopy(t *testing.T) {
	a := NewSenderState("alice", 1, GenerateRandomKey())
	s1 := a.ExportMySeed()
	s1[0] ^= 0xff
	s2 := a.ExportMySeed()
	if s2[0] == s1[0] && bytes.Equal(s1, s2) {
		t.Fatal("export should copy")
	}
}
