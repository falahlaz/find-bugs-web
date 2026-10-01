// Package httpx holds small JSON helpers shared by HTTP handlers.
package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
)

// MaxBody caps JSON request bodies.
const MaxBody = 64 << 10

// ErrorBody is the JSON shape of every error response.
type ErrorBody struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
}

// JSON writes v with status code.
func JSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	if v != nil {
		if err := json.NewEncoder(w).Encode(v); err != nil {
			slog.Debug("write json", "err", err)
		}
	}
}

// Error writes an ErrorBody.
func Error(w http.ResponseWriter, code int, errCode, msg string) {
	JSON(w, code, ErrorBody{Error: msg, Code: errCode})
}

// Internal logs err and writes a generic 500.
func Internal(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	Error(w, http.StatusInternalServerError, "internal", "Terjadi kesalahan di server.")
}

// Decode reads a JSON body into v, requiring application/json.
func Decode(w http.ResponseWriter, r *http.Request, v any) bool {
	mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mt != "application/json" {
		Error(w, http.StatusUnsupportedMediaType, "bad_content_type", "Content-Type harus application/json.")
		return false
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil && !errors.Is(err, io.EOF) {
		Error(w, http.StatusBadRequest, "bad_json", "Body JSON tidak valid: "+err.Error())
		return false
	}
	return true
}
