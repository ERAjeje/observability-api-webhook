// Package api expõe o servidor HTTP (arquitetura §1.2/§4). Fases 1-2:
// /healthz e /readyz. Fase 3: auth, CRUD admin, stats, status público e SSE.
package api

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"monitor/internal/auth"
	"monitor/internal/broker"
	"monitor/internal/domain"
	"monitor/internal/metrics"
	"monitor/internal/settings"
	"monitor/internal/storage"
)

// EngineHooks são os callbacks do engine chamados pelo CRUD admin
// (cache invalidado na edição — T3.2).
type EngineHooks interface {
	SyncEndpoint(ctx context.Context, ep domain.Endpoint) error
	DropEndpoint(id int64)
}

// Config reúne as dependências do servidor HTTP.
type Config struct {
	Auth          *auth.Service
	Broker        *broker.Broker
	Settings      *settings.Service
	Obs           *metrics.Registry // observabilidade — rota /metrics (RF-012)
	HeadersSecret []byte            // S-05 — cifrar headers em repouso (AES-GCM)
	SSEHeartbeat  time.Duration
	AllowPrivate  bool // libera faixas internas no teste de conectividade
	AuthRateMax   int
	AuthRateWin   time.Duration
}

// Server é o servidor HTTP da aplicação.
type Server struct {
	router       *chi.Mux
	store        storage.Store
	eng          EngineHooks
	auth         *auth.Service
	broker       *broker.Broker
	settings     *settings.Service
	obs          *metrics.Registry
	sealKey      []byte // S-05 — decifra headers no retorno ao admin
	sseHeartbeat time.Duration
	allowPrivate bool
	authLimiter  *RateLimiter
	sseSlots     chan struct{} // limite de streams SSE concorrentes
}

// maxSSEStreams limita conexões SSE simultâneas (mitigação de amplificação).
const maxSSEStreams = 256

// New monta as rotas da API (Fases 1-3).
func New(cfg Config, store storage.Store, eng EngineHooks) *Server {
	s := &Server{
		store:        store,
		eng:          eng,
		auth:         cfg.Auth,
		broker:       cfg.Broker,
		settings:     cfg.Settings,
		obs:          cfg.Obs,
		sealKey:      cfg.HeadersSecret,
		sseHeartbeat: cfg.SSEHeartbeat,
		allowPrivate: cfg.AllowPrivate,
		authLimiter:  NewRateLimiter(cfg.AuthRateMax, cfg.AuthRateWin),
	}
	if s.obs == nil {
		s.obs = metrics.New(nil, 0) // endpoint sempre disponível (testes sem runtime)
	}
	if s.sseHeartbeat <= 0 {
		s.sseHeartbeat = 15 * time.Second
	}
	s.sseSlots = make(chan struct{}, maxSSEStreams)

	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)

	// Health (Fase 1).
	r.Get("/healthz", s.healthz)
	r.Get("/readyz", s.readyz)

	// Métricas de observabilidade (RF-012) — formato Prometheus text.
	// Sem auth (scrape por Prometheus); em produção restrinja por rede (S-11).
	r.Get("/metrics", s.metricsHandler)

	// Auth (T3.1) — sem JWT.
	r.Route("/api/v1/auth", func(r chi.Router) {
		r.Use(middleware.Timeout(30 * time.Second))
		r.Post("/signup", s.signup)
		r.Post("/login", s.login)
	})

	// Público (status page — T3.4) + config de marca (RF-023).
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(middleware.Timeout(30 * time.Second))
		r.Get("/status", s.publicStatus)
		r.Get("/incidents", s.publicIncidents)
		r.Get("/config", s.publicConfig)
		// Stats públicos por endpoint (só rollups — RNF-014): gráficos da
		// status page (RF-020) sem exigir auth.
		r.Get("/status/{id}/stats/summary", s.publicStatsSummary)
		r.Get("/status/{id}/stats/series", s.publicStatsSeries)
	})

	// SSE (T3.5) — stream LONGO: SEM middleware.Timeout (RNF-009).
	r.Get("/api/v1/events", s.sseStream)

	// Admin (protegido por JWT — T3.1/T3.2/T3.3).
	r.Route("/api/v1/admin", func(r chi.Router) {
		r.Use(s.requireAuth)
		r.Use(middleware.Timeout(30 * time.Second))
		r.Route("/endpoints", func(r chi.Router) {
			r.Get("/", s.listEndpoints)
			r.Post("/", s.createEndpoint)
			r.Post("/test", s.testEndpoint)
			r.Get("/{id}", s.getEndpoint)
			r.Put("/{id}", s.updateEndpoint)
			r.Delete("/{id}", s.deleteEndpoint)
		})
		r.Route("/groups", func(r chi.Router) {
			r.Get("/", s.listGroups)
			r.Post("/", s.createGroup)
			r.Get("/{id}", s.getGroup)
			r.Put("/{id}", s.updateGroup)
			r.Delete("/{id}", s.deleteGroup)
		})
		r.Get("/checks", s.listChecks)
		r.Get("/stats/series", s.statsSeries)
		r.Get("/stats/summary", s.statsSummary)
		r.Get("/notifications", s.listNotifications)
		r.Get("/settings", s.getSettings)
		r.Put("/settings", s.putSettings)
	})

	s.router = r
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
