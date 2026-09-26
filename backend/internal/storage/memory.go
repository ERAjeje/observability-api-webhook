package storage

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"monitor/internal/domain"
)

// MemStore é uma implementação goroutine-safe em memória — usada para
// desenvolvimento local sem banco (env DB_DSN vazio) e para os testes do
// Core Engine.
type MemStore struct {
	mu          sync.RWMutex
	endpoints   map[int64]domain.Endpoint
	nextEPID    int64
	checks      []domain.Check
	nextChID    int64
	rollups     map[[2]int64]domain.Rollup // [endpointID, bucketUnix]
	incidents   map[int64]domain.Incident
	nextIncID   int64
	groups      map[int64]domain.CheckGroup
	nextGroupID int64
	users       map[string]domain.User // por e-mail normalizado
	nextUserID  int64
	notifs      []domain.Notification
	nextNotifID int64
}

// NewMem create a nova store em memória.
func NewMem() *MemStore {
	return &MemStore{
		endpoints: map[int64]domain.Endpoint{},
		rollups:   map[[2]int64]domain.Rollup{},
		incidents: map[int64]domain.Incident{},
		groups:    map[int64]domain.CheckGroup{},
		users:     map[string]domain.User{},
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
		if r.P50LatencyMS > cur.P50LatencyMS {
			cur.P50LatencyMS = r.P50LatencyMS
		}
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

// ListChecks devolve checks paginados e filtráveis (RF-018).
func (m *MemStore) ListChecks(_ context.Context, f CheckFilter) ([]domain.Check, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []domain.Check
	for _, c := range m.checks {
		if f.EndpointID > 0 && c.EndpointID != f.EndpointID {
			continue
		}
		if f.Result != "" && string(c.Result) != f.Result {
			continue
		}
		if !f.From.IsZero() && c.CheckedAt.Before(f.From) {
			continue
		}
		if !f.To.IsZero() && c.CheckedAt.After(f.To) {
			continue
		}
		out = append(out, c)
	}
	// Ordena da mais recente para a mais antiga (consistente com o pg).
	sort.Slice(out, func(i, j int) bool { return out[i].CheckedAt.After(out[j].CheckedAt) })
	limit, offset := f.Limit, f.Offset
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= len(out) {
		return []domain.Check{}, nil
	}
	end := offset + limit
	if end > len(out) {
		end = len(out)
	}
	return out[offset:end], nil
}

// ListRollups devolve rollups ordenados por bucket ascendente (séries).
func (m *MemStore) ListRollups(_ context.Context, endpointID int64, from, to time.Time, limit int) ([]domain.Rollup, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []domain.Rollup
	for _, r := range m.rollups {
		if r.EndpointID != endpointID {
			continue
		}
		if !from.IsZero() && r.Bucket.Before(from) {
			continue
		}
		if !to.IsZero() && r.Bucket.After(to) {
			continue
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Bucket.Before(out[j].Bucket) })
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

// GetIncident devolve um incidente por id (RF-028 — resumo da recuperação).
func (m *MemStore) GetIncident(_ context.Context, id int64) (domain.Incident, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	inc, ok := m.incidents[id]
	if !ok {
		return domain.Incident{}, ErrNotFound
	}
	return inc, nil
}

// ListIncidents devolve incidentes (só abertos ou todos) por recência.
func (m *MemStore) ListIncidents(_ context.Context, openOnly bool, limit int) ([]domain.Incident, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []domain.Incident
	for _, inc := range m.incidents {
		if openOnly && inc.EndedAt != nil {
			continue
		}
		out = append(out, inc)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// ─── Users (T3.1) ─────────────────────────────────────────────────────────

func (m *MemStore) CreateUser(_ context.Context, u domain.User) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	email := normalizeEmailKey(u.Email)
	if _, exists := m.users[email]; exists {
		return 0, ErrConflict
	}
	m.nextUserID++
	u.ID = m.nextUserID
	if u.CreatedAt.IsZero() {
		u.CreatedAt = time.Now().UTC()
	}
	u.Email = email
	m.users[email] = u
	return u.ID, nil
}

func (m *MemStore) GetUserByEmail(_ context.Context, email string) (domain.User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	u, ok := m.users[normalizeEmailKey(email)]
	if !ok {
		return domain.User{}, ErrNotFound
	}
	return u, nil
}

func normalizeEmailKey(e string) string {
	b := make([]byte, 0, len(e))
	for i := 0; i < len(e); i++ {
		c := e[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b = append(b, c)
	}
	return string(b)
}

// ─── Groups (RF-005, T3.2) ────────────────────────────────────────────────

func (m *MemStore) CreateGroup(_ context.Context, g domain.CheckGroup) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextGroupID++
	g.ID = m.nextGroupID
	if g.CreatedAt.IsZero() {
		g.CreatedAt = time.Now().UTC()
	}
	m.groups[g.ID] = g
	return g.ID, nil
}

func (m *MemStore) GetGroup(_ context.Context, id int64) (domain.CheckGroup, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	g, ok := m.groups[id]
	if !ok {
		return domain.CheckGroup{}, ErrNotFound
	}
	return g, nil
}

func (m *MemStore) ListGroups(_ context.Context) ([]domain.CheckGroup, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]domain.CheckGroup, 0, len(m.groups))
	for _, g := range m.groups {
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].DisplayOrder == out[j].DisplayOrder {
			return out[i].ID < out[j].ID
		}
		return out[i].DisplayOrder < out[j].DisplayOrder
	})
	return out, nil
}

func (m *MemStore) UpdateGroup(_ context.Context, g domain.CheckGroup) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.groups[g.ID]; !ok {
		return ErrNotFound
	}
	m.groups[g.ID] = g
	return nil
}

func (m *MemStore) DeleteGroup(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.groups[id]; !ok {
		return ErrNotFound
	}
	delete(m.groups, id)
	return nil
}

// ─── Notifications (RF-029) ───────────────────────────────────────────────

func (m *MemStore) CreateNotification(_ context.Context, n domain.Notification) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextNotifID++
	n.ID = m.nextNotifID
	m.notifs = append(m.notifs, n)
	return n.ID, nil
}

func (m *MemStore) ListNotifications(_ context.Context, endpointID int64, limit int) ([]domain.Notification, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []domain.Notification
	for _, n := range m.notifs {
		if endpointID > 0 && n.EndpointID != endpointID {
			continue
		}
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].DeliveredAt, out[j].DeliveredAt
		if a == nil || b == nil {
			return false
		}
		return a.After(*b)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// IncidentsExport devolve todos os incidents (uso em testes/verificação).
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

// ErrConflict indica violação de unicidade (ex.: e-mail já cadastrado).
var ErrConflict = errors.New("storage: conflict")
