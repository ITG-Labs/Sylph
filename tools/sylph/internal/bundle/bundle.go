package bundle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/ITG-Labs/sylph/pkg/castore"
	"github.com/ITG-Labs/sylph/pkg/mtls"
)

const (
	Prefix       = "sylph-certs:"
	Ext          = ".sylph-certs"
	manifestName = "sylph-bundle.json"
	formatV1     = 1
	maxBundle    = 1 << 20
	maxEntry     = 256 << 10
	RevokedFile  = "revoked.json"
	CAFile       = "ca.pem"
)

type Manifest struct {
	Version int       `json:"version"`
	Role    string    `json:"role"`
	Name    string    `json:"name"`
	Created time.Time `json:"created"`
}

type Bundle struct {
	Manifest Manifest
	Files    map[string][]byte
}

type pair struct {
	cert, key string
	server    bool
	role      string
}

type Leaf struct {
	Path     string
	Name     string
	Role     string
	Serial   string
	NotAfter time.Time
}

func pairs(role string) []pair {
	if role == "core" {
		return []pair{
			{cert: "core/cert.pem", key: "core/key.pem", server: true},
			{cert: "cron/cert.pem", key: "cron/key.pem", role: mtls.RoleCron},
		}
	}
	return []pair{{cert: "cert.pem", key: "key.pem", role: role}}
}

func Paths(role string) []string {
	paths := []string{CAFile}
	for _, p := range pairs(role) {
		paths = append(paths, p.cert, p.key)
	}
	if role == "core" {
		paths = append(paths, RevokedFile)
	}
	return paths
}

func allowed(role string, path string) bool {
	for _, p := range Paths(role) {
		if p == path {
			return true
		}
	}
	return false
}

func LeafNames(role string, name string, coreName string) []string {
	if role == "core" {
		return []string{coreName, mtls.RoleCron}
	}
	return []string{name}
}

func Collect(caDir string, role string, name string, coreName string, now time.Time) (*Bundle, error) {
	b := &Bundle{
		Manifest: Manifest{Version: formatV1, Role: role, Name: name, Created: now.UTC()},
		Files:    map[string][]byte{},
	}
	ca, err := os.ReadFile(castore.CACertPath(caDir))
	if err != nil {
		return nil, err
	}
	b.Files[CAFile] = ca
	names := LeafNames(role, name, coreName)
	for i, p := range pairs(role) {
		certPath, keyPath := castore.LeafPaths(caDir, names[i])
		if b.Files[p.cert], err = os.ReadFile(certPath); err != nil {
			return nil, err
		}
		if b.Files[p.key], err = os.ReadFile(keyPath); err != nil {
			return nil, err
		}
	}
	if role == "core" {
		revoked, err := os.ReadFile(castore.RevokedPath(caDir))
		switch {
		case err == nil:
			b.Files[RevokedFile] = revoked
		case !errors.Is(err, fs.ErrNotExist):
			return nil, err
		}
	}
	return b, nil
}

func (b *Bundle) Encode() ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	manifest, err := json.Marshal(b.Manifest)
	if err != nil {
		return nil, err
	}
	write := func(name string, data []byte) error {
		hdr := &tar.Header{Name: name, Mode: 0o600, Size: int64(len(data)), ModTime: b.Manifest.Created, Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		_, err := tw.Write(data)
		return err
	}
	if err := write(manifestName, manifest); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(b.Files))
	for name := range b.Files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := write(name, b.Files[name]); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (b *Bundle) Inline() (string, error) {
	data, err := b.Encode()
	if err != nil {
		return "", err
	}
	return Prefix + base64.StdEncoding.EncodeToString(data), nil
}

func Load(input string) (*Bundle, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, errors.New("нужен путь к пакету или строка sylph-certs:…")
	}
	if rest, ok := strings.CutPrefix(input, Prefix); ok {
		data, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(rest), ""))
		if err != nil {
			return nil, errors.New("строка пакета повреждена (base64)")
		}
		return Decode(data)
	}
	f, err := os.Open(input)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("нет файла %s", input)
		}
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxBundle+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxBundle {
		return nil, errors.New("файл слишком большой для пакета сертов")
	}
	return Decode(data)
}

