package engine

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"monitor/internal/broker"
	"monitor/internal/config"
	"monitor/internal/domain"
	"monitor/internal/storage"
)

func testLog() *slog.Logger { return slog.New(slog.DiscardHandler) }

// flippableServer alterna o status de resposta sob demanda (simula outage e
// recuperação).
type flippableServer struct {
	status int
	body   string
}

func (f *flippableServer) set(code int, body string) { f.status, f.body = code, body }

func (f *flippableServer) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(f.status)
	_, _ = w.Write([]byte(f.body))
}

// testCfg devolve uma configuração com ticks/batches curtos e jitter 0
// (determinístico para testes). AllowPrivateTargets=true porque os testes
// usam httptest (loopback).
func testCfg() config.Config {
	return config.Config{
		WorkerPoolSize:      8,
		PoolQueueSize:       100,
		SchedulerTick:       100 * time.Millisecond,
		BatchFlush:          50 * time.Millisecond,
		BatchSize:           10,
		JitterFraction:      0,
		FailThreshold:       3,
		SuccessThreshold:    2,
		AllowPrivateTargets: true,
		SSEHeartbeat:        time.Second,
		HTTPAddr:            ":0",
	}
}

func mustCreateEndpoint(t *testing.T, s storage.Store, e domain.Endpoint) int64 {
	t.Helper()
	id, err := s.CreateEndpoint(context.Background(), e)
	if err != nil {
		t.Fatalf("create endpoint: %v", err)
	}
	return id
}

// TestEngine_CicloCompleto cobre o fluxo de ponta a ponta (UC-01/UC-03):
// estabilidade (UP), queda (DOWN + incidente), recuperação (UP + incidente
// fechado), logs crus, rollups e eventos no broker.
func TestEngine_CicloCompleto_DownRecuperacaoEventos(t *testing.T) {
	// Dois serviços: um estável (UP) e um que cai e se recupera.
	stable := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer stable.Close()

	flip := &flippableServer{status: http.StatusOK, body: "healthy"}
	outage := httptest.NewServer(flip)
	defer outage.Close()

	store := storage.NewMem()
	eng, err := New(context.Background(), testCfg(), store, testLog())
	if err != nil {
		t.Fatal(err)
	}
	// Assina transições ANTES do run para não perder eventos (fan-out não
	// faz replay).
	evCh, evUn := eng.Broker().Subscribe("status_changed")
	defer evUn()
	go eng.Run(context.Background())
	t.Cleanup(func() { eng.Stop(); eng.Pool().Close() })

	stableID := mustCreateEndpoint(t, store, domain.Endpoint{
		Name: "stable", URL: stable.URL, Method: "GET",
		Interval: 300 * time.Millisecond, Timeout: 2 * time.Second, Active: true,
		NextCheckAt: time.Now().Add(-time.Second),
	})
	outageID := mustCreateEndpoint(t, store, domain.Endpoint{
		Name: "outage", URL: outage.URL, Method: "GET",
		Interval: 300 * time.Millisecond, Timeout: 2 * time.Second, Active: true,
		NextCheckAt: time.Now().Add(-time.Second),
	})

	// 1. Endpoint estável promove a UP e gera checks/rollups.
	pollUntil(t, 5*time.Second, func() bool {
		st, _, ok := eng.RuntimeState(stableID)
		return ok && st == domain.StatusUp
	}, "stable UP")
	pollUntil(t, 5*time.Second, func() bool {
		for _, r := range store.RollupsExport() {
			if r.EndpointID == stableID && r.Count > 0 {
				return true
			}
		}
		return false
	}, "rollup do stable")

	// 2. Outage: derruba o serviço → DOWN + incidente após N falhas.
	flip.set(http.StatusInternalServerError, "boom")
	pollUntil(t, 8*time.Second, func() bool {
		st, incID, ok := eng.RuntimeState(outageID)
		return ok && st == domain.StatusDown && incID != 0
	}, "outage DOWN com incidente")

	if incs := openIncidents(store); len(incs) != 1 {
		t.Fatalf("esperado 1 incidente aberto; got %d", len(incs))
	} else if incs[0].StartedAt.IsZero() {
		t.Fatal("incidente deve registrar início")
	}

	// 3. Recuperação: volta ao normal → UP + incidente fechado com duração.
	flip.set(http.StatusOK, "healthy")
	pollUntil(t, 8*time.Second, func() bool {
		st, incID, ok := eng.RuntimeState(outageID)
		return ok && st == domain.StatusUp && incID == 0
	}, "outage UP com incidente fechado")

	closed := closedIncidents(store)
	if len(closed) != 1 {
		t.Fatalf("esperado 1 incidente fechado; got %d", len(closed))
	}
	if closed[0].DurationMS == nil || *closed[0].DurationMS < 1 {
		t.Fatalf("incidente fechado deve ter duração > 0; got %v", closed[0].DurationMS)
	}

	// 4. Eventos de transição publicados (UP→DOWN e DOWN→UP).
	if events := drainEvents(evCh, 5*time.Second, 2); len(events) < 2 {
		t.Fatalf("esperado >= 2 eventos de transição; got %d (%v)", len(events), events)
	}

	// 5. Logs brutos gravados com detalhe (500 no fail).
	checks := store.ChecksExport()
	if len(checks) == 0 {
		t.Fatal("nenhum check gravado")
	}
	foundOutageFail := false
	for _, c := range checks {
		if c.EndpointID == outageID && c.Result == domain.ResultFail {
			foundOutageFail = true
			if c.HTTPStatus != 500 {
				t.Fatalf("fail deveria carregar http 500; got %d", c.HTTPStatus)
			}
		}
	}
	if !foundOutageFail {
		t.Fatal("esperado ao menos 1 check fail com status 500 no outage")
	}
}

