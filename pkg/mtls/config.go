package mtls

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
)

type ServerConfig struct {
	CertPEM     []byte
	KeyPEM      []byte
	ClientCAPEM []byte
}

func NewServerTLSConfig(cfg ServerConfig) (*tls.Config, error) {
	cert, err := tls.X509KeyPair(cfg.CertPEM, cfg.KeyPEM)
	if err != nil {
		return nil, fmt.Errorf("load server keypair: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(cfg.ClientCAPEM) {
		return nil, fmt.Errorf("parse client CA certificate")
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    pool,
		MinVersion:   tls.VersionTLS12,
	}, nil
}

type ClientConfig struct {
	CertPEM    []byte
	KeyPEM     []byte
	CAPEM      []byte
	ServerName string
}

func NewClientTLSConfig(cfg ClientConfig) (*tls.Config, error) {
	cert, err := tls.X509KeyPair(cfg.CertPEM, cfg.KeyPEM)
	if err != nil {
		return nil, fmt.Errorf("load client keypair: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(cfg.CAPEM) {
		return nil, fmt.Errorf("parse CA certificate")
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      pool,
		ServerName:   cfg.ServerName,
		MinVersion:   tls.VersionTLS12,
	}, nil
}
