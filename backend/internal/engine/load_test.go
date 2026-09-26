//go:build load

// Teste de Sobrecarga (RNF-002/RNF-011, checklist final) — rode com:
//
//	make load-test        (≡ go test -tags load ./internal/engine -run Load -v)
//
// Valida que o pipeline scheduler→queue→worker pool:
//  1. sustenta N endpoints vencidos no mesmo tick (burst) sem queda de jobs
//     e sem execução sobreposta (in-flight ≤ pool);
//  2. sob backpressure (fila minúscula), registra queue_starved e reprocessa
//     nos próximos ticks até concluir tudo — sem deadlock (arquitetura §3.2).
package engine

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"monitor/internal/config"
	"monitor/internal/domain"
	"monitor/internal/storage"
)

func loadConfig(pool, queue int) config.Config {
	return config.Config{
		AllowPrivateTargets: true, // httptest em loopback
		WorkerPoolSize:      pool,
		PoolQueueSize:       queue,
		BatchSize:           100,
		BatchFlush:          50 * time.Millisecond,
		SchedulerTick:       25 * time.Millisecond,
		JitterFraction:      0.0,
		FailThreshold:       3,
		SuccessThreshold:    2,
	}
}

// seedDueEndpoints cria N endpoints todos vencidos agora (intervalo 1 min —
// não voltam a vencer dentro da janela do teste) e sincroniza no engine.
func seedDueEndpoints(t *testing.T, store storage.Store, eng *Engine, n int, url string) {
	t.Helper()
	for i := 0; i < n; i++ {
		id, err := store.CreateEndpoint(context.Background(), domain.Endpoint{
			Name:             fmt.Sprintf("ep-%05d", i),
			URL:              url,
			Method:           "GET",
			Headers:          map[string]string{},
			Interval:         60 * time.Second,
			Timeout:          2 * time.Second,
			LatencyThreshold: 0,
			ExpectStatus:     200,
			Active:           true,
		})
		if err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
		ep := domain.Endpoint{ID: id, Name: fmt.Sprintf("ep-%05d", i), URL: url, Method: "GET", Active: true}
		ep.Interval, ep.Timeout = 60*time.Second, 2*time.Second
		if err := eng.SyncEndpoint(context.Background(), ep); err != nil {
			t.Fatalf("sync %d: %v", i, err)
		}
	}
}

// peakMonitor amostra o in-flight durante o burst e guarda o pico.
type peakMonitor struct {
	peak atomic.Int64
	done chan struct{}
}

func startPeakMonitor(eng *Engine) *peakMonitor {
	pm := &peakMonitor{done: make(chan struct{})}
	go func() {
		for {
			select {
			case <-pm.done:
				return
			default:
			}
			cur := eng.pool.Metrics().InFlight.Load()
			for {
				p := pm.peak.Load()
				if cur <= p || pm.peak.CompareAndSwap(p, cur) {
					break
				}
			}
			time.Sleep(2 * time.Millisecond)
		}
	}()
	return pm
}

func (pm *peakMonitor) stop() { close(pm.done) }

