package panel

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxAdminNameRunes = 64

type AdminRecord struct {
	Name         string `json:"name"`
	PasswordHash string `json:"password_hash"`
	TOTPSecret   string `json:"totp_secret"`
}

type admin struct {
	name     string
	password *passwordHash
	secret   []byte
}

func LoadAdmins(path string) (map[string]*admin, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read admins file: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var recs []AdminRecord
	if err := dec.Decode(&recs); err != nil {
		return nil, fmt.Errorf("admins file: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("admins file: unexpected data after the list")
	}
	if len(recs) == 0 {
		return nil, errors.New("admins file: no admins")
	}
	out := make(map[string]*admin, len(recs))
	for i, r := range recs {
		a, err := r.parse()
		if err != nil {
			return nil, fmt.Errorf("admins file, entry %d: %w", i+1, err)
		}
		if _, dup := out[a.name]; dup {
			return nil, fmt.Errorf("admins file, entry %d: duplicate name %q", i+1, a.name)
		}
		out[a.name] = a
	}
	return out, nil
}

func (r AdminRecord) parse() (*admin, error) {
	if err := validAdminName(r.Name); err != nil {
		return nil, err
	}
	ph, err := parsePasswordHash(r.PasswordHash)
	if err != nil {
		return nil, err
	}
	if ph.iter < minIterations {
		return nil, fmt.Errorf("password hash: at least %d iterations required", minIterations)
	}
	secret, err := decodeTOTPSecret(r.TOTPSecret)
	if err != nil {
		return nil, err
	}
	return &admin{name: r.Name, password: ph, secret: secret}, nil
}

func validAdminName(name string) error {
	if name == "" || strings.TrimSpace(name) != name {
		return errors.New("name must be non-empty without leading or trailing spaces")
	}
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) > maxAdminNameRunes {
		return fmt.Errorf("name must be valid UTF-8, at most %d characters", maxAdminNameRunes)
	}
	for _, c := range name {
		if unicode.IsControl(c) || c == '\u2028' || c == '\u2029' {
			return errors.New("name must not contain control characters")
		}
	}
	return nil
}

func NewAdmin(name string) (AdminRecord, string, string, error) {
	if err := validAdminName(name); err != nil {
		return AdminRecord{}, "", "", err
	}
	pw := make([]byte, 18)
	secret := make([]byte, 20)
	if _, err := rand.Read(pw); err != nil {
		return AdminRecord{}, "", "", err
	}
	if _, err := rand.Read(secret); err != nil {
		return AdminRecord{}, "", "", err
	}
	password := base64.RawURLEncoding.EncodeToString(pw)
	hash, err := HashPassword(password)
	if err != nil {
		return AdminRecord{}, "", "", err
	}
	rec := AdminRecord{Name: name, PasswordHash: hash, TOTPSecret: totpEncoding.EncodeToString(secret)}
	q := url.Values{}
	q.Set("secret", rec.TOTPSecret)
	q.Set("issuer", "Sylph")
	q.Set("algorithm", "SHA1")
	q.Set("digits", "6")
	q.Set("period", "30")
	u := url.URL{Scheme: "otpauth", Host: "totp", Path: "/Sylph:" + name, RawQuery: q.Encode()}
	return rec, password, u.String(), nil
}
