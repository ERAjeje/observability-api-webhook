// Package metrics implementa a observabilidade do runtime (RF-012, RNF-011):
// um registro leve que agrega os contadores do worker pool (arquitetura §3.2)
// e do engine e os expõe no formato Prometheus text (text/plain; version=0.0.4)
// — sem dependências externas, direto no binário estático.
package metrics

import (
	"bytes"
	"runtime"
	"sort"
	"strconv"
	"sync/atomic"
	"time"

	"monitor/internal/domain"
	"monitor/internal/worker"
)

// StatusSnapshot é a foto de estado dos endpoints em um instante, fornecida
// pelo engine no render (monitor_endpoints_status).
type StatusSnapshot struct {
	Active   int
	Up       int
	Down     int
	Degraded int
	Unknown  int
}

// histogramLe são os limites (ms) dos buckets cumulativos de latência —
// coincidem com o faturamento típico de health-checks (P50/P95 relevante).
var histogramLe = []int64{5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000, 30000}

// Registry agrega métricas do pool e do engine.
type Registry struct {
	pool *worker.Metrics
	size int // worker pool size (fixo na construção)

	checksTotal atomic.Int64
	checksOK    atomic.Int64
	checksDeg   atomic.Int64
	checksFail  atomic.Int64

	latSamples atomic.Int64
	latSumMS   atomic.Int64
	latBuckets []atomic.Int64 // cumulativos por le (histogramLe)

	transitionsTotal atomic.Int64
	eventsTotal      atomic.Int64

	incidentsOpen  atomic.Int64
	incidentsTotal atomic.Int64

	statusSrc func() StatusSnapshot

	started time.Time
}

// New cria o registro. pool é o *worker.Metrics do engine (pode ser nil em
// testes do pacote api/sem runtime — linhas zeradas).
func New(pool *worker.Metrics, poolSize int) *Registry {
	return &Registry{
		pool:       pool,
		size:       poolSize,
		latBuckets: make([]atomic.Int64, len(histogramLe)),
		started:    time.Now(),
	}
}

// SetStatusSource registra a função que fotografa os endpoints (engine).
func (r *Registry) SetStatusSource(fn func() StatusSnapshot) { r.statusSrc = fn }

// ObserveCheck contabiliza um check concluído por resultado e alimenta o
// histograma de latência (RF-012).
func (r *Registry) ObserveCheck(result domain.ResultClass, latencyMS int64) {
	r.checksTotal.Add(1)
	switch result {
	case domain.ResultOK:
		r.checksOK.Add(1)
	case domain.ResultDegraded:
		r.checksDeg.Add(1)
	default:
		r.checksFail.Add(1)
	}
	if latencyMS >= 0 {
		r.latSamples.Add(1)
		r.latSumMS.Add(latencyMS)
		for i, le := range histogramLe {
			if latencyMS <= le {
				r.latBuckets[i].Add(1)
			}
		}
	}
}

// ObserveTransition contabiliza transições de estado publicadas (RF-025).
func (r *Registry) ObserveTransition() { r.transitionsTotal.Add(1) }

// CountEvent contabiliza um evento publicado no broker (RNF-011).
func (r *Registry) CountEvent() { r.eventsTotal.Add(1) }

// IncidentOpened/IncidentClosed mantêm o gauge de incidentes em aberto.
func (r *Registry) IncidentOpened() {
	r.incidentsTotal.Add(1)
	r.incidentsOpen.Add(1)
}
func (r *Registry) IncidentClosed() {
	if r.incidentsOpen.Load() > 0 {
		r.incidentsOpen.Add(-1)
	}
}

// EventsTotal devolve os eventos publicados no broker (RNF-011).
func (r *Registry) EventsTotal() int64 { return r.eventsTotal.Load() }

