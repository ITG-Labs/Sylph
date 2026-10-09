package main

import (
	"fmt"
	"os"
	"path/filepath"
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
