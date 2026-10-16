package logging

import (
	"log/slog"
	"net/http"
	"time"
)

type statusWriter struct {
	http.ResponseWriter
	status int
	err    error
}

func RecordError(w http.ResponseWriter, err error) {
	for {
		switch x := w.(type) {
		case *statusWriter:
			x.err = err
			return
		case interface{ Unwrap() http.ResponseWriter }:
			w = x.Unwrap()
		default:
			return
		}
	}
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func Middleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get(HeaderRequestID)
			if id == "" || len(id) > 64 {
				id = NewRequestID()
			}
			r = r.WithContext(WithRequestID(r.Context(), id))
			w.Header().Set(HeaderRequestID, id)
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			start := time.Now()
			defer func() {
				rec := recover()
				status := sw.status
				event := "http_request"
				if rec != nil {
					status = http.StatusInternalServerError
					event = "http_panic"
				}
				level := slog.LevelInfo
				attrs := []slog.Attr{
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
					slog.Int("status", status),
					slog.Duration("duration", time.Since(start)),
					slog.String("request_id", id),
					slog.String("remote", r.RemoteAddr),
					slog.String("peer", peerCN(r)),
				}
				if sw.err != nil {
					level = slog.LevelError
					attrs = append(attrs, slog.String("error", sw.err.Error()))
				}
				logger.LogAttrs(r.Context(), level, event, attrs...)
				if rec != nil {
					panic(rec)
				}
			}()
			next.ServeHTTP(sw, r)
		})
	}
}

func peerCN(r *http.Request) string {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return ""
	}
	return r.TLS.PeerCertificates[0].Subject.CommonName
}
