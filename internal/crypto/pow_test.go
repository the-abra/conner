package crypto

import (
	"testing"
)

func TestPoW(t *testing.T) {
	data := []byte("test-challenge-nonce")
	difficulty := 10 // Low difficulty for fast test

	nonce := ComputePoW(data, difficulty)
	if !VerifyPoW(data, nonce, difficulty) {
		t.Errorf("PoW verification failed for nonce %d", nonce)
	}

	if VerifyPoW(data, nonce+1, difficulty) {
		t.Errorf("PoW verification succeeded for incorrect nonce")
	}
}
