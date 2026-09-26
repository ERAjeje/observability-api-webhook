// Package scheduler produz os jobs de health-check a partir dos endpoints
// vencidos (arquitetura §3.1 / T2.3).
package scheduler

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"time"

	"monitor/internal/domain"
	"monitor/internal/storage"
	"monitor/internal/worker"
)

// Scheduler varre o store a cada tick, seleciona endpoints vencidos e os
// enfileira — aplicando jitter e garantindo que nunca haja execução
// sobreposta do mesmo endpoint (RF-011).
type Scheduler struct {
	store storage.Store
	// Submit entrega o job ao pool; deve ser não-bloqueante.
	Submit func(worker.Job) bool
	// IsInFlight/MarkInFlight/ReleaseInFlight controlam a guarda de
	// sobreposição por endpoint (RF-011).
	IsInFlight   func(endpointID int64) bool
	MarkInFlight func(endpointID int64)
	Release      func(endpointID int64)

	Tick   time.Duration
	Jitter float64 // fração (±) aplicada ao intervalo
	AtMost int     // limite de endpoints por round (bound de trabalho)

	log *slog.Logger
	now func() time.Time
}

// New cria o scheduler; o caller injeta os callbacks do engine.
func New(store storage.Store, log *slog.Logger) *Scheduler {
	return &Scheduler{
		store:  store,
		Tick:   10 * time.Second,
		Jitter: 0.2,
		AtMost: 500,
		log:    log,
		now:    time.Now,
	}
}

// Run executa o loop do agendador até o cancelamento do ctx.
func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(s.Tick)
	defer ticker.Stop()
	s.log.Info("scheduler: iniciado", "tick", s.Tick, "atMost", s.AtMost)
	for {
		select {
		case <-ctx.Done():
			s.log.Info("scheduler: encerrado")
			return
		case now := <-ticker.C:
			s.round(ctx, now)
		}
	}
}

// round processa os endpoints vencidos em um tick.
func (s *Scheduler) round(ctx context.Context, now time.Time) {
	due, err := s.store.ListDueEndpoints(ctx, now, s.AtMost)
	if err != nil {
		s.log.Error("scheduler: listar vencidos", "err", err)
		return
	}
	if len(due) == 0 {
		return
	}
	queued, skipped := 0, 0
	for _, ep := range due {
		if s.IsInFlight != nil && s.IsInFlight(ep.ID) {
			skipped++ // nunca enfileirar endpoint já em execução (RF-011)
			continue
		}
		if s.MarkInFlight != nil {
			s.MarkInFlight(ep.ID)
		}
		job := worker.Job{Endpoint: ep, Due: now}
		if s.Submit == nil || !s.Submit(job) {
			// Buffer cheio (backpressure) — libera e reprocessa no próximo tick.
			if s.Release != nil {
				s.Release(ep.ID)
			}
			skipped++
			continue
		}
		queued++
	}
	if s.log != nil && (queued > 0 || skipped > 0) {
		s.log.Debug("scheduler: round", "queued", queued, "skipped", skipped)
	}
}

// ApplyJitter retorna o delay do endpoint com jitter ±fraction.
func ApplyJitter(interval time.Duration, fraction float64) time.Duration {
	if fraction <= 0 {
		return interval
	}
	amp := time.Duration(float64(interval) * fraction)
	offset := time.Duration((rand.Float64()*2 - 1) * float64(amp))
	return interval + offset
}

var _ = domain.StatusUnknown // referência de pacote mantida por clareza