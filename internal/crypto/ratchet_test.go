package crypto

import (
	"bytes"
	"testing"
)

func TestRatchet(t *testing.T) {
	initialKey := []byte("01234567890123456789012345678901")
	sender := NewRatchet(initialKey)
	receiver := NewRatchet(initialKey)

	// Sequential
	mk1, step1 := sender.Next()
	mk1_recv, err := receiver.GetMessageKey(step1)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(mk1, mk1_recv) {
		t.Fatal("Keys do not match")
	}

	// Out of order
	mk2, step2 := sender.Next()
	mk3, step3 := sender.Next()

	// Receive step3 first
	mk3_recv, err := receiver.GetMessageKey(step3)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(mk3, mk3_recv) {
		t.Fatal("Step 3 keys do not match")
	}

	// Now receive step2
	mk2_recv, err := receiver.GetMessageKey(step2)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(mk2, mk2_recv) {
		t.Fatal("Step 2 keys do not match")
	}

	// Try past unskipped key
	_, err = receiver.GetMessageKey(step1)
	if err == nil {
		t.Fatal("Should fail on past key")
	}
}
