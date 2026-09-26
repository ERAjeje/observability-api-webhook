// Package engine orquestra o Core Engine de monitoramento: scheduler +
// worker pool + checker + state machine + persistência em lote + broker
// (arquitetura §3, T2.3..T2.7).
package engine

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"

	"monitor/internal/broker"
	"monitor/internal/checker"
	"monitor/internal/config"
	"monitor/internal/domain"
	"monitor/internal/scheduler"
	"monitor/internal/storage"
	"monitor/internal/worker"
)

// Engine executa o ciclo completo de monitoramento.
type Engine struct {
	cfg     config.Config
	store   storage.Store
	check   *checker.Checker
	pool    *worker.Pool
	sched   *scheduler.Scheduler
	broker  *broker.Broker
	sm      *domain.StateMachine
	log     *slog.Logger
	metrics *worker.Metrics

	mu       sync.Mutex
	runtimes map[int64]*domain.EndpointRuntime

	batchMu  sync.Mutex
	pending  []domain.Check
	flushSig chan struct{}

	closed atomicBool
	wg     sync.WaitGroup
}

type atomicBool struct {
	mu sync.Mutex
	v  bool
}

func (a *atomicBool) Set(b bool) { a.mu.Lock(); a.v = b; a.mu.Unlock() }
func (a *atomicBool) Get() bool  { a.mu.Lock(); defer a.mu.Unlock(); return a.v }

// New constrói o engine, carregando o estado persistido dos endpoints.
func New(ctx context.Context, cfg config.Config, store storage.Store, log *slog.Logger) (*Engine, error) {
	check := checker.New(cfg.AllowPrivateTargets)
	brok := broker.New()
	sm := domain.NewStateMachine(cfg.FailThreshold, cfg.SuccessThreshold)

	e := &Engine{
		cfg:      cfg,
		store:    store,
		check:    check,
		broker:   brok,
		sm:       sm,
		log:      log,
		metrics:  &worker.Metrics{},
		runtimes: map[int64]*domain.EndpointRuntime{},
		flushSig: make(chan struct{}, 1),
	}

	eps, err := store.ListEndpoints(ctx)
	if err != nil {
		return nil, err
	}
	for _, ep := range eps {
		e.runtimes[ep.ID] = &domain.EndpointRuntime{Status: ep.Status}
	}

	// Pool de workers — handler é o ciclo por job.
	e.pool = worker.New(cfg.WorkerPoolSize, cfg.PoolQueueSize, e.handleJob, log)

	// Scheduler — callbacks apontam para o engine.
	e.sched = scheduler.New(store, log)
	e.sched.Tick = cfg.SchedulerTick
	e.sched.Jitter = cfg.JitterFraction
	e.sched.Submit = e.pool.TryEnqueue
	e.sched.IsInFlight = func(id int64) bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		rt, ok := e.runtimes[id]
		return ok && rt.InFlight
	}
	e.sched.MarkInFlight = func(id int64) {
		e.mu.Lock()
		defer e.mu.Unlock()
		if rt, ok := e.runtimes[id]; ok {
			rt.InFlight = true
		}
	}
	e.sched.Release = func(id int64) {
		e.mu.Lock()
		defer e.mu.Unlock()
		if rt, ok := e.runtimes[id]; ok {
			rt.InFlight = false
		}
	}
	return e, nil
}

// Metrics expõe as métricas do pool.
func (e *Engine) Metrics() *worker.Metrics { return e.metrics }

// Broker expõe o barramento (usado pelo SSE na Fase 3).
func (e *Engine) Broker() *broker.Broker { return e.broker }

// Run inicia pool, scheduler e o escritor de lotes. Bloqueia até ctx cancel.
func (e *Engine) Run(ctx context.Context) {
	e.closed.Set(false)
	e.wg.Add(1)
	go func() { defer e.wg.Done(); e.batchWriter(ctx) }()

	e.pool.Start(ctx)
	e.log.Info("engine: pool iniciado",
		"workers", e.cfg.WorkerPoolSize, "queue", e.cfg.PoolQueueSize,
		"failThreshold", e.cfg.FailThreshold, "successThreshold", e.cfg.SuccessThreshold)

	e.sched.Run(ctx) // bloqueia até shutdown

	// Shutdown: pool drena jobs enfileirados; em seguida flush final.
	e.pool.Close()
	e.flushPending(context.Background())
}

