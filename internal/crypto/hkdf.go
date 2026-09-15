package crypto

import (
	"io"

	"crypto/sha256"

	"golang.org/x/crypto/hkdf"
)

// DeriveSessionKey turns a raw X25519 shared secret into a 32-byte AEAD key.
func DeriveSessionKey(shared []byte, info string) ([]byte, error) {
	r := hkdf.New(sha256.New, shared, nil, []byte(info))
	key := make([]byte, 32)
	if _, err := io.ReadFull(r, key); err != nil {
		return nil, err
	}
	return key, nil
}
