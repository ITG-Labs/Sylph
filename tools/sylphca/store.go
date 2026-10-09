package main

import (
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/ITG-Labs/sylph/pkg/mtls"
)

const (
	caCertFile  = "ca.pem"
	caKeyFile   = "ca.key"
	certsDir    = "certs"
	revokedFile = "revoked.json"
)

func initCA(dir string, name string, now time.Time) error {
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

func loadCA(dir string) (*mtls.CA, error) {
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

func issueCert(dir string, name string, usage mtls.LeafUsage, ttl time.Duration, now time.Time) error {
	ca, err := loadCA(dir)
	if err != nil {
		return err
	}
	leafDir := filepath.Join(dir, certsDir, name)
	certPath := filepath.Join(leafDir, "cert.pem")
	if _, err := os.Stat(certPath); err == nil {
		return fmt.Errorf("certificate %q already exists", name)
	}
	if err := os.MkdirAll(leafDir, 0o700); err != nil {
		return err
	}
	leaf, err := ca.Issue(name, usage, now, ttl)
	if err != nil {
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

type certInfo struct {
	Name     string
	Type     string
	Serial   string
	NotAfter time.Time
}

func loadRevoked(dir string) ([]string, error) {
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

func revokeCert(dir string, name string) error {
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
	serials, err := loadRevoked(dir)
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

func listCerts(dir string) ([]certInfo, error) {
	entries, err := os.ReadDir(filepath.Join(dir, certsDir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var infos []certInfo
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
		typ := "client"
		for _, eku := range cert.ExtKeyUsage {
			if eku == x509.ExtKeyUsageServerAuth {
				typ = "server"
			}
		}
		infos = append(infos, certInfo{
			Name:     entry.Name(),
			Type:     typ,
			Serial:   cert.SerialNumber.String(),
			NotAfter: cert.NotAfter,
		})
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })
	return infos, nil
}