// TestLoadSustentado: N endpoints vencidos no mesmo tick → todos processados
// EXATAMENTE uma vez, zero drops e in-flight nunca excede o pool. Este teste
// pegou um double-run real (endpoint re-enfileirado por snapshot obsoleto —
// fix no scheduler.round: revalida due no tempo atual) e o trava aqui.
func TestLoadSustentado(t *testing.T) {
	const N, poolSize, queueSize = 800, 64, 2000

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	store := storage.NewMem()
	cfg := loadConfig(poolSize, queueSize)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	eng, err := New(context.Background(), cfg, store, logger)
	if err != nil {
		t.Fatal(err)
	}
	seedDueEndpoints(t, store, eng, N, srv.URL+"/ok")

	ctx, cancel := context.WithCancel(context.Background())
	peak := startPeakMonitor(eng)
	go eng.Run(ctx)

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) && eng.pool.Metrics().Processed.Load() < N {
		time.Sleep(50 * time.Millisecond)
	}
	cancel()
	peak.stop()
	eng.pool.Close() // drena (idempotente no Run, mas seguro no teste)

	got := eng.pool.Metrics()
	if got.Processed.Load() != N {
		t.Fatalf("processados=%d, esperado %d (restaram %d em fila/drop)", got.Processed.Load(), N, got.QueueDepth.Load())
	}
	if g := got.Dropped.Load(); g != 0 {
		t.Fatalf("drops=%d — o buffer %d deveria absorver o burst", g, queueSize)
	}
	if p := peak.peak.Load(); p > int64(poolSize) {
		t.Fatalf("in-flight pico %d excedeu o pool de %d (sobreposição!)", p, poolSize)
	}
	if g := got.InFlight.Load(); g != 0 {
		t.Fatalf("in-flight residual %d após dreno", g)
	}
	// Eventos publicados (rollups + possíveis transições) — RNF-011 ≥ 100/s.
	if ev := eng.obs.EventsTotal(); ev == 0 {
		t.Fatal("nenhum evento publicado no broker durante o burst")
	}
	// Toda checagem persistiu (flush em lote — RF-012) e nenhum endpoint foi
	// processado mais de uma vez (RF-011 — sem double-run).
	checks, err := store.ListChecks(context.Background(), storage.CheckFilter{Limit: 100000})
	if err != nil {
		t.Fatal(err)
	}
	if len(checks) != N {
		t.Fatalf("checks persistidos=%d, esperado %d", len(checks), N)
	}
	seen := map[int64]int{}
	for _, c := range checks {
		seen[c.EndpointID]++
		if seen[c.EndpointID] > 1 {
			t.Fatalf("endpoint %d processado %d vezes (double-run!)", c.EndpointID, seen[c.EndpointID])
		}
	}
	if len(seen) != N {
		t.Fatalf("endpoints distintos com check=%d, esperado %d", len(seen), N)
	}
	t.Logf("OK: %d endpoints em burst, %d eventos broker, 0 drops, 0 double-runs, pico in-flight %d/%d",
		N, eng.obs.EventsTotal(), peak.peak.Load(), poolSize)
}

// TestLoadBackpressure: fila minúscula → queue_starved>0, mas o scheduler
// re-processa nos próximos ticks e NENHUM endpoint fica de fora (não stacka).
func TestLoadBackpressure(t *testing.T) {
	const N, poolSize, queueSize = 300, 8, 8

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	store := storage.NewMem()
	cfg := loadConfig(poolSize, queueSize)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	eng, err := New(context.Background(), cfg, store, logger)
	if err != nil {
		t.Fatal(err)
	}
	seedDueEndpoints(t, store, eng, N, srv.URL+"/ok")

	ctx, cancel := context.WithCancel(context.Background())
	go eng.Run(ctx)

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) && eng.pool.Metrics().Processed.Load() < N {
		time.Sleep(50 * time.Millisecond)
	}
	cancel()
	eng.pool.Close()

	got := eng.pool.Metrics()
	if got.Processed.Load() != N {
		t.Fatalf("processados=%d, esperado %d (deadlock? fila %d)", got.Processed.Load(), N, got.QueueDepth.Load())
	}
	if g := got.Dropped.Load(); g == 0 {
		t.Fatal("esperava queue_starved>0 com fila de 8 e 300 vencidos")
	}
	// Mesmo sob backpressure, nenhum endpoint pode rodar 2× (fix do snapshot).
	checks, err := store.ListChecks(context.Background(), storage.CheckFilter{Limit: 100000})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int64]int{}
	for _, c := range checks {
		seen[c.EndpointID]++
		if seen[c.EndpointID] > 1 {
			t.Fatalf("backpressure: endpoint %d rodou %d vezes", c.EndpointID, seen[c.EndpointID])
		}
	}
	t.Logf("OK: backpressure com %d starved/drops → todos %d processados nos rounds seguintes",
		got.Dropped.Load(), len(seen))
}
