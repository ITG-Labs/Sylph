package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/ITG-Labs/sylph/services/core/internal/service"
	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

const (
	maxBodyBytes   = 64 << 10
	maxDeltasBytes = 16 << 20
)

func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	return decodeJSON(w, r, v, maxBodyBytes, true)
}

func decodeOptionalBody(w http.ResponseWriter, r *http.Request, v any) bool {
	return decodeJSONOr(w, r, v, maxBodyBytes, true, true)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, v any, limit int64, strict bool) bool {
	return decodeJSONOr(w, r, v, limit, strict, false)
}

func decodeJSONOr(w http.ResponseWriter, r *http.Request, v any, limit int64, strict, emptyOK bool) bool {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", fmt.Sprintf("request body is larger than %d KiB", limit>>10))
		return false
	}
	if err == nil && emptyOK && len(bytes.TrimSpace(raw)) == 0 {
		return true
	}
	if err == nil {
		err = rejectNUL(raw)
	}
	if err == nil {
		dec := json.NewDecoder(bytes.NewReader(raw))
		if strict {
			dec.DisallowUnknownFields()
		}
		err = dec.Decode(v)
		if err == nil && dec.More() {
			err = errors.New("unexpected data after the JSON object")
		}
	}
	if err == nil {
		return true
	}
	writeError(w, http.StatusBadRequest, "bad_request", err.Error())
	return false
}

func rejectNUL(raw []byte) error {
	if !bytes.Contains(raw, []byte(`\u0000`)) {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil
		}
		if s, ok := tok.(string); ok && strings.ContainsRune(s, 0) {
			return errors.New("NUL characters are not allowed")
		}
	}
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeFieldError(w, http.StatusBadRequest, "bad_request", "id", "must be a positive integer")
		return 0, false
	}
	return id, true
}

func writeBanned(w http.ResponseWriter) {
	writeError(w, http.StatusForbidden, "banned", "user is banned")
}

func writeFieldError(w http.ResponseWriter, status int, code, field, message string) {
	writeJSON(w, status, map[string]string{"code": code, "field": field, "message": message})
}

func (s *Server) writeServiceError(w http.ResponseWriter, err error, what string) {
	s.writeServiceErrorAs(w, err, what, "admin_failed")
}

func (s *Server) writeServiceErrorAs(w http.ResponseWriter, err error, what, event string) {
	var fe *service.FieldError
	var ce *service.ConflictError
	switch {
	case errors.As(err, &fe):
		writeFieldError(w, http.StatusBadRequest, "bad_request", fe.Field, fe.Message)
	case errors.As(err, &ce):
		writeFieldError(w, http.StatusConflict, "conflict", ce.Field, ce.Message)
	case errors.Is(err, store.ErrUserBanned):
		writeBanned(w)
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", what+" not found")
	default:
		s.log.Error(event, "what", what, "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal", what+" failed")
	}
}
