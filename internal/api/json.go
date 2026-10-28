package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/bobbyrc/foundry/internal/store"
)

// maxBodyBytes caps request bodies.
const maxBodyBytes = 1 << 20

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type errorBody struct {
	Error string `json:"error"`
}

func writeErrorMsg(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorBody{Error: msg})
}

// writeError maps err to a status: not found 404, invalid 400, conflict 409, anything else 500.
// The message of a 500 is hidden and the error is logged.
func writeError(w http.ResponseWriter, log *slog.Logger, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeErrorMsg(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrInvalid):
		writeErrorMsg(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrConflict):
		writeErrorMsg(w, http.StatusConflict, err.Error())
	default:
		log.Error("api: internal error", "err", err)
		writeErrorMsg(w, http.StatusInternalServerError, "internal error")
	}
}

// readJSON decodes a request body, rejecting unknown fields and bodies over maxBodyBytes. A
// failure wraps ErrInvalid. An empty body yields io.EOF (wrapped), which optional bodies accept.
func readJSON(w http.ResponseWriter, r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	return nil
}

// invalidf returns an ErrInvalid error with a formatted message.
func invalidf(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, a...))
}

// queryLimit parses ?limit=, returning def when absent.
func queryLimit(q url.Values, def int) (int, error) {
	s := q.Get("limit")
	if s == "" {
		return def, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, invalidf("limit must be a non-negative integer")
	}
	if n == 0 {
		return def, nil
	}
	return n, nil
}

// queryInt64 parses an int64 query parameter, returning 0 when absent.
func queryInt64(q url.Values, key string) (int64, error) {
	s := q.Get(key)
	if s == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		return 0, invalidf("%s must be a non-negative integer", key)
	}
	return n, nil
}

// csvQuery splits a comma-separated query parameter into typed values. Absent means nil.
func csvQuery[T ~string](q url.Values, key string) []T {
	var out []T
	for _, part := range strings.Split(q.Get(key), ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, T(p))
		}
	}
	return out
}

// nonNil keeps empty lists as [] in JSON rather than null.
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
