package crypto

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
)

func GenerateServerSeed() (seed string, hash string) {
	bytes := make([]byte, 32)
	rand.Read(bytes)

	seed = hex.EncodeToString(bytes)

	h := sha256.Sum256([]byte(seed))
	hash = hex.EncodeToString(h[:])

	return
}

func VerifySeed(seed, hash string) bool {
	h := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(h[:]) == hash
}
