package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"

	"golang.org/x/crypto/curve25519"
)

// GenerateKeyPair generates an X25519 keypair
func GenerateKeyPair() (privateKey []byte, publicKey []byte, err error) {
	privateKey = make([]byte, curve25519.ScalarSize)
	if _, err := io.ReadFull(rand.Reader, privateKey); err != nil {
		return nil, nil, err
	}
	publicKey, err = curve25519.X25519(privateKey, curve25519.Basepoint)
	return privateKey, publicKey, err
}

// DeriveSharedKey generates a shared secret using X25519
func DeriveSharedKey(privateKey []byte, peerPublicKey []byte) ([]byte, error) {
	return curve25519.X25519(privateKey, peerPublicKey)
}

// GenerateRandomKey generates a random AES key
func GenerateRandomKey() []byte {
	key := make([]byte, 32)
	io.ReadFull(rand.Reader, key)
	return key
}

// Encrypt encrypts data using AES-GCM
func Encrypt(key []byte, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aesgcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	ciphertext := aesgcm.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

// Decrypt decrypts data using AES-GCM
func Decrypt(key []byte, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := aesgcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, errors.New("ciphertext too short")
	}
	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := aesgcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}
	return plaintext, nil
}

func Base64Encode(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

func Base64Decode(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}
