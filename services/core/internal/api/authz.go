package api

import (
	"crypto/x509"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/ITG-Labs/sylph/pkg/mtls"
)

func (s *Server) route(mux *http.ServeMux, pattern string, h http.HandlerFunc, roles ...string) {
	if len(roles) == 0 {
		panic("route " + pattern + " registered without roles")
	}
	for _, role := range roles {
		if !mtls.IsKnownRole(role) {
			panic("route " + pattern + " registered with unknown role " + role)
		}
	}
	mux.Handle(pattern, s.requireRole(roles, h))
}

func (s *Server) routeMachine(mux *http.ServeMux, pattern string, h http.HandlerFunc, roles ...string) {
	s.machine[pattern] = true
	s.route(mux, pattern, h, roles...)
}

func (s *Server) requireRole(roles []string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cert := peerCert(r)
		role, ok := mtls.RoleOf(cert)
		if !ok || !slices.Contains(roles, role) {
			s.log.LogAttrs(r.Context(), slog.LevelWarn, "authz_denied",
				slog.String("peer", clientCN(r)),
				slog.String("role", rawRole(cert)),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
			)
			writeError(w, http.StatusForbidden, "forbidden", "role not allowed")
			return
		}
		if role == mtls.RolePanel && !s.machine[r.Pattern] {
			s.servePanel(w, r, next)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func peerCert(r *http.Request) *x509.Certificate {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return nil
	}
	return r.TLS.PeerCertificates[0]
}

func rawRole(cert *x509.Certificate) string {
	if cert == nil {
		return ""
	}
	return strings.Join(cert.Subject.OrganizationalUnit, ",")
}
