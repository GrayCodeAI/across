package store

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// NewID returns prefix + "_" + 12 hex chars + time-based suffix for ordering.
func NewID(prefix string) string {
	var b [9]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%s_%s%04d", prefix, hex.EncodeToString(b[:]), time.Now().Unix()%10000)
}

// NowUTC returns RFC3339 UTC time.
func NowUTC() string { return time.Now().UTC().Format(time.RFC3339Nano) }
