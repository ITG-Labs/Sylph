package inbound

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ITG-Labs/sylph/pkg/netaddr"
)

const (
	ProtocolVLESS   = "vless"
	TransportTCP    = "tcp"
	SecurityReality = "reality"
	FlowVision      = "xtls-rprx-vision"
	PublicPort      = 443
)

var Fingerprints = []string{"chrome", "firefox", "safari", "ios", "android", "edge", "random"}

var tagPattern = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)

type Reality struct {
	Dest        string   `json:"dest"`
	ServerNames []string `json:"server_names"`
	ShortIDs    []string `json:"short_ids"`
	Fingerprint string   `json:"fingerprint"`
	PrivateKey  string   `json:"private_key"`
}

type Spec struct {
	Tag       string
	Port      int
	Protocol  string
	Transport string
	Security  string
	Label     string
	Reality   *Reality
	XHTTP     *XHTTP
	TLS       *TLS
}

type FieldError struct {
	Field   string
	Message string
}

func (e *FieldError) Error() string {
	return e.Field + ": " + e.Message
}

func fieldErr(field, format string, args ...any) error {
	return &FieldError{Field: field, Message: fmt.Sprintf(format, args...)}
}

func Flow(transport, security string) string {
	if transport == TransportTCP && security == SecurityReality {
		return FlowVision
	}
	return ""
}

func Normalize(s *Spec) error {
	if s.Protocol == "" {
		s.Protocol = ProtocolVLESS
	}
	if s.Transport == "" {
		s.Transport = TransportTCP
	}
	if s.Security == "" {
		s.Security = SecurityReality
	}
	if !tagPattern.MatchString(s.Tag) {
		return fieldErr("tag", "must be 1-32 characters of a-z, 0-9 and -")
	}
	if s.Port != PublicPort {
		return fieldErr("port", "only %d is supported", PublicPort)
	}
	if s.Protocol != ProtocolVLESS {
		return fieldErr("protocol", "only %s is supported", ProtocolVLESS)
	}
	if s.Transport != TransportTCP && s.Transport != TransportXHTTP {
		return fieldErr("transport", "must be %s or %s", TransportTCP, TransportXHTTP)
	}
	if s.Security != SecurityReality && s.Security != SecurityTLS {
		return fieldErr("security", "must be %s or %s", SecurityReality, SecurityTLS)
	}
	if s.Security == SecurityTLS && s.Transport != TransportXHTTP {
		return fieldErr("security", "%s works only with transport %s", SecurityTLS, TransportXHTTP)
	}
	if utf8.RuneCountInString(s.Label) > 32 {
		return fieldErr("label", "must be at most 32 characters")
	}
	if s.Label != "" && strings.TrimSpace(s.Label) == "" {
		return fieldErr("label", "must not be only spaces")
	}
	for _, r := range s.Label {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return fieldErr("label", "must not contain control characters")
		}
	}
	if err := normalizeTransport(s); err != nil {
		return err
	}
	if s.Security == SecurityTLS {
		if s.Reality != nil {
			return fieldErr("reality", "is only for security %s", SecurityReality)
		}
		if s.TLS == nil {
			return fieldErr("tls", "is required for security %s", SecurityTLS)
		}
		return normalizeTLS(s.TLS)
	}
	if s.TLS != nil {
		return fieldErr("tls", "is only for security %s", SecurityTLS)
	}
	if s.Reality == nil {
		return fieldErr("reality", "is required for security reality")
	}
	return normalizeReality(s.Reality)
}

func normalizeReality(r *Reality) error {
	host, port, err := net.SplitHostPort(r.Dest)
	if err != nil {
		return fieldErr("reality.dest", "must be host:port")
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 1 || p > 65535 {
		return fieldErr("reality.dest", "port must be 1-65535")
	}
	port = strconv.Itoa(p)
	host = strings.ToLower(host)
	if net.ParseIP(host) == nil && (!netaddr.IsHostname(host) || !netaddr.LetterTLD(host)) {
		return fieldErr("reality.dest", "host must be a hostname or an IP in its usual notation")
	}
	host = CanonicalHost(host)
	if netaddr.Internal(host) || unroutableHost(host) {
		return fieldErr("reality.dest", "must not point to localhost, a private, link-local or special address")
	}
	r.Dest = net.JoinHostPort(host, port)
	if len(r.ServerNames) == 0 {
		return fieldErr("reality.server_names", "must not be empty")
	}
	names := make([]string, 0, len(r.ServerNames))
	for _, n := range r.ServerNames {
		n = strings.ToLower(n)
		if !netaddr.IsHostname(n) || !strings.Contains(n, ".") || net.ParseIP(n) != nil {
			return fieldErr("reality.server_names", "%q is not a hostname", n)
		}
		if slices.Contains(names, n) {
			return fieldErr("reality.server_names", "%q is listed twice", n)
		}
		names = append(names, n)
	}
	r.ServerNames = names
	if len(r.ShortIDs) == 0 {
		r.ShortIDs = []string{NewShortID()}
	}
	ids := make([]string, 0, len(r.ShortIDs))
	for _, id := range r.ShortIDs {
		id = strings.ToLower(id)
		if len(id) < 2 || len(id) > 16 || len(id)%2 != 0 {
			return fieldErr("reality.short_ids", "each must be 2-16 hex characters, even length")
		}
		if _, err := hex.DecodeString(id); err != nil {
			return fieldErr("reality.short_ids", "each must be hex")
		}
		if slices.Contains(ids, id) {
			return fieldErr("reality.short_ids", "%q is listed twice", id)
		}
		ids = append(ids, id)
	}
	r.ShortIDs = ids
	if r.Fingerprint == "" {
		r.Fingerprint = "chrome"
	}
	if !slices.Contains(Fingerprints, r.Fingerprint) {
		return fieldErr("reality.fingerprint", "must be one of %s", strings.Join(Fingerprints, ", "))
	}
	if r.PrivateKey == "" {
		key, err := NewPrivateKey()
		if err != nil {
			return err
		}
		r.PrivateKey = key
	}
	if _, err := PublicKey(r.PrivateKey); err != nil {
		return fieldErr("reality.private_key", "must be 32 bytes in unpadded base64url (xray x25519 format)")
	}
	return nil
}

func CanonicalHost(host string) string {
	return netaddr.CanonicalHost(host)
}

func DestHost(dest string) string {
	host, _, err := net.SplitHostPort(dest)
	if err != nil {
		return ""
	}
	return CanonicalHost(host)
}

func IsAddress(h string) bool {
	if net.ParseIP(h) != nil {
		return true
	}
	return netaddr.IsHostname(strings.ToLower(h)) && strings.Contains(h, ".")
}

func NewPrivateKey() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[0] &= 248
	b[31] &= 127
	b[31] |= 64
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

func PublicKey(privateKey string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(privateKey)
	if err != nil {
		return "", fmt.Errorf("decode private key: %w", err)
	}
	key, err := ecdh.X25519().NewPrivateKey(raw)
	if err != nil {
		return "", fmt.Errorf("parse private key: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()), nil
}

func NewShortID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

func ServerNames(s *Spec) []string {
	if s.Reality == nil {
		return nil
	}
	return s.Reality.ServerNames
}