func Decode(data []byte) (*Bundle, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("это не пакет сертов sylph")
	}
	tr := tar.NewReader(io.LimitReader(gz, maxBundle))
	b := &Bundle{Files: map[string][]byte{}}
	var manifest []byte
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, errors.New("пакет сертов повреждён")
		}
		if hdr.Typeflag != tar.TypeReg || hdr.Size > maxEntry {
			return nil, fmt.Errorf("лишнее в пакете: %s", hdr.Name)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, errors.New("пакет сертов повреждён")
		}
		if hdr.Name == manifestName {
			manifest = data
			continue
		}
		if _, dup := b.Files[hdr.Name]; dup {
			return nil, fmt.Errorf("файл %s в пакете дважды", hdr.Name)
		}
		b.Files[hdr.Name] = data
	}
	if manifest == nil {
		return nil, errors.New("это не пакет сертов sylph (нет описи)")
	}
	if err := json.Unmarshal(manifest, &b.Manifest); err != nil {
		return nil, errors.New("опись пакета повреждена")
	}
	if b.Manifest.Version != formatV1 {
		return nil, fmt.Errorf("пакет формата %d, этот sylph понимает %d", b.Manifest.Version, formatV1)
	}
	for name := range b.Files {
		if !allowed(b.Manifest.Role, name) {
			return nil, fmt.Errorf("лишний файл в пакете: %s", name)
		}
	}
	return b, nil
}

func (b *Bundle) Verify(role string, coreName string, now time.Time) ([]Leaf, error) {
	if b.Manifest.Role != role {
		return nil, fmt.Errorf("пакет для роли %s, а ставим %s", b.Manifest.Role, role)
	}
	for _, p := range Paths(role) {
		if _, ok := b.Files[p]; !ok && p != RevokedFile {
			return nil, fmt.Errorf("в пакете нет %s", p)
		}
	}
	ca, err := mtls.ParseCertPEM(b.Files[CAFile])
	if err != nil || !ca.IsCA {
		return nil, errors.New("ca.pem в пакете — не серт CA")
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	var revoked map[string]bool
	if data, ok := b.Files[RevokedFile]; ok {
		var list struct {
			Serials []string `json:"serials"`
		}
		if err := json.Unmarshal(data, &list); err != nil {
			return nil, errors.New("revoked.json в пакете повреждён")
		}
		revoked = map[string]bool{}
		for _, s := range list.Serials {
			revoked[s] = true
		}
	}
	var leaves []Leaf
	for _, p := range pairs(role) {
		leaf, err := verifyPair(b, p, roots, coreName, now)
		if err != nil {
			return nil, err
		}
		if revoked[leaf.Serial] {
			return nil, fmt.Errorf("%s: серт %s отозван в revoked.json этого же пакета", p.cert, leaf.Name)
		}
		leaves = append(leaves, leaf)
	}
	return leaves, nil
}

func verifyPair(b *Bundle, p pair, roots *x509.CertPool, coreName string, now time.Time) (Leaf, error) {
	cert, err := mtls.ParseCertPEM(b.Files[p.cert])
	if err != nil {
		return Leaf{}, fmt.Errorf("%s: не серт", p.cert)
	}
	usage := x509.ExtKeyUsageClientAuth
	if p.server {
		usage = x509.ExtKeyUsageServerAuth
	}
	if _, err := cert.Verify(x509.VerifyOptions{Roots: roots, CurrentTime: now, KeyUsages: []x509.ExtKeyUsage{usage}}); err != nil {
		var invalid x509.CertificateInvalidError
		if errors.As(err, &invalid) && invalid.Reason == x509.Expired {
			return Leaf{}, fmt.Errorf("%s: серт %s истёк или ещё не начал действовать (проверь часы)", p.cert, cert.Subject.CommonName)
		}
		return Leaf{}, fmt.Errorf("%s: серт %s не подписан CA из пакета или не для этой цели", p.cert, cert.Subject.CommonName)
	}
	key, err := mtls.ParseKeyPEM(b.Files[p.key])
	if err != nil {
		return Leaf{}, fmt.Errorf("%s: не ключ", p.key)
	}
	pub, ok := cert.PublicKey.(*ecdsa.PublicKey)
	if !ok || !pub.Equal(&key.PublicKey) {
		return Leaf{}, fmt.Errorf("%s не от серта %s", p.key, p.cert)
	}
	leaf := Leaf{Path: p.cert, Name: cert.Subject.CommonName, Serial: cert.SerialNumber.String(), NotAfter: cert.NotAfter}
	if p.server {
		if err := cert.VerifyHostname(coreName); err != nil {
			return Leaf{}, fmt.Errorf("серт ядра выпущен на %s, а SYLPH_CORE_NAME = %s", strings.Join(cert.DNSNames, ", "), coreName)
		}
		return leaf, nil
	}
	got, _ := mtls.RoleOf(cert)
	if got != p.role {
		return Leaf{}, fmt.Errorf("%s: серт %s с ролью %q, а нужна %q", p.cert, cert.Subject.CommonName, got, p.role)
	}
	leaf.Role = got
	return leaf, nil
}

func Earliest(leaves []Leaf) Leaf {
	var first Leaf
	for i, l := range leaves {
		if i == 0 || l.NotAfter.Before(first.NotAfter) {
			first = l
		}
	}
	return first
}
