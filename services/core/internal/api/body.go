package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/ITG-Labs/sylph/services/core/internal/service"
	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

const maxBodyBytes = 64 << 10

func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	err := dec.Decode(v)
	if err == nil && dec.More() {
		err = errors.New("unexpected data after the JSON object")
	}
	if err == nil {
		return true
	}
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", "request body is larger than 64 KiB")
		return false
	}
	writeError(w, http.StatusBadRequest, "bad_request", err.Error())
	return false
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeFieldError(w, http.StatusBadRequest, "bad_request", "id", "must be a positive integer")
		return 0, false
	}
	return id, true
}

func writeFieldError(w http.ResponseWriter, status int, code, field, message string) {
	writeJSON(w, status, map[string]string{"code": code, "field": field, "message": message})
}

func (s *Server) writeServiceError(w http.ResponseWriter, err error, what string) {
	var fe *service.FieldError
	var ce *service.ConflictError
	switch {
	case errors.As(err, &fe):
		writeFieldError(w, http.StatusBadRequest, "bad_request", fe.Field, fe.Message)
	case errors.As(err, &ce):
		writeFieldError(w, http.StatusConflict, "conflict", ce.Field, ce.Message)
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", what+" not found")
	default:
		s.log.Error("admin_failed", "what", what, "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal", what+" failed")
	}
}
