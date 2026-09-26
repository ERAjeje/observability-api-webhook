// Package api expõe o servidor HTTP (arquitetura §1.2). Na Fase 1 fornece
// /healthz e /readyz (T1.4); as rotas REST/SSE chegam na Fase 3.
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"monitor/internal/storage"
)

// Server é o servidor HTTP da aplicação.
type Server struct {
	router *chi.Mux
	store  storage.Store
}

// New monta as rotas. ready é uma função que valida dependências (store.Ping).
func New(store storage.Store) *Server {
	s := &Server{router: chi.NewRouter(), store: store}
	s.router.Use(middleware.Recoverer)
	s.router.Use(middleware.Timeout(30 * time.Second))

	s.router.Get("/healthz", s.healthz)
	s.router.Get("/readyz", s.readyz)
	return s
}

// Handler expõe o http.Handler.
func (s *Server) Handler() http.Handler { return s.router }

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.store.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not ready"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
