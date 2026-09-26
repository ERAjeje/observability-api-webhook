// Package storage abstrai a persistência (arquitetura §5). Duas
// implementações: memory (dev/testes) e postgres (produção).
package storage

import (
	"context"
	"time"

	"monitor/internal/domain"
)

// Store é o contrato usado pelo engine. Implementações devem ser
// goroutine-safe.
type Store interface {
	Ping(ctx context.Context) error
	Close()

	// Endpoints (RF-001..RF-005)
	CreateEndpoint(ctx context.Context, e domain.Endpoint) (int64, error)
	GetEndpoint(ctx context.Context, id int64) (domain.Endpoint, error)
	ListEndpoints(ctx context.Context) ([]domain.Endpoint, error)
	// ListDueEndpoints retorna endpoints ativos com NextCheckAt <= now,
	// limitado a at most para limitar trabalho por tick (T2.3).
	ListDueEndpoints(ctx context.Context, now time.Time, atMost int) ([]domain.Endpoint, error)
	UpdateEndpoint(ctx context.Context, e domain.Endpoint) error
	DeleteEndpoint(ctx context.Context, id int64) error
	SetEndpointStatus(ctx context.Context, id int64, s domain.StatusClass) error
	SetNextCheckAt(ctx context.Context, id int64, t time.Time) error

	// Checks & rollups (RF-014..RF-015)
	AppendChecks(ctx context.Context, checks []domain.Check) error
	UpsertRollup(ctx context.Context, r domain.Rollup) error

	// Incidents (RF-017)
	CreateIncident(ctx context.Context, inc domain.Incident) (int64, error)
	CloseIncident(ctx context.Context, id int64, endedAt time.Time) error
}