// Stop sinaliza shutdown (usado com cancel do ctx em main).
func (e *Engine) Stop() {

}

func (e *Engine) flushPending(ctx context.Context) {
	e.batchMu.Lock()
	pend := e.pending
	e.pending = nil
	e.batchMu.Unlock()
	if len(pend) == 0 {
		return
	}
	if err := e.store.AppendChecks(ctx, pend); err != nil {
		e.log.Error("engine: flush de checks falhou (retentado)", "n", len(pend), "err", err)
		e.batchMu.Lock()
		e.pending = append(pend, e.pending...) // devolve ao buffer para retry
		e.batchMu.Unlock()
		return
	}
	for _, r := range aggregateRollups(pend) {
		if err := e.store.UpsertRollup(ctx, r); err != nil {
			e.log.Error("engine: upsert rollup falhou", "err", err)
			return
		}
		e.broker.Publish(broker.EventCheckRecorded, broker.Event{
			Type: broker.EventCheckRecorded, Timestamp: time.Now(),
			EndpointID: r.EndpointID, Status: domain.StatusUnknown,
		})
	}
}

// batchWriter consolida checks e empurra ao store em lotes (T2.6).
func (e *Engine) batchWriter(ctx context.Context) {
	ticker := time.NewTicker(e.cfg.BatchFlush)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-e.flushSig:
			e.flushPending(ctx)
		case <-ticker.C:
			e.batchMu.Lock()
			n := len(e.pending)
			e.batchMu.Unlock()
			if n > 0 {
				e.flushPending(ctx)
			}
		}
	}
}

// appendCheck adiciona um check ao lote e sinaliza o flush quando atinge
// BatchSize (bound de memória) (T2.6).
func (e *Engine) appendCheck(c domain.Check) {
	e.batchMu.Lock()
	defer e.batchMu.Unlock()
	e.pending = append(e.pending, c)
	if len(e.pending) >= e.cfg.BatchSize {
		select {
		case e.flushSig <- struct{}{}:
		default:
		}
	}
}

// handleJob é o ciclo completo por checagem (arquitetura §1.3, T2.5..T2.7).
func (e *Engine) handleJob(parentCtx context.Context, job worker.Job) error {
	ep := job.Endpoint
	rt := e.runtime(ep.ID)
	// Contexto derivado SOMENTE para a requisição HTTP (timeout do endpoint).
	// Após o run, cancelamos e persistimos com o contexto pai (ainda vivo).
	ctx, cancel := context.WithTimeout(parentCtx, ep.Timeout)
	out := e.check.Run(ctx, ep)
	cancel()

	now := time.Now()
	e.mu.Lock()
	tr := e.sm.Apply(rt, out.Result, now)
	e.mu.Unlock()

	// 1. Log bruto (batch writer).
	e.appendCheck(domain.Check{
		EndpointID: ep.ID, CheckedAt: now, Result: out.Result,
		HTTPStatus: out.HTTPStatus, LatencyMS: out.LatencyMS, ErrorDetail: out.ErrorDetail,
	})

	// 2. Transições de estado → incidentes + persistência + eventos.
	if tr.StatusChanged || tr.OpenedIncident || tr.ClosedIncident {
		e.applyTransition(parentCtx, ep, rt, tr, now)
	}

	// 3. Reagenda com base na conclusão + jitter (sem deriva).
	delay, err := scheduler.ApplyJitter(ep.Interval, e.sched.Jitter)
	if err != nil {
		e.log.Warn("engine: jitter falhou — usando intervalo nominal", "endpoint", ep.ID, "err", err)
		delay = ep.Interval
	}
	next := now.Add(delay)
	if err := e.store.SetNextCheckAt(parentCtx, ep.ID, next); err != nil {
		e.log.Error("engine: reagendar falhou", "endpoint", ep.ID, "err", err)
	}
	e.release(ep.ID)
	return nil
}

func (e *Engine) runtime(id int64) *domain.EndpointRuntime {
	e.mu.Lock()
	defer e.mu.Unlock()
	rt, ok := e.runtimes[id]
	if !ok {
		rt = &domain.EndpointRuntime{Status: domain.StatusUnknown}
		e.runtimes[id] = rt
	}
	return rt
}

func (e *Engine) release(id int64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if rt, ok := e.runtimes[id]; ok {
		rt.InFlight = false
	}
}

