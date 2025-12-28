package game

import (
	"crypto/sha256"
	"encoding/binary"
	"math/rand"
)

func NewSeededRNG(seed string) *rand.Rand {
	hash := sha256.Sum256([]byte(seed))
	seedInt := int64(binary.BigEndian.Uint64(hash[:8]))
	return rand.New(rand.NewSource(seedInt))
}
