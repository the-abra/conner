package crypto

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/big"
)

// PoWDifficulty defines the number of leading zeros required (bits)
const PoWDifficulty = 16

// ComputePoW finds a nonce such that SHA256(data + nonce) has difficulty leading zero bits.
func ComputePoW(data []byte, difficulty int) uint64 {
	var nonce uint64
	target := big.NewInt(1)
	target.Lsh(target, uint(256-difficulty))

	for {
		hash := sha256.Sum256(append(data, uint64ToBytes(nonce)...))
		hashInt := new(big.Int).SetBytes(hash[:])

		if hashInt.Cmp(target) == -1 {
			return nonce
		}
		nonce++
	}
}

// VerifyPoW checks if SHA256(data + nonce) meets the difficulty requirement.
func VerifyPoW(data []byte, nonce uint64, difficulty int) bool {
	target := big.NewInt(1)
	target.Lsh(target, uint(256-difficulty))

	hash := sha256.Sum256(append(data, uint64ToBytes(nonce)...))
	hashInt := new(big.Int).SetBytes(hash[:])

	return hashInt.Cmp(target) == -1
}

func uint64ToBytes(n uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, n)
	return b
}

func GetPoWTarget(difficulty int) string {
	return fmt.Sprintf("%d", difficulty)
}
