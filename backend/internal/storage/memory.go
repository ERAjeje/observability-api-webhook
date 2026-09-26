package storage

import (
	"context"
	"errors"
	"sync"
	"time"

	"monitor/internal/domain"
)

// MemStore é uma implementação goroutine-safe em memória — usada para
// desenvolvimento local sem banco (env DB_DSN vazio) e para os testes do
// Core Engine.
type MemStore struct {
	mu        sync.RWMutex
	endpoints map[int64]domain.Endpoint
	nextEPID  int64
	checks    []domain.Check
	nextChID  int64
	rollups   map[[2]int64]domain.Rollup // [endpointID, bucketUnix]
	incidents map[int64]domain.Incident
	nextIncID int64
}

// NewMem create a nova store em memória.
func NewMem() *MemStore {
	return &MemStore{
		endpoints: map[int64]domain.Endpoint{},
		rollups:   map[[2]int64]domain.Rollup{},
		incidents: map[int64]domain.Incident{},
	}
}

func (m *MemStore) Ping(context.Context) error { return nil }
func (m *MemStore) Close()                     {}

func (m *MemStore) CreateEndpoint(_ context.Context, e domain.Endpoint) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}
	e.UpdatedAt = e.CreatedAt
	m.nextEPID++
	e.ID = m.nextEPID
	m.endpoints[e.ID] = e
	return e.ID, nil
}

func (m *MemStore) GetEndpoint(_ context.Context, id int64) (domain.Endpoint, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.endpoints[id]
	if !ok {
		return domain.Endpoint{}, ErrNotFound
	}
	return e, nil
}

func (m *MemStore) ListEndpoints(_ context.Context) ([]domain.Endpoint, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]domain.Endpoint, 0, len(m.endpoints))
	for _, e := range m.endpoints {
		out = append(out, e)
	}
	return out, nil
}

func (m *MemStore) ListDueEndpoints(_ context.Context, now time.Time, atMost int) ([]domain.Endpoint, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]domain.Endpoint, 0)
	for _, e := range m.endpoints {
		if e.Active && !e.NextCheckAt.After(now) {
			out = append(out, e)
			if atMost > 0 && len(out) >= atMost {
				break
			}
		}
	}
	return out, nil
}

func (m *MemStore) UpdateEndpoint(_ context.Context, e domain.Endpoint) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.endpoints[e.ID]; !ok {
		return ErrNotFound
	}
	e.UpdatedAt = time.Now().UTC()
	m.endpoints[e.ID] = e
	return nil
}

func (m *MemStore) DeleteEndpoint(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.endpoints[id]; !ok {
		return ErrNotFound
	}
	delete(m.endpoints, id)
	return nil
}

func (m *MemStore) SetEndpointStatus(_ context.Context, id int64, s domain.StatusClass) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.endpoints[id]
	if !ok {
		return ErrNotFound
	}
	e.Status = s
	e.UpdatedAt = time.Now().UTC()
	m.endpoints[id] = e
	return nil
}

func (m *MemStore) SetNextCheckAt(_ context.Context, id int64, t time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.endpoints[id]
	if !ok {
		return ErrNotFound
	}
	e.NextCheckAt = t
	m.endpoints[id] = e
	return nil
}

func (m *MemStore) AppendChecks(_ context.Context, checks []domain.Check) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range checks {
		m.nextChID++
		c.ID = m.nextChID
		m.checks = append(m.checks, c)
	}
	return nil
}

func (m *MemStore) UpsertRollup(_ context.Context, r domain.Rollup) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := [2]int64{r.EndpointID, r.Bucket.Unix()}
	cur, ok := m.rollups[key]
	if !ok {
		cur = r
	} else {
		cur.Count += r.Count
		cur.OKCount += r.OKCount
		cur.SumLatencyMS += r.SumLatencyMS
		if r.P95LatencyMS > cur.P95LatencyMS {
			cur.P95LatencyMS = r.P95LatencyMS
		}
	}
	m.rollups[key] = cur
	return nil
}

func (m *MemStore) CreateIncident(_ context.Context, inc domain.Incident) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextIncID++
	inc.ID = m.nextIncID
	m.incidents[inc.ID] = inc
	return inc.ID, nil
}

func (m *MemStore) CloseIncident(_ context.Context, id int64, endedAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	inc, ok := m.incidents[id]
	if !ok {
		return ErrNotFound
	}
	d := endedAt.Sub(inc.StartedAt).Milliseconds()
	inc.EndedAt = &endedAt
	inc.DurationMS = &d
	m.incidents[id] = inc
	return nil
}

// RollupsExport devolve todos os rollups (uso em testes/verificação).
func (m *MemStore) RollupsExport() []domain.Rollup {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]domain.Rollup, 0, len(m.rollups))
	for _, r := range m.rollups {
		out = append(out, r)
	}
	return out
}

// ChecksExport devolve todos os checks (uso em testes/verificação).
func (m *MemStore) ChecksExport() []domain.Check {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]domain.Check, len(m.checks))
	copy(out, m.checks)
	return out
}

// IncidentsExport devolve todos os incidents (uso em testes/verificação).
func (m *MemStore) IncidentsExport() []domain.Incident {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]domain.Incident, len(m.incidents))
	i := 0
	for _, inc := range m.incidents {
		out[i] = inc
		i++
	}
	return out
}

// ErrNotFound indica recurso ausente.
var ErrNotFound = errors.New("storage: not found")