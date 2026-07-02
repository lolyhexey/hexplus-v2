package panel

import (
	"crypto/rand"
	"encoding/hex"
)

// encoding.go: tiny wrappers over stdlib crypto helpers so security.go
// stays readable and doesn't import crypto/rand directly for one call.

func randRead(buf []byte) (int, error) { return rand.Read(buf) }
func hexEncode(b []byte) string          { return hex.EncodeToString(b) }
