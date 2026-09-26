package metrics

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"monitor/internal/domain"
	"monitor/internal/worker"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRenderContemNomesEValores(t *testing.T) {
	r := New(nil, 20)
	r.ObserveCheck(domain.ResultOK, 10)
	r.ObserveCheck(domain.ResultOK, 60)
	r.ObserveCheck(domain.ResultFail, 4000)
	r.ObserveTransition()
	r.CountEvent()
	r.IncidentOpened()
	r.SetStatusSource(func() StatusSnapshot { return StatusSnapshot{Active: 3, Up: 2, Down: 1} })

	out := string(r.Render())
	for _, n := range MetricNames() {
		if !strings.Contains(out, n) {
			t.Fatalf("render não contém %q", n)
		}
	}
	if !strings.Contains(out, "# TYPE monitor_checks_total counter") ||
		!strings.Contains(out, "monitor_worker_pool_size 20") ||
		!strings.Contains(out, "monitor_endpoints_status{state=\"up\"} 2") {
		t.Fatalf("valores esperados ausentes:\n%s", out)
	}
	if !strings.Contains(out, "monitor_incidents_open 1") {
		t.Fatalf("gauge incidentes aberto: %s", out)
	}
}

func TestHistogramaCumulativo(t *testing.T) {
	r := New(nil, 4)
	r.ObserveCheck(domain.ResultOK, 10)     // ≤ 10, ≤ 25, ... ≤ 30000
	r.ObserveCheck(domain.ResultOK, 60)     // > 10, >25? 60 > 25 → entra em le=50, 100, ...
	r.ObserveCheck(domain.ResultFail, 4000) // > 50,100,250,500,1000,2500,5000? 4000 ≤ 5000

	out := string(r.Render())
	checkBucket := func(le, want string) {
		line := "monitor_check_duration_milliseconds_bucket{le=\"" + le + "\"} " + want
		if !strings.Contains(out, line) {
			t.Fatalf("bucket le=%s esperava %s; linha ausente:\n%s", le, want, out)
		}
	}
	checkBucket("10", "1")
	checkBucket("50", "1") // 10 e 60? 60>50 → só 10
	checkBucket("5000", "3")
	checkBucket("+Inf", "3")
	if !strings.Contains(out, "monitor_check_duration_milliseconds_sum 4070") ||
		!strings.Contains(out, "monitor_check_duration_milliseconds_count 3") {
		t.Fatalf("sum/count errados:\n%s", out)
	}
}

func TestPoolMetricsRefletemNoRender(t *testing.T) {
	log := newTestLogger()
	p := worker.New(2, 8, func(ctx context.Context, job worker.Job) error { return nil }, log)
	r := New(p.Metrics(), 2)
	p.Start(context.Background())
	if !p.TryEnqueue(worker.Job{Endpoint: domain.Endpoint{ID: 1}, Due: time.Now()}) {
		t.Fatal("enqueue falhou")
	}
	if !p.TryEnqueue(worker.Job{Endpoint: domain.Endpoint{ID: 2}, Due: time.Now()}) {
		t.Fatal("enqueue falhou")
	}
	// aguarda processamento
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && p.Metrics().Processed.Load() < 2 {
		time.Sleep(10 * time.Millisecond)
	}
	p.Close()

	out := string(r.Render())
	if !strings.Contains(out, "monitor_jobs_processed_total 2") {
		t.Fatalf("processed esperava 2:\n%s", out)
	}
	if !strings.Contains(out, "monitor_queue_depth 0") {
		t.Fatalf("fila deveria estar vazia:\n%s", out)
	}
	// Starved: pool fechado → enqueue conta drop (backpressure documentada).
	if !p.TryEnqueue(worker.Job{Endpoint: domain.Endpoint{ID: 3}, Due: time.Now()}) {
		t.Log("enqueue pós-Close rejeitado (esperado)")
	}
}

func TestIncidentGaugeNaoVaiAbaixoDeZero(t *testing.T) {
	r := New(nil, 4)
	r.IncidentClosed() // sem abertos → não negativa
	if got := r.incidentsOpen.Load(); got != 0 {
		t.Fatalf("gauge negativo: %d", got)
	}
	r.IncidentOpened()
	r.IncidentOpened()
	r.IncidentClosed()
	if got := r.incidentsOpen.Load(); got != 1 {
		t.Fatalf("esperava 1, got %d", got)
	}
}
