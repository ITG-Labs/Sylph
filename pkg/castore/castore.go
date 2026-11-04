package castore

import (
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ITG-Labs/sylph/pkg/mtls"
)

const (
	caCertFile  = "ca.pem"
	caKeyFile   = "ca.key"
	certsDir    = "certs"
	archiveDir  = "archive"
	revokedFile = "revoked.json"
)

var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

func ValidName(name string) error {
	if !namePattern.MatchString(name) || strings.Contains(name, "..") {
		return fmt.Errorf("invalid certificate name %q: use letters, digits, dot, dash or underscore", name)
	}
	return nil
}

func Exists(dir string) (bool, error) {
	_, err := os.Stat(filepath.Join(dir, caCertFile))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

func CACertPath(dir string) string {
	return filepath.Join(dir, caCertFile)
}

func RevokedPath(dir string) string {
	return filepath.Join(dir, revokedFile)
}

func LeafPaths(dir string, name string) (string, string) {
	leafDir := filepath.Join(dir, certsDir, name)
	return filepath.Join(leafDir, "cert.pem"), filepath.Join(leafDir, "key.pem")
}

func HasLeaf(dir string, name string) (bool, error) {
	if err := ValidName(name); err != nil {
		return false, err
	}
	certPath, _ := LeafPaths(dir, name)
	_, err := os.Stat(certPath)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

func Init(dir string, name string, now time.Time) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	certPath := filepath.Join(dir, caCertFile)
	if _, err := os.Stat(certPath); err == nil {
		return fmt.Errorf("%s already exists", certPath)
	}
	ca, err := mtls.NewCA(name, now)
	if err != nil {
		return err
	}
	if err := os.WriteFile(certPath, mtls.EncodeCertPEM(ca.Cert), 0o644); err != nil {
		return err
	}
	keyPEM, err := mtls.EncodeKeyPEM(ca.Key)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, caKeyFile), keyPEM, 0o600)
}

func Load(dir string) (*mtls.CA, error) {
	certPEM, err := os.ReadFile(filepath.Join(dir, caCertFile))
	if err != nil {
		return nil, err
	}
	cert, err := mtls.ParseCertPEM(certPEM)
	if err != nil {
		return nil, err
	}
	keyPEM, err := os.ReadFile(filepath.Join(dir, caKeyFile))
	if err != nil {
		return nil, err
	}
	key, err := mtls.ParseKeyPEM(keyPEM)
	if err != nil {
		return nil, err
	}
	return &mtls.CA{Cert: cert, Key: key}, nil
}

func Issue(dir string, name string, usage mtls.LeafUsage, role string, ttl time.Duration, now time.Time) error {
	if err := ValidName(name); err != nil {
		return err
	}
	ca, err := Load(dir)
	if err != nil {
		return err
	}
	leafDir := filepath.Join(dir, certsDir, name)
	certPath := filepath.Join(leafDir, "cert.pem")
	if _, err := os.Stat(certPath); err == nil {
		return fmt.Errorf("certificate %q already exists", name)
	}
	leaf, err := ca.Issue(name, usage, role, now, ttl)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(leafDir, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(certPath, mtls.EncodeCertPEM(leaf.Cert), 0o644); err != nil {
		return err
	}
	keyPEM, err := mtls.EncodeKeyPEM(leaf.Key)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(leafDir, "key.pem"), keyPEM, 0o600)
}

type CertInfo struct {
	Name     string
	Type     string
	Role     string
	Serial   string
	NotAfter time.Time
}

func LoadRevoked(dir string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(dir, revokedFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var list struct {
		Serials []string `json:"serials"`
	}
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("parse %s: %w", revokedFile, err)
	}
	return list.Serials, nil
}

