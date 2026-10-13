package api

import (
	"net/http"
	"net/url"
	"strconv"
)

type queryReader struct {
	w      http.ResponseWriter
	values url.Values
	failed bool
}

func readQuery(w http.ResponseWriter, r *http.Request, allowed ...string) (*queryReader, bool) {
	values := r.URL.Query()
	for key, v := range values {
		known := false
		for _, a := range allowed {
			known = known || key == a
		}
		if !known {
			writeFieldError(w, http.StatusBadRequest, "bad_request", key, "unknown query parameter")
			return nil, false
		}
		if len(v) > 1 {
			writeFieldError(w, http.StatusBadRequest, "bad_request", key, "must be given once")
			return nil, false
		}
	}
	return &queryReader{w: w, values: values}, true
}

func (q *queryReader) str(name string) string {
	return q.values.Get(name)
}

func (q *queryReader) int(name string, min, max, def int64) int64 {
	raw := q.values.Get(name)
	if q.failed || raw == "" {
		return def
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < min || n > max {
		q.fail(name, "must be an integer "+strconv.FormatInt(min, 10)+"-"+strconv.FormatInt(max, 10))
		return def
	}
	return n
}

func (q *queryReader) optID(name string) *int64 {
	if q.values.Get(name) == "" {
		return nil
	}
	n := q.int(name, 1, 1<<62, 0)
	if q.failed {
		return nil
	}
	return &n
}

func (q *queryReader) bool(name string) bool {
	raw := q.values.Get(name)
	if q.failed || raw == "" {
		return false
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		q.fail(name, "must be true or false")
		return false
	}
	return v
}

func (q *queryReader) fail(name, message string) {
	if !q.failed {
		writeFieldError(q.w, http.StatusBadRequest, "bad_request", name, message)
	}
	q.failed = true
}

func (q *queryReader) ok() bool {
	return !q.failed
}

func nextBefore[T any](rows []T, limit int, id func(T) int64) *int64 {
	if len(rows) < limit || len(rows) == 0 {
		return nil
	}
	v := id(rows[len(rows)-1])
	return &v
}