// TestEngine_MultiEndpointConcorrente prova que o pool executa N endpoints
// simultaneamente e todos convergem para UP.
func TestEngine_MultiEndpointConcorrente(t *testing.T) {
	store := storage.NewMem()
	cfg := testCfg()
	cfg.WorkerPoolSize = 4

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(20 * time.Millisecond) // simula latência real
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	eng, err := New(context.Background(), cfg, store, testLog())
	if err != nil {
		t.Fatal(err)
	}
	go eng.Run(context.Background())
	t.Cleanup(func() { eng.Stop(); eng.Pool().Close() })

	const n = 12
	for i := 0; i < n; i++ {
		mustCreateEndpoint(t, store, domain.Endpoint{
			Name: "svc", URL: srv.URL, Method: "GET",
			Interval: 300 * time.Millisecond, Timeout: 2 * time.Second, Active: true,
			NextCheckAt: time.Now().Add(-time.Second),
		})
	}

	// Todos os endpoints devem convergir para UP.
	pollUntil(t, 10*time.Second, func() bool {
		eps, _ := store.ListEndpoints(context.Background())
		up := 0
		for _, ep := range eps {
			if ep.Status == domain.StatusUp {
				up++
			}
		}
		return up == n
	}, "todos os endpoints UP")

	// Checagens gravadas em lote para todos.
	pollUntil(t, 5*time.Second, func() bool {
		return len(store.ChecksExport()) >= n
	}, "checks para todos os endpoints")
}

// ─── helpers ──────────────────────────────────────────────────────────────

func pollUntil(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	last := ""
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		last = msg
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timeout aguardando: %s", last)
}

func openIncidents(s storage.Store) []domain.Incident {
	var out []domain.Incident
	for _, inc := range s.(*storage.MemStore).IncidentsExport() {
		if inc.EndedAt == nil {
			out = append(out, inc)
		}
	}
	return out
}

func closedIncidents(s storage.Store) []domain.Incident {
	var out []domain.Incident
	for _, inc := range s.(*storage.MemStore).IncidentsExport() {
		if inc.EndedAt != nil {
			out = append(out, inc)
		}
	}
	return out
}

// drainEvents lê transições do canal até atingir o mínimo ou o timeout.
func drainEvents(ch <-chan broker.Event, timeout time.Duration, min int) []string {
	deadline := time.Now().Add(timeout)
	var out []string
	for time.Now().Before(deadline) && len(out) < min {
		select {
		case ev := <-ch:
			out = append(out, string(ev.From)+"→"+string(ev.Status))
		case <-time.After(100 * time.Millisecond):
		}
	}
	return out
}