// applyTransition materializa incidentes/status e publica eventos.
func (e *Engine) applyTransition(ctx context.Context, ep domain.Endpoint, rt *domain.EndpointRuntime, tr domain.Transition, at time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Persistência do status do endpoint.
	if tr.StatusChanged {
		if err := e.store.SetEndpointStatus(ctx, ep.ID, tr.Status); err != nil {
			e.log.Error("engine: persistir status falhou", "endpoint", ep.ID, "err", err)
		}
		e.broker.Publish(broker.EventStatusChanged, broker.Event{
			Type: broker.EventStatusChanged, Timestamp: at, EndpointID: ep.ID,
			Name: ep.Name, Status: tr.Status, From: rt.Status,
		})
		e.log.Info("engine: status alterado", "endpoint", ep.ID, "name", ep.Name,
			"from", rt.Status, "to", tr.Status)
		rt.Status = tr.Status
	}

	if tr.OpenedIncident {
		incID, err := e.store.CreateIncident(ctx, domain.Incident{
			EndpointID: ep.ID, StartedAt: rt.IncidentStarted, Resolution: "auto",
		})
		if err != nil {
			e.log.Error("engine: abrir incidente falhou", "endpoint", ep.ID, "err", err)
			return
		}
		rt.IncidentID = incID
		e.broker.Publish(broker.EventIncidentOpened, broker.Event{
			Type: broker.EventIncidentOpened, Timestamp: at, EndpointID: ep.ID,
			Name: ep.Name, IncidentID: incID, Status: domain.StatusDown,
		})
		e.log.Warn("engine: incidente aberto", "endpoint", ep.ID, "incident", incID,
			"started", rt.IncidentStarted)
	}

	if tr.ClosedIncident {
		incID := rt.IncidentID
		if err := e.store.CloseIncident(ctx, incID, at); err != nil {
			e.log.Error("engine: fechar incidente falhou", "endpoint", ep.ID, "err", err)
			return
		}
		rt.IncidentID = 0
		dur := at.Sub(rt.IncidentStarted).Milliseconds()
		e.broker.Publish(broker.EventIncidentClosed, broker.Event{
			Type: broker.EventIncidentClosed, Timestamp: at, EndpointID: ep.ID,
			Name: ep.Name, IncidentID: incID, DurationMS: dur, Status: domain.StatusUp,
		})
		e.log.Info("engine: incidente fechado", "endpoint", ep.ID, "incident", incID,
			"duration_ms", dur)
	}
}

// Pool expõe o pool (uso em testes/instrumentação).
func (e *Engine) Pool() *worker.Pool { return e.pool }

// RuntimeState retorna o estado atual de um endpoint (uso em testes).
func (e *Engine) RuntimeState(id int64) (domain.StatusClass, int64, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	rt, ok := e.runtimes[id]
	if !ok {
		return domain.StatusUnknown, 0, false
	}
	return rt.Status, rt.IncidentID, true
}

// aggregateRollups consolida checks em rollups por (endpoint, minuto)
// com P95 aproximado (max entre flushes) — RNF-014.
func aggregateRollups(checks []domain.Check) []domain.Rollup {
	type bucket struct {
		lats []int64
		ok   int64
	}
	agg := map[[2]int64]*bucket{} // [endpoint, bucketUnix]
	for _, c := range checks {
		key := [2]int64{c.EndpointID, c.CheckedAt.UTC().Truncate(time.Minute).Unix()}
		b, ok := agg[key]
		if !ok {
			b = &bucket{}
			agg[key] = b
		}
		b.lats = append(b.lats, c.LatencyMS)
		if c.Result == domain.ResultOK || c.Result == domain.ResultDegraded {
			b.ok++
		}
	}
	out := make([]domain.Rollup, 0, len(agg))
	for key, b := range agg {
		sort.Slice(b.lats, func(i, j int) bool { return b.lats[i] < b.lats[j] })
		var sum int64
		for _, l := range b.lats {
			sum += l
		}
		out = append(out, domain.Rollup{
			EndpointID:   key[0],
			Bucket:       time.Unix(key[1], 0).UTC(),
			Count:        int64(len(b.lats)),
			OKCount:      b.ok,
			SumLatencyMS: sum,
			P95LatencyMS: p95(b.lats),
		})
	}
	return out
}

// p95 retorna o percentil 95 de uma lista ordenada (ceil).
func p95(sorted []int64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted))*0.95+0.999) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}
