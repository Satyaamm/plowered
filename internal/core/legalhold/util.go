package legalhold

import (
	"crypto/rand"
	"encoding/hex"
)

// Tiny indirection so newID can be tested without touching crypto/rand.
var randRead = rand.Read

func hexEncode(b []byte) string { return hex.EncodeToString(b) }
