package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"io"
	"os"
	"sync"

	"conner/internal/appdir"
)

const maxHistory = 2000

// EncryptedStore appends length-prefixed AEAD blobs to disk (history cap).
type EncryptedStore struct {
	mu   sync.Mutex
	path string
	key  []byte
	n    int
}

func Open(roomID string, key []byte) (*EncryptedStore, error) {
	if err := os.MkdirAll(appdir.Path("history"), 0o700); err != nil {
		return nil, err
	}
	return &EncryptedStore{path: appdir.HistoryFile(roomID), key: key}, nil
}

func (s *EncryptedStore) Append(plaintext []byte) error {
	if s == nil || len(s.key) != 32 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return err
	}
	ct := gcm.Seal(nonce, nonce, plaintext, nil)
	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(ct)))
	if _, err := f.Write(hdr[:]); err != nil {
		return err
	}
	if _, err := f.Write(ct); err != nil {
		return err
	}
	s.n++
	if s.n > maxHistory {
		_ = s.truncate()
	}
	return nil
}

func (s *EncryptedStore) truncate() error {
	// Best-effort: rewrite last maxHistory/2 records is expensive; just rotate file.
	bak := s.path + ".old"
	_ = os.Rename(s.path, bak)
	s.n = 0
	return os.Remove(bak)
}

func (s *EncryptedStore) Wipe() {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = os.Remove(s.path)
	s.n = 0
}
