package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

const operatorHeader = "X-Sylph-Operator"

const maxOperatorRunes = 64

type ctxKey int

const (
	auditKey ctxKey = iota
	operatorKey
)

var redactedKeys = []string{"private_key", "sub_token", "hwid", "uuid", "password", "raw"}

func redactedKey(k string) bool {
	for _, r := range redactedKeys {
		if strings.EqualFold(k, r) {
			return true
		}
	}
	return false
}

type auditNote struct {
	entity, action, id string
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

type cappedBuffer struct {
	buf bytes.Buffer
	max int
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if room := c.max - c.buf.Len(); room > 0 {
		c.buf.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

func operatorFrom(r *http.Request) (string, bool) {
	if len(r.Header.Values(operatorHeader)) > 1 {
		return "", false
	}
	op := r.Header.Get(operatorHeader)
	if utf8.RuneCountInString(op) > maxOperatorRunes || !utf8.ValidString(op) {
		return "", false
	}
	for _, c := range op {
		if unicode.IsControl(c) || c == '\u2028' || c == '\u2029' {
			return "", false
		}
	}
	return op, true
}

func operatorOf(r *http.Request) string {
	op, _ := r.Context().Value(operatorKey).(string)
	return op
}

func (s *Server) logChange(r *http.Request, entity, action string, id any) {
	ids := fmt.Sprint(id)
	if note, ok := r.Context().Value(auditKey).(*auditNote); ok {
		note.entity, note.action, note.id = entity, action, ids
	}
	s.log.Info("admin_change", "entity", entity, "action", action, "id", ids, "peer", clientCN(r), "operator", operatorOf(r))
}

func (s *Server) servePanel(w http.ResponseWriter, r *http.Request, next http.Handler) {
	op, ok := operatorFrom(r)
	if !ok {
		writeFieldError(w, http.StatusBadRequest, "bad_request", operatorHeader, "must be sent once, at most 64 characters, without control characters")
		return
	}
	ctx := context.WithValue(r.Context(), operatorKey, op)
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		next.ServeHTTP(w, r.WithContext(ctx))
		return
	}
	note := &auditNote{}
	captured := &cappedBuffer{max: maxBodyBytes}
	if r.Body != nil {
		r.Body = struct {
			io.Reader
			io.Closer
		}{io.TeeReader(r.Body, captured), r.Body}
	}
	sw := &statusWriter{ResponseWriter: w}
	req := r.WithContext(context.WithValue(ctx, auditKey, note))
	next.ServeHTTP(sw, req)
	if sw.status < 200 || sw.status > 299 {
		return
	}
	entity, action, id := note.entity, note.action, note.id
	if entity == "" {
		entity, action, id = routeParts(req)
	}
	entry := store.AuditEntry{
		PeerCN: clientCN(r), Operator: op, Method: r.Method, Route: r.Pattern, Path: r.URL.Path,
		Entity: entity, EntityID: id, Action: action, Details: redact(captured.buf.Bytes()),
	}
	if err := s.svc.RecordAudit(context.WithoutCancel(r.Context()), entry); err != nil {
		s.log.Error("admin_audit_failed", "route", r.Pattern, "entity", entity, "id", id, "error", err.Error())
	}
}

func routeParts(r *http.Request) (string, string, string) {
	_, path, _ := strings.Cut(r.Pattern, " ")
	var parts []string
	for _, p := range strings.Split(strings.Trim(path, "/"), "/") {
		if p != "v1" && p != "admin" {
			parts = append(parts, p)
		}
	}
	entity, action, id := "", strings.ToLower(r.Method), ""
	for i, p := range parts {
		switch {
		case strings.HasPrefix(p, "{"):
			if id == "" {
				id = r.PathValue(strings.Trim(p, "{}."))
			}
		case i == 0:
			entity = p
		default:
			action = p
		}
	}
	return entity, action, id
}

func redact(raw []byte) json.RawMessage {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	var v any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return nil
	}
	out, err := json.Marshal(redactValue(v))
	if err != nil {
		return nil
	}
	return out
}

func redactValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			if redactedKey(k) {
				x[k] = "***"
			} else {
				x[k] = redactValue(val)
			}
		}
	case []any:
		for i := range x {
			x[i] = redactValue(x[i])
		}
	}
	return v
}

func (s *Server) handleAuditList(w http.ResponseWriter, r *http.Request) {
	q, ok := readQuery(w, r, "entity", "entity_id", "before", "limit")
	if !ok {
		return
	}
	f := store.AuditFilter{Entity: q.str("entity"), EntityID: q.str("entity_id"), Before: q.int("before", 0, 1<<62, 0), Limit: int(q.int("limit", 1, 100, 50))}
	if !q.ok() {
		return
	}
	rows, err := s.svc.ListAudit(r.Context(), f)
	if err != nil {
		s.writeServiceError(w, err, "audit")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": rows, "next_before": nextBefore(rows, f.Limit, func(e store.AuditEntry) int64 { return e.ID })})
}
