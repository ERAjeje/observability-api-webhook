// Package storage abstrai a persistência (arquitetura §5). Duas
// implementações: memory (dev/testes) e postgres (produção).
package storage

import (
	"context"
	"encoding/json"
	"time"

	"monitor/internal/domain"
)

// CheckFilter filtra a listagem paginada de logs brutos (RF-018).
// Zero de EndpointID/Result e From/To zerados = sem filtro.
type CheckFilter struct {
	EndpointID int64
	From, To   time.Time
	Result     string // "ok" | "degraded" | "fail" | "" (todos)
	Limit      int
	Offset     int
}

// Store é o contrato usado pelo engine e pela API. Implementações devem ser
// goroutine-safe.
type Store interface {
	Ping(ctx context.Context) error
	Close()

	// Endpoints (RF-001..RF-005)
	CreateEndpoint(ctx context.Context, e domain.Endpoint) (int64, error)
	GetEndpoint(ctx context.Context, id int64) (domain.Endpoint, error)
	ListEndpoints(ctx context.Context) ([]domain.Endpoint, error)
	// ListEndpointsByOwner devolve os endpoints de uma conta (S-08).
	// ownerID = 0 devolve apenas endpoints legados sem dono.
	ListEndpointsByOwner(ctx context.Context, ownerID int64) ([]domain.Endpoint, error)
	// ListDueEndpoints retorna endpoints ativos com NextCheckAt <= now,
	// limitado a at most para limitar trabalho por tick (T2.3).
	ListDueEndpoints(ctx context.Context, now time.Time, atMost int) ([]domain.Endpoint, error)
	UpdateEndpoint(ctx context.Context, e domain.Endpoint) error
	DeleteEndpoint(ctx context.Context, id int64) error
	SetEndpointStatus(ctx context.Context, id int64, s domain.StatusClass) error
	SetNextCheckAt(ctx context.Context, id int64, t time.Time) error

	// Groups (RF-005)
	CreateGroup(ctx context.Context, g domain.CheckGroup) (int64, error)
	GetGroup(ctx context.Context, id int64) (domain.CheckGroup, error)
	ListGroups(ctx context.Context) ([]domain.CheckGroup, error)
	UpdateGroup(ctx context.Context, g domain.CheckGroup) error
	DeleteGroup(ctx context.Context, id int64) error

	// Users (RF-006, T3.1)
	CreateUser(ctx context.Context, u domain.User) (int64, error)
	GetUserByEmail(ctx context.Context, email string) (domain.User, error)

	// Checks & rollups (RF-014..RF-015)
	AppendChecks(ctx context.Context, checks []domain.Check) error
	UpsertRollup(ctx context.Context, r domain.Rollup) error
	ListChecks(ctx context.Context, f CheckFilter) ([]domain.Check, error)
	ListRollups(ctx context.Context, endpointID int64, from, to time.Time, limit int) ([]domain.Rollup, error)

	// Incidents (RF-017, RF-022)
	CreateIncident(ctx context.Context, inc domain.Incident) (int64, error)
	CloseIncident(ctx context.Context, id int64, endedAt time.Time) error
	GetIncident(ctx context.Context, id int64) (domain.Incident, error)
	ListIncidents(ctx context.Context, openOnly bool, limit int) ([]domain.Incident, error)

	// Notifications audit (RF-029)
	CreateNotification(ctx context.Context, n domain.Notification) (int64, error)
	ListNotifications(ctx context.Context, endpointID int64, limit int) ([]domain.Notification, error)

	// Settings (RF-023, T4.5) — chave/valor jsonb. ErrNotFound se ausente.
	GetSetting(ctx context.Context, key string) (json.RawMessage, error)
	SetSetting(ctx context.Context, key string, value json.RawMessage) error
}
