package panel

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	defaultIterations = 600000
	minIterations     = 100000
	passwordScheme    = "pbkdf2-sha256"
	passwordKeyLen    = 32
)

var b64 = base64.RawStdEncoding

type passwordHash struct {
	iter int
	salt []byte
	key  []byte
}

func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	return hashPassword(password, salt, defaultIterations)
}

func hashPassword(password string, salt []byte, iter int) (string, error) {
	key, err := pbkdf2.Key(sha256.New, password, salt, iter, passwordKeyLen)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s$%d$%s$%s", passwordScheme, iter, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

func parsePasswordHash(s string) (*passwordHash, error) {
	parts := strings.Split(s, "$")
	if len(parts) != 4 || parts[0] != passwordScheme {
		return nil, errors.New("password hash must look like pbkdf2-sha256$<iterations>$<salt>$<key>")
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter < 1 {
		return nil, errors.New("password hash: bad iteration count")
	}
	salt, err := b64.DecodeString(parts[2])
	if err != nil || len(salt) == 0 {
		return nil, errors.New("password hash: bad salt")
	}
	key, err := b64.DecodeString(parts[3])
	if err != nil || len(key) == 0 {
		return nil, errors.New("password hash: bad key")
	}
	return &passwordHash{iter: iter, salt: salt, key: key}, nil
}

func (p *passwordHash) verify(password string) bool {
	key, err := pbkdf2.Key(sha256.New, password, p.salt, p.iter, len(p.key))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(key, p.key) == 1
}
