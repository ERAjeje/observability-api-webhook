// Package worker implementa o pool de goroutines que executa os
// health-checks de forma concorrente e resiliente (arquitetura §3, T2.6).
package worker

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"monitor/internal/domain"
)

// Job é a unidade de trabalho enfileirada pelo scheduler.
type Job struct {
	Endpoint domain.Endpoint
	Due      time.Time
}

// Handler processa um job. Um erro de retorno indica falha de checagem
// (não do pool) e é contabilizado como jobFailed.
type Handler func(ctx context.Context, job Job) error

// Metrics expõe contadores atômicos para observabilidade do pool
// (arquitetura §3.2 — backpressure/starving).
type Metrics struct {
	Submitted  atomic.Int64
	Processed  atomic.Int64
	Failed     atomic.Int64
	Dropped    atomic.Int64
	Panicked   atomic.Int64
	InFlight   atomic.Int64
	QueueDepth atomic.Int64
	MaxQueue   atomic.Int64
}

// Pool executa Jobs com N goroutines consumindo um canal bufferizado.
//
// Resiliência:
//   - panic no handler é recuperado por job (processo nunca morre);
//   - TryEnqueue é não-bloqueante → backpressure controlado (buffer cheio
//     nunca trava o scheduler);
//   - Close() drena o buffer após parar novas submissões (graceful).
type Pool struct {
	queue   chan Job
	handler Handler
	num     int
	closed  atomic.Bool
	wg      sync.WaitGroup
	metrics Metrics
	log     *slog.Logger
}

// New cria um pool com num workers e buffer de tamanho queue.
func New(num, queue int, handler Handler, log *slog.Logger) *Pool {
	if num < 1 {
		num = 1
	}
	if queue < 1 {
		queue = 1
	}
	p := &Pool{
		queue:   make(chan Job, queue),
		handler: handler,
		num:     num,
		log:     log,
	}
	p.metrics.MaxQueue.Store(int64(queue))
	return p
}

// Start inicia os workers. ctx cancela os workers entre jobs.
func (p *Pool) Start(ctx context.Context) {
	for i := 0; i < p.num; i++ {
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			for job := range p.queue {
				p.metrics.QueueDepth.Add(-1)
				select {
				case <-ctx.Done():
					return
				default:
				}
				p.run(ctx, job)
			}
		}()
	}
}

// TryEnqueue adiciona um job sem bloquear. Retorna false se o buffer estiver
// cheio (contabiliza drop) ou o pool já fechado.
func (p *Pool) TryEnqueue(job Job) bool {
	if p.closed.Load() {
		p.metrics.Dropped.Add(1)
		return false
	}
	select {
	case p.queue <- job:
		p.metrics.Submitted.Add(1)
		p.metrics.QueueDepth.Add(1)
		return true
	default:
		p.metrics.Dropped.Add(1)
		return false
	}
}

// Metrics expõe os contadores do pool para o observatório (RF-012).
func (p *Pool) Metrics() *Metrics { return &p.metrics }

// InFlight retorna o número atual de jobs em execução.
func (p *Pool) InFlight() int64 { return p.metrics.InFlight.Load() }

// Close impede novas submissões e drena o buffer: workers terminam os jobs
// enfileirados. Bloqueia até o fim do drain.
func (p *Pool) Close() {
	if p.closed.Swap(true) {
		return
	}
	close(p.queue)
	p.wg.Wait()
}

func (p *Pool) run(ctx context.Context, job Job) {
	p.metrics.InFlight.Add(1)
	defer p.metrics.InFlight.Add(-1)

	defer func() {
		if r := recover(); r != nil {
			p.metrics.Panicked.Add(1)
			p.log.Error("worker: panic recuperado", "endpoint", job.Endpoint.ID, "panic", r)
		}
	}()

	if err := p.handler(ctx, job); err != nil {
		p.metrics.Failed.Add(1)
		p.log.Warn("worker: checagem falhou", "endpoint", job.Endpoint.ID, "err", err)
		return
	}
	p.metrics.Processed.Add(1)
}