// Render gera a exposição Prometheus text (sem dependência de lib externa).
func (r *Registry) Render() []byte {
	var b bytes.Buffer
	w := func(s string) {
		// conteúdo estático — sem FastHTTP/HTML issues
		b.WriteString(s)
	}
	line := func(name, typ, help, value string) {
		w("# HELP " + name + " " + help + "\n")
		w("# TYPE " + name + " " + typ + "\n")
		w(name + " " + value + "\n")
	}
	hist := func(name, help string) {
		// histograma cumulativo: _bucket{le=}, _sum, _count
		w("# HELP " + name + " " + help + "\n")
		w("# TYPE " + name + " histogram\n")
		var cum int64
		for i, le := range histogramLe {
			// só emite bucket se tiver amostras acumuladas ou se é o último
			cum = r.latBuckets[i].Load()
			w(name + "_bucket{le=\"" + strconv.FormatInt(le, 10) + "\"} " + strconv.FormatInt(cum, 10) + "\n")
		}
		w(name + "_bucket{le=\"+Inf\"} " + strconv.FormatInt(r.latSamples.Load(), 10) + "\n")
		w(name + "_sum " + strconv.FormatInt(r.latSumMS.Load(), 10) + "\n")
		w(name + "_count " + strconv.FormatInt(r.latSamples.Load(), 10) + "\n")
	}

	// Worker pool (arquitetura §3.2).
	var dp, proc, fail, panicN, submit, inflight, depth, maxq int64
	if r.pool != nil {
		dp = r.pool.Dropped.Load()
		proc = r.pool.Processed.Load()
		fail = r.pool.Failed.Load()
		panicN = r.pool.Panicked.Load()
		submit = r.pool.Submitted.Load()
		inflight = r.pool.InFlight.Load()
		depth = r.pool.QueueDepth.Load()
		maxq = r.pool.MaxQueue.Load()
	}
	line("monitor_worker_pool_size", "gauge", "Número de workers do pool.", strconv.Itoa(r.size))
	line("monitor_queue_capacity", "gauge", "Capacidade do buffer da fila de jobs.", strconv.FormatInt(maxq, 10))
	line("monitor_queue_depth", "gauge", "Jobs aguardando no buffer da fila.", strconv.FormatInt(depth, 10))
	line("monitor_jobs_inflight", "gauge", "Jobs em execução agora.", strconv.FormatInt(inflight, 10))
	line("monitor_jobs_submitted_total", "counter", "Jobs submetidos ao pool.", strconv.FormatInt(submit, 10))
	line("monitor_jobs_processed_total", "counter", "Jobs concluídos com sucesso.", strconv.FormatInt(proc, 10))
	line("monitor_jobs_failed_total", "counter", "Jobs cuja checagem falhou (contagem do pool).", strconv.FormatInt(fail, 10))
	line("monitor_jobs_panicked_total", "counter", "Panics recuperados no handler.", strconv.FormatInt(panicN, 10))
	line("monitor_queue_starved_total", "counter",
		"Backpressure no enfileiramento (buffer cheio/fechado) — reprocessado no próximo tick.", strconv.FormatInt(dp, 10))

	// Engine.
	line("monitor_checks_total", "counter", "Checagens concluídas (todas).", strconv.FormatInt(r.checksTotal.Load(), 10))
	line("monitor_checks_ok_total", "counter", "Checagens com resultado ok.", strconv.FormatInt(r.checksOK.Load(), 10))
	line("monitor_checks_degraded_total", "counter", "Checagens com resultado degraded.", strconv.FormatInt(r.checksDeg.Load(), 10))
	line("monitor_checks_fail_total", "counter", "Checagens com resultado fail.", strconv.FormatInt(r.checksFail.Load(), 10))
	hist("monitor_check_duration_milliseconds", "Latência das checagens em ms (histograma cumulativo).")
	line("monitor_transitions_total", "counter", "Transições de estado confirmadas.", strconv.FormatInt(r.transitionsTotal.Load(), 10))
	line("monitor_events_published_total", "counter", "Eventos publicados no broker (SSE).", strconv.FormatInt(r.eventsTotal.Load(), 10))
	line("monitor_incidents_total", "counter", "Incidentes abertos (histórico acumulado).", strconv.FormatInt(r.incidentsTotal.Load(), 10))
	line("monitor_incidents_open", "gauge", "Incidentes atualmente em aberto.", strconv.FormatInt(r.incidentsOpen.Load(), 10))

	// Endpoints (foto do engine).
	if r.statusSrc != nil {
		s := r.statusSrc()
		line("monitor_endpoints_active", "gauge", "Endpoints com runtime no engine.", strconv.Itoa(s.Active))
		line("monitor_endpoints_status{state=\"up\"}", "gauge", "Endpoints UP.", strconv.Itoa(s.Up))
		line("monitor_endpoints_status{state=\"down\"}", "gauge", "Endpoints DOWN.", strconv.Itoa(s.Down))
		line("monitor_endpoints_status{state=\"degraded\"}", "gauge", "Endpoints DEGRADED.", strconv.Itoa(s.Degraded))
		line("monitor_endpoints_status{state=\"unknown\"}", "gauge", "Endpoints UNKNOWN.", strconv.Itoa(s.Unknown))
	}

	// Runtime Go + upness.
	line("monitor_uptime_seconds", "gauge", "Tempo desde o início do processo.", strconv.FormatInt(int64(time.Since(r.started).Seconds()), 10))
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	line("monitor_go_goroutines", "gauge", "Goroutines ativas.", strconv.Itoa(runtime.NumGoroutine()))
	line("monitor_go_alloc_bytes", "gauge", "Bytes alocados no heap.", strconv.FormatInt(int64(ms.Alloc), 10))

	return b.Bytes()
}

// MetricNames devolve os nomes expostos (teste/docs).
func MetricNames() []string {
	out := []string{
		"monitor_worker_pool_size", "monitor_queue_capacity", "monitor_queue_depth",
		"monitor_jobs_inflight", "monitor_jobs_submitted_total", "monitor_jobs_processed_total",
		"monitor_jobs_failed_total", "monitor_jobs_panicked_total", "monitor_queue_starved_total",
		"monitor_checks_total", "monitor_checks_ok_total", "monitor_checks_degraded_total",
		"monitor_checks_fail_total", "monitor_check_duration_milliseconds", "monitor_transitions_total",
		"monitor_events_published_total", "monitor_incidents_total", "monitor_incidents_open",
		"monitor_endpoints_active", "monitor_endpoints_status", "monitor_uptime_seconds",
		"monitor_go_goroutines", "monitor_go_alloc_bytes",
	}
	sort.Strings(out)
	return out
}
