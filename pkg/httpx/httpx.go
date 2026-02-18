package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// ErrorResponse is a minimal, consistent JSON error envelope.
type ErrorResponse struct {
	Error string `json:"error"`
}

// WriteJSON writes v as JSON with the provided status code.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteError writes a JSON error envelope.
func WriteError(w http.ResponseWriter, status int, msg string) {
	WriteJSON(w, status, ErrorResponse{Error: msg})
}

// DecodeJSON decodes a JSON request body into dst.
// It rejects empty bodies and trailing data.
func DecodeJSON(r *http.Request, dst any) error {
	if r.Body == nil {
		return errors.New("empty body")
	}
	defer r.Body.Close()

	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		return err
	}
	// Ensure there is no trailing data.
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return errors.New("trailing data")
		}
		return err
	}
	return nil
}

