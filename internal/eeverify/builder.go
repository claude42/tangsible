package eeverify

import (
	"crypto/rand"
	"encoding/hex"
)

// TagPrefix is the repository of the throw-away images; localhost/ keeps
// podman from ever trying to pull one.
const TagPrefix = "localhost/tangsible-ee-verify:"

// NewTag is a fresh throw-away image name.
func NewTag() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return TagPrefix + "verify"
	}
	return TagPrefix + hex.EncodeToString(b[:])
}
