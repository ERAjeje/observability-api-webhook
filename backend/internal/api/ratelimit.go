package api

import (
	"sync"
	"time"
)

// RateLimiter é um limitador de janela fixa por chave (ex.: IP+e-mail em
// login/signup — RNF-017). Excede o limite → 429.
type RateLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	hits   map[string]*rlWindow
}

type rlWindow struct {
	count int
	reset time.Time
}

// NewRateLimiter cria um limitador com limit tentativas por window.
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	if limit <= 0 {
		limit = 5
	}
	if window <= 0 {
		window = time.Minute
	}
	return &RateLimiter{limit: limit, window: window, hits: map[string]*rlWindow{}}
}

// Allow devolve true se a chave ainda tem cota na janela corrente.
func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	w, ok := rl.hits[key]
	if !ok || now.After(w.reset) {
		w = &rlWindow{count: 0, reset: now.Add(rl.window)}
		rl.hits[key] = w
		rl.prune()
	}
	w.count++
	return w.count <= rl.limit
}

// prune descarta janelas expiradas para evitar crescimento infinito.
func (rl *RateLimiter) prune() {
	if len(rl.hits) < 1024 {
		return
	}
	now := time.Now()
	for k, w := range rl.hits {
		if now.After(w.reset) {
			delete(rl.hits, k)
		}
	}
}

// Reset limpa o estado (uso em testes).
func (rl *RateLimiter) Reset() {
	rl.mu.Lock()
	rl.hits = map[string]*rlWindow{}
	rl.mu.Unlock()
}
