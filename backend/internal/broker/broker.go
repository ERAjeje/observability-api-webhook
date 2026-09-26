// Package broker implementa o barramento pub/sub em memória que alimentará
// o streaming SSE (arquitetura §4.3 — Fase 3). Publicações são
// não-bloqueantes: um consumidor lento nunca trava os workers.
package broker

import (
	"sync"
	"time"

	"monitor/internal/domain"
)

// Event é um evento de domínio publicado pelo engine.
type Event struct {
	Type       string // status_changed | incident_opened | incident_closed | rollup_updated | endpoint_removed
	Timestamp  time.Time
	EndpointID int64
	Name       string
	Status     domain.StatusClass
	From       domain.StatusClass
	IncidentID int64
	DurationMS int64
	// Dados de rollup (rollup_updated).
	Bucket time.Time
	Count  int64
	P50MS  int64
	P95MS  int64
	AvgMS  int64
}

const (
	EventStatusChanged   = "status_changed"
	EventIncidentOpened  = "incident_opened"
	EventIncidentClosed  = "incident_closed"
	EventRollupUpdated   = "rollup_updated"
	EventEndpointRemoved = "endpoint_removed"
)

const subBuffer = 256

type subscriber struct {
	ch   chan Event
	drop atomicCounter
}

type atomicCounter = counter

// counter é um contador atômico simples (drops por subscriber).
type counter struct {
	mu sync.Mutex
	n  int
}

func (c *counter) Inc()          { c.mu.Lock(); c.n++; c.mu.Unlock() }
func (c *counter) Snapshot() int { c.mu.Lock(); defer c.mu.Unlock(); return c.n }

// Broker publica eventos por tópico para múltiplos assinantes.
type Broker struct {
	mu     sync.RWMutex
	closed bool
	subs   map[string]map[*subscriber]struct{}
}

// New cria um broker vazio.
func New() *Broker {
	return &Broker{subs: map[string]map[*subscriber]struct{}{}}
}

// Subscribe assina um tópico e devolve um canal + unsubscribe.
func (b *Broker) Subscribe(topic string) (<-chan Event, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.subs[topic] == nil {
		b.subs[topic] = map[*subscriber]struct{}{}
	}
	s := &subscriber{ch: make(chan Event, subBuffer)}
	b.subs[topic][s] = struct{}{}
	once := sync.Once{}
	unsub := func() {
		once.Do(func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			if _, ok := b.subs[topic][s]; ok {
				delete(b.subs[topic], s)
				close(s.ch)
			}
		})
	}
	return s.ch, unsub
}

// Publish envia o evento a todos os assinantes do tópico sem bloquear.
// Assinantes com buffer cheio são marcados (drop) e seguem recebendo os
// próximos eventos — a non-blocking bar frombertura é intencional.
func (b *Broker) Publish(topic string, ev Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return
	}
	for s := range b.subs[topic] {
		select {
		case s.ch <- ev:
		default:
			s.drop.Inc()
		}
	}
}

// Close fecha o broker e todos os canais de assinatura.
func (b *Broker) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	for _, set := range b.subs {
		for s := range set {
			close(s.ch)
		}
	}
	b.subs = map[string]map[*subscriber]struct{}{}
}