func Revoke(dir string, name string) error {
	if err := ValidName(name); err != nil {
		return err
	}
	certPEM, err := os.ReadFile(filepath.Join(dir, certsDir, name, "cert.pem"))
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("certificate %q not found", name)
	}
	if err != nil {
		return err
	}
	cert, err := mtls.ParseCertPEM(certPEM)
	if err != nil {
		return err
	}
	serial := cert.SerialNumber.String()
	serials, err := LoadRevoked(dir)
	if err != nil {
		return err
	}
	for _, s := range serials {
		if s == serial {
			return fmt.Errorf("certificate %q already revoked", name)
		}
	}
	serials = append(serials, serial)
	data, err := json.Marshal(struct {
		Serials []string `json:"serials"`
	}{Serials: serials})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, revokedFile), append(data, '\n'), 0o644)
}

func IsRevoked(dir string, serial string) (bool, error) {
	serials, err := LoadRevoked(dir)
	if err != nil {
		return false, err
	}
	for _, s := range serials {
		if s == serial {
			return true, nil
		}
	}
	return false, nil
}

func Inspect(dir string, name string) (CertInfo, error) {
	if err := ValidName(name); err != nil {
		return CertInfo{}, err
	}
	certPEM, err := os.ReadFile(filepath.Join(dir, certsDir, name, "cert.pem"))
	if errors.Is(err, fs.ErrNotExist) {
		return CertInfo{}, fmt.Errorf("certificate %q not found", name)
	}
	if err != nil {
		return CertInfo{}, err
	}
	cert, err := mtls.ParseCertPEM(certPEM)
	if err != nil {
		return CertInfo{}, err
	}
	return certInfo(name, cert), nil
}

func Renew(dir string, name string, ttl time.Duration, now time.Time) error {
	if err := ValidName(name); err != nil {
		return err
	}
	certPEM, err := os.ReadFile(filepath.Join(dir, certsDir, name, "cert.pem"))
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("certificate %q not found", name)
	}
	if err != nil {
		return err
	}
	cert, err := mtls.ParseCertPEM(certPEM)
	if err != nil {
		return err
	}
	info := certInfo(name, cert)
	usage := mtls.ClientUsage
	if info.Type == "server" {
		usage = mtls.ServerUsage
	}
	leafDir := filepath.Join(dir, certsDir, name)
	archived, err := archiveLeaf(dir, name, leafDir, now)
	if err != nil {
		return err
	}
	if err := Issue(dir, name, usage, info.Role, ttl, now); err != nil {
		os.RemoveAll(leafDir)
		if rerr := os.Rename(archived, leafDir); rerr != nil {
			return fmt.Errorf("%w (restore from %s failed: %v)", err, archived, rerr)
		}
		return err
	}
	return nil
}

func archiveLeaf(dir string, name string, leafDir string, now time.Time) (string, error) {
	if err := os.MkdirAll(filepath.Join(dir, archiveDir), 0o700); err != nil {
		return "", err
	}
	base := filepath.Join(dir, archiveDir, name+"-"+now.UTC().Format("20060102-150405"))
	target := base
	for i := 2; ; i++ {
		if _, err := os.Stat(target); errors.Is(err, fs.ErrNotExist) {
			break
		} else if err != nil {
			return "", err
		}
		target = fmt.Sprintf("%s-%d", base, i)
	}
	if err := os.Rename(leafDir, target); err != nil {
		return "", err
	}
	return target, nil
}

func certInfo(name string, cert *x509.Certificate) CertInfo {
	typ := "client"
	for _, eku := range cert.ExtKeyUsage {
		if eku == x509.ExtKeyUsageServerAuth {
			typ = "server"
		}
	}
	role, _ := mtls.RoleOf(cert)
	return CertInfo{Name: name, Type: typ, Role: role, Serial: cert.SerialNumber.String(), NotAfter: cert.NotAfter}
}

func List(dir string) ([]CertInfo, error) {
	entries, err := os.ReadDir(filepath.Join(dir, certsDir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var infos []CertInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		certPEM, err := os.ReadFile(filepath.Join(dir, certsDir, entry.Name(), "cert.pem"))
		if err != nil {
			return nil, err
		}
		cert, err := mtls.ParseCertPEM(certPEM)
		if err != nil {
			return nil, err
		}
		infos = append(infos, certInfo(entry.Name(), cert))
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })
	return infos, nil
}
