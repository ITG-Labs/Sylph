package api

import (
	"net/http"

	"github.com/ITG-Labs/sylph/pkg/logging"
)

func (s *Server) middleware(next http.Handler) http.Handler {
	revoked := logging.Middleware(s.log)(s.revoked.Middleware(next))
	plain := logging.Middleware(s.log)(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			plain.ServeHTTP(w, r)
			return
		}
		revoked.ServeHTTP(w, r)
	})
}
