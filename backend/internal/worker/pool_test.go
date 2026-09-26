package worker

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testLog() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// jobBasic monta um Job com Due preenchido.
func jobBasic() Job { return Job{Due: time.Now()} }

func TestPool_ExecutaConcorrente(t *testing.T) {
	const nJobs, nWorkers = 500, 16
	var mu sync.Mutex
	hits := map[int64]int{}

	p := New(nWorkers, nJobs+100, func(_ context.Context, _ Job) error {
		mu.Lock()
		hits[time.Now().UnixNano()%nJobs]++
		mu.Unlock()
		time.Sleep(2 * time.Millisecond)
		return nil
	}, testLog())
	p.Start(context.Background())

	for i := 0; i < nJobs; i++ {
		if !p.TryEnqueue(jobBasic()) {
			t.Fatalf("falhou ao enfileirar job %d", i)
		}
	}
	p.Close()

	if got := p.metrics.Processed.Load(); got != nJobs {
		t.Fatalf("processados esperado %d; got %d", nJobs, got)
	}
}

// Panic no handler não pode derrubar o pool nem os demais jobs.
func TestPool_PanicRecuperado(t *testing.T) {
	var processed atomic.Int64
	p := New(2, 10, func(_ context.Context, j Job) error {
		if j.Due.UnixNano()%2 == 0 {
			panic("boom")
		}
		processed.Add(1)
		return nil
	}, testLog())
	p.Start(context.Background())

	now := time.Now()
	for i := 0; i < 20; i++ {
		p.TryEnqueue(Job{Due: now.Add(time.Duration(i) * time.Nanosecond)})
	}
	p.Close()

	if got := p.metrics.Panicked.Load(); got == 0 {
		t.Fatal("esperado panics recuperados > 0")
	}
	if processed.Load() == 0 {
		t.Fatal("jobs válidos deveriam ter sido processados após panics")
	}
}

// Buffer cheio → TryEnqueue retorna false (backpressure) e nunca trava.
func TestPool_BackpressureNaoBloqueia(t *testing.T) {
	p := New(1, 2, func(ctx context.Context, j Job) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
		return nil
	}, testLog())
	p.Start(context.Background())
	defer p.Close()

	if !p.TryEnqueue(jobBasic()) {
		t.Fatal("primeiro job deveria entrar")
	}
	if !p.TryEnqueue(jobBasic()) {
		t.Fatal("segundo job deveria entrar (buffer)")
	}
	// Worker está ocupado, buffer cheio → drop (não-bloqueante).
	for i := 0; i < 3; i++ {
		if p.TryEnqueue(jobBasic()) {
			t.Fatalf("job %d não deveria entrar com buffer cheio", i)
		}
	}
	if p.metrics.Dropped.Load() < 3 {
		t.Fatalf("esperado got dropped >= 3; got %d", p.metrics.Dropped.Load())
	}
}

// Graceful drain: após Close, todos os jobs enfileirados são processados.
func TestPool_GracefulDrain(t *testing.T) {
	const n = 200
	var processed atomic.Int64
	p := New(4, n+100, func(_ context.Context, _ Job) error {
		processed.Add(1)
		time.Sleep(5 * time.Millisecond)
		return nil
	}, testLog())
	p.Start(context.Background())

	for i := 0; i < n; i++ {
		p.TryEnqueue(jobBasic())
	}
	p.Close() // bloqueia até drenar
	if got := processed.Load(); got != n {
		t.Fatalf("drain: esperado %d processados; got %d", n, got)
	}
}

// Handler com erro é contabilizado como falha, mas o pool segue vivo.
func TestPool_HandlerErroNaoMataPool(t *testing.T) {
	p := New(2, 16, func(_ context.Context, _ Job) error {
		return errors.New("checagem falhou")
	}, testLog())
	p.Start(context.Background())
	for i := 0; i < 10; i++ {
		p.TryEnqueue(jobBasic())
	}
	p.Close()
	if p.metrics.Failed.Load() != 10 {
		t.Fatalf("esperado 10 falhas; got %d", p.metrics.Failed.Load())
	}
	if p.metrics.Processed.Load() != 0 {
		t.Fatalf("nenhum job deveria contar como processado; got %d", p.metrics.Processed.Load())
	}
}

// Submissões após Close são recusadas.
func TestPool_RejeitaAposClose(t *testing.T) {
	p := New(1, 4, func(_ context.Context, _ Job) error { return nil }, testLog())
	p.Start(context.Background())
	p.Close()
	if p.TryEnqueue(jobBasic()) {
		t.Fatal("submissão após Close deveria ser recusada")
	}
}
