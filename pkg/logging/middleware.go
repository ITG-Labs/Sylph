package logging

import (
	"log/slog"
	"net/http"
	"time"
)

type statusWriter struct {
	http.ResponseWriter
	status int
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
				logger.LogAttrs(r.Context(), slog.LevelInfo, event,
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
					slog.Int("status", status),
					slog.Duration("duration", time.Since(start)),
					slog.String("request_id", id),
					slog.String("remote", r.RemoteAddr),
				)
				if rec != nil {
					panic(rec)
				}
			}()
			next.ServeHTTP(sw, r)
		})
	}
}
