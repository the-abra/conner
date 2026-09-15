package crypto

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
)

const ChunkSize = 64 * 1024

// EncryptChunk seals one file chunk. AAD binds fileID + index so chunks cannot be swapped.
func EncryptChunk(key []byte, fileID string, idx uint32, plaintext []byte) ([]byte, error) {
	aad := chunkAAD(fileID, idx)
	return encryptWithAAD(key, plaintext, aad)
}

func DecryptChunk(key []byte, fileID string, idx uint32, ciphertext []byte) ([]byte, error) {
	aad := chunkAAD(fileID, idx)
	return decryptWithAAD(key, ciphertext, aad)
}

// FileContentKey derives a per-file AEAD key from a capability token (hex or raw).
func FileContentKey(token, fileID string) []byte {
	h := sha256.New()
	h.Write([]byte("conner-file-v1"))
	h.Write([]byte(token))
	h.Write([]byte(fileID))
	return h.Sum(nil)
}

func FileSHA256(r io.Reader) ([]byte, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

func chunkAAD(fileID string, idx uint32) []byte {
	buf := make([]byte, 4+len(fileID))
	binary.BigEndian.PutUint32(buf[:4], idx)
	copy(buf[4:], fileID)
	return buf
}

func encryptWithAAD(key, plaintext, aad []byte) ([]byte, error) {
	block, err := aesGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, block.NonceSize())
	if _, err := io.ReadFull(randReader(), nonce); err != nil {
		return nil, err
	}
	return block.Seal(nonce, nonce, plaintext, aad), nil
}

func decryptWithAAD(key, ciphertext, aad []byte) ([]byte, error) {
	block, err := aesGCM(key)
	if err != nil {
		return nil, err
	}
	ns := block.NonceSize()
	if len(ciphertext) < ns {
		return nil, errors.New("ciphertext too short")
	}
	return block.Open(nil, ciphertext[:ns], ciphertext[ns:], aad)
}
