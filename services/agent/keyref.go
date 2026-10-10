package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

func keyRef(uuid string) string {
	sum := sha256.Sum256([]byte(uuid))
	return hex.EncodeToString(sum[:6])
}

func redactKey(s string, uuid string) string {
	if uuid == "" {
		return s
	}
	return strings.ReplaceAll(s, uuid, keyRef(uuid))
}
