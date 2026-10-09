package mtls

import (
	"crypto/x509"
	"net/http"
)

type RevocationList struct {
	serials map[string]struct{}
}

func NewRevocationList(serials []string) *RevocationList {
	r := &RevocationList{serials: make(map[string]struct{}, len(serials))}
	for _, s := range serials {
		r.serials[s] = struct{}{}
	}
	return r
}

func (r *RevocationList) IsRevoked(cert *x509.Certificate) bool {
	if cert == nil {
		return false
	}
	_, ok := r.serials[cert.SerialNumber.String()]
	return ok
}

func (r *RevocationList) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.TLS == nil || len(req.TLS.PeerCertificates) == 0 {
			http.Error(w, "client certificate required", http.StatusUnauthorized)
			return
		}
		if r.IsRevoked(req.TLS.PeerCertificates[0]) {
			http.Error(w, "certificate revoked", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, req)
	})
}
