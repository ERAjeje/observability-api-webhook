package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"monitor/internal/domain"
	"monitor/internal/storage"
	"monitor/internal/worker"
)

func testLog() *slog.Logger { return slog.New(slog.DiscardHandler) }

// fakeSubmit captura jobs e devolve sucesso configurável.
type fakeSubmit struct {
	mu       sync.Mutex
	accepted bool
	jobs     []worker.Job
}

func (f *fakeSubmit) Submit(j worker.Job) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.accepted {
		return false
	}
	f.jobs = append(f.jobs, j)
	return true
}

func (f *fakeSubmit) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.jobs)
}

func dueEndpoint(id int64, due time.Time) domain.Endpoint {
	return domain.Endpoint{
		ID: id, Name: "svc", URL: "http://localhost:1", Method: "GET",
		Interval: time.Minute, Timeout: time.Second, Active: true,
		NextCheckAt: due,
	}
}

func TestScheduler_RoundEnfileiraSomenteVencidos(t *testing.T) {
	store := storage.NewMem()
	ctx := context.Background()
	now := time.Now()

	// vencido vs futuro
	mustCreate(t, store, dueEndpoint(1, now.Add(-time.Minute)))
	mustCreate(t, store, dueEndpoint(2, now.Add(time.Hour)))

	sub := &fakeSubmit{accepted: true}
	inflight := map[int64]bool{}
	var mu sync.Mutex

	s := New(store, testLog())
	s.Tick = time.Hour // irrelevante: chamamos round diretamente
	s.Submit = sub.Submit
	s.IsInFlight = func(id int64) bool { mu.Lock(); defer mu.Unlock(); return inflight[id] }
	s.MarkInFlight = func(id int64) { mu.Lock(); defer mu.Unlock(); inflight[id] = true }
	s.Release = func(id int64) { mu.Lock(); defer mu.Unlock(); inflight[id] = false }

	s.round(ctx, now)

	if sub.count() != 1 {
		t.Fatalf("esperado 1 job (só o vencido); got %d", sub.count())
	}
	if got := sub.jobs[0].Endpoint.ID; got != 1 {
		t.Fatalf("job deveria ser do endpoint 1; got %d", got)
	}
}

// RF-011 — endpoint já em voo não é re-enfileirado.
func TestScheduler_SkippaInFlight(t *testing.T) {
	store := storage.NewMem()
	ctx := context.Background()
	now := time.Now()
	mustCreate(t, store, dueEndpoint(1, now.Add(-time.Minute)))

	sub := &fakeSubmit{accepted: true}
	s := New(store, testLog())
	inflight := map[int64]bool{1: true}
	var mu sync.Mutex
	s.Submit = sub.Submit
	s.IsInFlight = func(id int64) bool { mu.Lock(); defer mu.Unlock(); return inflight[id] }
	s.MarkInFlight = func(id int64) { mu.Lock(); defer mu.Unlock(); inflight[id] = true }
	s.Release = func(id int64) { mu.Lock(); defer mu.Unlock(); inflight[id] = false }

	s.round(ctx, now)
	if sub.count() != 0 {
		t.Fatalf("endpoint in-flight não deve gerar job; got %d", sub.count())
	}
}

// Backpressure: buffer cheio → release para reprocessar no próximo tick.
func TestScheduler_BackpressureLiberaEndpoint(t *testing.T) {
	store := storage.NewMem()
	ctx := context.Background()
	now := time.Now()
	mustCreate(t, store, dueEndpoint(1, now.Add(-time.Minute)))

	type marked struct {
		id int64
		in bool
	} // captura liberação
	var log []marked
	var mu sync.Mutex
	sub := &fakeSubmit{accepted: false} // simula queue cheia
	s := New(store, testLog())
	s.Submit = sub.Submit
	s.IsInFlight = func(int64) bool { return false }
	s.MarkInFlight = func(id int64) { mu.Lock(); log = append(log, marked{id, true}); mu.Unlock() }
	s.Release = func(id int64) { mu.Lock(); log = append(log, marked{id, false}); mu.Unlock() }

	s.round(ctx, now)
	mu.Lock()
	defer mu.Unlock()
	if len(log) != 2 || log[0].in != true || log[1].in != false {
		t.Fatalf("esperado mark+release (backpressure); got %+v", log)
	}
}

func TestScheduler_ApplyJitterDentroDoIntervalo(t *testing.T) {
	base := 60 * time.Second
	for i := 0; i < 500; i++ {
		d, err := ApplyJitter(base, 0.2)
		if err != nil {
			t.Fatalf("jitter: %v", err)
		}
		if d < base-(20*time.Second) || d > base+(20*time.Second) {
			t.Fatalf("jitter fora da faixa ±20%%: %v", d)
		}
	}
}

func mustCreate(t *testing.T, s storage.Store, e domain.Endpoint) int64 {
	t.Helper()
	id, err := s.CreateEndpoint(context.Background(), e)
	if err != nil {
		t.Fatalf("criar endpoint: %v", err)
	}
	return id
}
