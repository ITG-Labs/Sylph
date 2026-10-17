package machine

import (
	"crypto/tls"
	"crypto/x509"

	"github.com/ITG-Labs/sylph/pkg/mtls"
)

func tlsPair(certPEM []byte, keyPEM []byte) (tls.Certificate, error) {
	return tls.X509KeyPair(certPEM, keyPEM)
}

func serverTLS(cert tls.Certificate, ca *mtls.CA) *tls.Config {
	pool := x509.NewCertPool()
	pool.AddCert(ca.Cert)
	return &tls.Config{Certificates: []tls.Certificate{cert}, ClientCAs: pool, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS13}
}
