package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"monitor/internal/storage"
)

// writeJSON serializa v como JSON com código HTTP.
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// apiError é o corpo de erro padronizado.
type apiError struct {
	Error string `json:"error"`
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, apiError{Error: msg})
}

// mapStoreErr traduz erros de storage para respostas HTTP.
func mapStoreErr(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, storage.ErrNotFound):
		writeErr(w, http.StatusNotFound, "recurso não encontrado")
	case errors.Is(err, storage.ErrConflict):
		writeErr(w, http.StatusConflict, "recurso já existe")
	case err != nil:
		writeErr(w, http.StatusInternalServerError, "erro interno")
	default:
		return false
	}
	return true
}

// decodeJSON lê o corpo como JSON limitando o tamanho (1 MiB).
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "JSON inválido: "+err.Error())
		return false
	}
	return true
}
