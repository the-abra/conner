package crypto

import (
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"sync"
)

// Ratchet implements a symmetric-key forward secrecy ratchet.
type Ratchet struct {
	initialKey []byte
	mu       sync.Mutex
	chainKey []byte
	step     uint32

	// We can store skipped message keys if we receive out of order, 
	// but for simplicity we'll keep a basic window.
	skippedKeys map[uint32][]byte
}

func NewRatchet(initialKey []byte) *Ratchet {
	ck := make([]byte, len(initialKey))
	copy(ck, initialKey)
	return &Ratchet{
		chainKey:    ck,
		initialKey:  ck,
		step:        0,
		skippedKeys: make(map[uint32][]byte),
	}
}

// kdf derives the Message Key and the next Chain Key from the current Chain Key
func kdf(chainKey []byte) (messageKey, nextChainKey []byte) {
	mac := hmac.New(sha256.New, chainKey)
	mac.Write([]byte{0x01})
	messageKey = mac.Sum(nil)

	mac.Reset()
	mac.Write([]byte{0x02})
	nextChainKey = mac.Sum(nil)

	return messageKey, nextChainKey
}

// Next derives the next message key to send or receive a message sequentially.
func (r *Ratchet) GetInitialKey() []byte {
	return r.initialKey
}

func (r *Ratchet) Next() (msgKey []byte, step uint32) {
	r.mu.Lock()
	defer r.mu.Unlock()

	mk, nextCk := kdf(r.chainKey)
	r.chainKey = nextCk
	
	currentStep := r.step
	r.step++

	return mk, currentStep
}

// GetMessageKey gets a message key for a specific step, advancing the ratchet if necessary.
func (r *Ratchet) GetMessageKey(targetStep uint32) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if it's an old skipped key
	if mk, ok := r.skippedKeys[targetStep]; ok {
		delete(r.skippedKeys, targetStep)
		return mk, nil
	}

	// If the target step is in the past and not in skipped keys, we cannot decrypt it
	if targetStep < r.step {
		return nil, errors.New("cannot decrypt past messages (forward secrecy enforced)")
	}

	// If it's too far in the future, limit the catch-up to prevent DoS
	if targetStep - r.step > 100 {
		return nil, errors.New("target step is too far ahead")
	}

	// Ratchet forward until we reach the target step
	for r.step < targetStep {
		mk, nextCk := kdf(r.chainKey)
		r.skippedKeys[r.step] = mk
		r.chainKey = nextCk
		r.step++
	}

	// Now we are at targetStep
	mk, nextCk := kdf(r.chainKey)
	r.chainKey = nextCk
	r.step++

	return mk, nil
}
