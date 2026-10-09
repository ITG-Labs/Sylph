package api

import (
	"net/http"

	"github.com/ITG-Labs/sylph/pkg/logging"
)

func (s *Server) middleware(next http.Handler) http.Handler {
	revoked := logging.Middleware(s.log)(s.revoked.Middleware(next))
	plain := logging.Middleware(s.log)(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.URL.Path) >= 4 && r.URL.Path[:4] == "/v1/" {
			revoked.ServeHTTP(w, r)
			return
		}
		plain.ServeHTTP(w, r)
	})
}

func (s *Server) registerSyncRoutes(mux *http.ServeMux) {
}

func (s *Server) registerServiceRoutes(mux *http.ServeMux) {
}
