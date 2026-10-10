package etoro

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"
)

// refNamespace is the UUIDv5 namespace for client references. Fixed forever:
// changing it changes every derived request id and breaks resume of orders
// placed before the change.
var refNamespace = mustUUID("8c0f4e2a-6b1d-5f3e-9a47-2d5e0c7b1f93")

// NewRequestID returns a random (version 4) UUID.
func NewRequestID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return formatUUID(b)
}

// RequestIDFor derives the x-request-id for a client reference such as
// "sma50-NSDQ100-2026-10-13-open". The same reference always yields the
// same UUID (version 5), so a resumed run can find its order with
// LookupOrderByReference(RequestIDFor(ref)) instead of opening twice.
func RequestIDFor(clientRef string) string {
	return uuidV5(refNamespace, clientRef)
}

func uuidV5(ns [16]byte, name string) string {
	h := sha1.New()
	h.Write(ns[:])
	h.Write([]byte(name))
	var b [16]byte
	copy(b[:], h.Sum(nil))
	b[6] = (b[6] & 0x0f) | 0x50
	b[8] = (b[8] & 0x3f) | 0x80
	return formatUUID(b)
}

func formatUUID(b [16]byte) string {
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]), hex.EncodeToString(b[4:6]), hex.EncodeToString(b[6:8]),
		hex.EncodeToString(b[8:10]), hex.EncodeToString(b[10:16]))
}

func mustUUID(s string) [16]byte {
	raw, err := hex.DecodeString(strings.ReplaceAll(s, "-", ""))
	if err != nil || len(raw) != 16 {
		panic("etoro: bad UUID literal " + s)
	}
	var b [16]byte
	copy(b[:], raw)
	return b
}
