package crypto

import (
	"bytes"
	"testing"
)

func TestE2EE(t *testing.T) {
	// Alice's keys
	aPriv, aPub, _ := GenerateKeyPair()
	// Bob's keys
	bPriv, bPub, _ := GenerateKeyPair()

	// Alice derives secret
	aSecret, _ := DeriveSharedKey(aPriv, bPub)
	// Bob derives secret
	bSecret, _ := DeriveSharedKey(bPriv, aPub)

	if !bytes.Equal(aSecret, bSecret) {
		t.Error("Shared secrets do not match")
	}

	plaintext := []byte("Hello Bob, this is Alice")
	ciphertext, _ := Encrypt(aSecret, plaintext)
	decrypted, _ := Decrypt(bSecret, ciphertext)

	if !bytes.Equal(plaintext, decrypted) {
		t.Error("Decrypted text does not match plaintext")
	}
}
