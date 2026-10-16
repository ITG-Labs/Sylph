package api

import (
	"crypto/tls"
	"log/slog"
	"net/http"

	"github.com/ITG-Labs/sylph/internal/version"
	"github.com/ITG-Labs/sylph/pkg/mtls"
	"github.com/ITG-Labs/sylph/services/core/internal/service"
)

type Server struct {
	svc     *service.Service
	log     *slog.Logger
	revoked *mtls.RevocationList
}

func NewServer(svc *service.Service, log *slog.Logger, revoked *mtls.RevocationList) *Server {
	return &Server{svc: svc, log: log, revoked: revoked}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": version.Version})
	})
	s.registerSyncRoutes(mux)
	s.registerServiceRoutes(mux)
	s.registerAdminRoutes(mux)
	s.registerPrivateRoutes(mux)
	s.registerUserAdminRoutes(mux)
	s.registerPaymentAdminRoutes(mux)
	return s.middleware(mux)
}

func (s *Server) ListenAndServe(addr string, tlsConfig *tls.Config) error {
	srv := &http.Server{Addr: addr, Handler: s.Handler(), TLSConfig: tlsConfig}
	return srv.ListenAndServeTLS("", "")
}
