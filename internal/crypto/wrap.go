package crypto

import (
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
	"os"

	"golang.org/x/crypto/pbkdf2"
)

const wrapIters = 200_000

// WrapKey encrypts identity material with a passphrase (PBKDF2-SHA256 + AES-GCM).
func WrapKey(passphrase string, plaintext []byte) ([]byte, error) {
	salt := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, err
	}
	key := pbkdf2.Key([]byte(passphrase), salt, wrapIters, 32, sha256.New)
	ct, err := Encrypt(key, plaintext)
	if err != nil {
		Wipe(key)
		return nil, err
	}
	Wipe(key)
	out := make([]byte, 16+len(ct))
	copy(out, salt)
	copy(out[16:], ct)
	return out, nil
}

func UnwrapKey(passphrase string, blob []byte) ([]byte, error) {
	if len(blob) < 16+12+16 {
		return nil, errors.New("wrapped key too short")
	}
	salt, ct := blob[:16], blob[16:]
	key := pbkdf2.Key([]byte(passphrase), salt, wrapIters, 32, sha256.New)
	pt, err := Decrypt(key, ct)
	Wipe(key)
	return pt, err
}

func WriteWrapped(path, passphrase string, plaintext []byte) error {
	blob, err := WrapKey(passphrase, plaintext)
	if err != nil {
		return err
	}
	return os.WriteFile(path, blob, 0o600)
}

func ReadWrapped(path, passphrase string) ([]byte, error) {
	blob, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return UnwrapKey(passphrase, blob)
}
