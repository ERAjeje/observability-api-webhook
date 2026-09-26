package storage

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"monitor/internal/domain"
)

// PgStore implementa Store sobre PostgreSQL usando pgxpool
// (arquitetura §5, T1.4/T1.5).
type PgStore struct {
	pool *pgxpool.Pool

	partMu   sync.Mutex
	lastPart string // mês (YYYY-MM) com partição garantida
}

// NewPg abre o pool e confere conectividade.
func NewPg(ctx context.Context, dsn string) (*PgStore, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("pg: criar pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pg: ping: %w", err)
	}
	return &PgStore{pool: pool}, nil
}

func (p *PgStore) Ping(ctx context.Context) error {
	return p.pool.Ping(ctx)
}
func (p *PgStore) Close() { p.pool.Close() }

// ─── Endpoints ────────────────────────────────────────────────────────────

const endpointCols = `id, group_id, name, url, method, headers, body,
	interval_seconds, timeout_ms, lat_threshold_ms, expect_status, expect_body,
	active, status, next_check_at, created_at, updated_at`

func scanEndpoint(row pgx.Row) (domain.Endpoint, error) {
	var e domain.Endpoint
	var groupID *int64
	var intervalS, timeoutMS, latThr int
	if err := row.Scan(
		&e.ID, &groupID, &e.Name, &e.URL, &e.Method, &e.Headers, &e.Body,
		&intervalS, &timeoutMS, &latThr, &e.ExpectStatus, &e.ExpectBody,
		&e.Active, &e.Status, &e.NextCheckAt, &e.CreatedAt, &e.UpdatedAt,
	); err != nil {
		return e, err
	}
	e.GroupID = groupID
	e.Interval = time.Duration(intervalS) * time.Second
	e.Timeout = time.Duration(timeoutMS) * time.Millisecond
	e.LatencyThreshold = time.Duration(latThr) * time.Millisecond
	return e, nil
}

func (p *PgStore) CreateEndpoint(ctx context.Context, e domain.Endpoint) (int64, error) {
	intervalS := int(e.Interval.Seconds())
	timeoutMS := int(e.Timeout.Milliseconds())
	latThr := int(e.LatencyThreshold.Milliseconds())
	var id int64
	err := p.pool.QueryRow(ctx, `
		INSERT INTO endpoints (group_id, name, url, method, headers, body,
			interval_seconds, timeout_ms, lat_threshold_ms, expect_status, expect_body,
			active, status, next_check_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13, now())
		RETURNING id`,
		e.GroupID, e.Name, e.URL, e.Method, e.Headers, e.Body,
		intervalS, timeoutMS, latThr, e.ExpectStatus, e.ExpectBody,
		e.Active, domain.StatusUnknown,
	).Scan(&id)
	return id, err
}

func (p *PgStore) GetEndpoint(ctx context.Context, id int64) (domain.Endpoint, error) {
	row := p.pool.QueryRow(ctx,
		`SELECT `+endpointCols+` FROM endpoints WHERE id=$1`, id)
	e, err := scanEndpoint(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Endpoint{}, ErrNotFound
	}
	return e, err
}

func (p *PgStore) ListEndpoints(ctx context.Context) ([]domain.Endpoint, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT `+endpointCols+` FROM endpoints ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Endpoint
	for rows.Next() {
		e, err := scanEndpoint(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (p *PgStore) ListDueEndpoints(ctx context.Context, now time.Time, atMost int) ([]domain.Endpoint, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT `+endpointCols+` FROM endpoints
		WHERE active AND next_check_at <= $1
		ORDER BY next_check_at
		LIMIT $2`, now, atMost)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Endpoint
	for rows.Next() {
		e, err := scanEndpoint(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (p *PgStore) UpdateEndpoint(ctx context.Context, e domain.Endpoint) error {
	intervalS := int(e.Interval.Seconds())
	timeoutMS := int(e.Timeout.Milliseconds())
	latThr := int(e.LatencyThreshold.Milliseconds())
	tag, err := p.pool.Exec(ctx, `
		UPDATE endpoints SET group_id=$2, name=$3, url=$4, method=$5, headers=$6,
			body=$7, interval_seconds=$8, timeout_ms=$9, lat_threshold_ms=$10,
			expect_status=$11, expect_body=$12, active=$13, updated_at=now()
		WHERE id=$1`,
		e.ID, e.GroupID, e.Name, e.URL, e.Method, e.Headers, e.Body,
		intervalS, timeoutMS, latThr, e.ExpectStatus, e.ExpectBody, e.Active)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *PgStore) DeleteEndpoint(ctx context.Context, id int64) error {
	tag, err := p.pool.Exec(ctx, `DELETE FROM endpoints WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *PgStore) SetEndpointStatus(ctx context.Context, id int64, s domain.StatusClass) error {
	_, err := p.pool.Exec(ctx,
		`UPDATE endpoints SET status=$2, updated_at=now() WHERE id=$1`, id, s)
	return err
}

func (p *PgStore) SetNextCheckAt(ctx context.Context, id int64, t time.Time) error {
	_, err := p.pool.Exec(ctx,
		`UPDATE endpoints SET next_check_at=$2 WHERE id=$1`, id, t)
	return err
}

// ─── Checks & rollups ─────────────────────────────────────────────────────

// AppendChecks grava em lote usando pgx.Batch e garante a partição do mês
// em uso (arquitetura §5.2, T2.6).
func (p *PgStore) AppendChecks(ctx context.Context, checks []domain.Check) error {
	if len(checks) == 0 {
		return nil
	}
	if err := p.ensurePartition(ctx, checks[0].CheckedAt); err != nil {
		return err
	}
	batch := &pgx.Batch{}
	for _, c := range checks {
		batch.Queue(`
			INSERT INTO checks (endpoint_id, checked_at, result, http_status, latency_ms, error_detail)
			VALUES ($1,$2,$3,$4,$5,$6)`,
			c.EndpointID, c.CheckedAt, c.Result, c.HTTPStatus, c.LatencyMS, c.ErrorDetail)
	}
	br := p.pool.SendBatch(ctx, batch)
	defer br.Close()
	for range checks {
		if _, err := br.Exec(); err != nil {
			return err
		}
	}
	return nil
}

func (p *PgStore) UpsertRollup(ctx context.Context, r domain.Rollup) error {
	_, err := p.pool.Exec(ctx, `
		INSERT INTO check_rollups_minute (endpoint_id, bucket, count, ok_count, sum_latency_ms, p50_latency_ms, p95_latency_ms)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (endpoint_id, bucket) DO UPDATE SET
			count = check_rollups_minute.count + EXCLUDED.count,
			ok_count = check_rollups_minute.ok_count + EXCLUDED.ok_count,
			sum_latency_ms = check_rollups_minute.sum_latency_ms + EXCLUDED.sum_latency_ms,
			p50_latency_ms = GREATEST(check_rollups_minute.p50_latency_ms, EXCLUDED.p50_latency_ms),
			p95_latency_ms = GREATEST(check_rollups_minute.p95_latency_ms, EXCLUDED.p95_latency_ms)`,
		r.EndpointID, r.Bucket, r.Count, r.OKCount, r.SumLatencyMS, r.P50LatencyMS, r.P95LatencyMS)
	return err
}

// ─── Incidents ────────────────────────────────────────────────────────────

func (p *PgStore) CreateIncident(ctx context.Context, inc domain.Incident) (int64, error) {
	var id int64
	err := p.pool.QueryRow(ctx, `
		INSERT INTO incidents (endpoint_id, started_at, resolution)
		VALUES ($1,$2,$3) RETURNING id`,
		inc.EndpointID, inc.StartedAt, inc.Resolution).Scan(&id)
	return id, err
}

func (p *PgStore) CloseIncident(ctx context.Context, id int64, endedAt time.Time) error {
	_, err := p.pool.Exec(ctx, `
		UPDATE incidents SET ended_at=$2,
			duration_ms = EXTRACT(EPOCH FROM ($2 - started_at)) * 1000
		WHERE id=$1`, id, endedAt)
	return err
}

// ─── Groups (RF-005, T3.2) ────────────────────────────────────────────────

func (p *PgStore) CreateGroup(ctx context.Context, g domain.CheckGroup) (int64, error) {
	var id int64
	err := p.pool.QueryRow(ctx, `
		INSERT INTO check_groups (name, display_order)
		VALUES ($1,$2) RETURNING id`,
		g.Name, g.DisplayOrder).Scan(&id)
	return id, err
}

func (p *PgStore) GetGroup(ctx context.Context, id int64) (domain.CheckGroup, error) {
	var g domain.CheckGroup
	err := p.pool.QueryRow(ctx,
		`SELECT id, name, display_order, created_at FROM check_groups WHERE id=$1`, id).
		Scan(&g.ID, &g.Name, &g.DisplayOrder, &g.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.CheckGroup{}, ErrNotFound
	}
	return g, err
}

func (p *PgStore) ListGroups(ctx context.Context) ([]domain.CheckGroup, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT id, name, display_order, created_at FROM check_groups
		 ORDER BY display_order, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.CheckGroup
	for rows.Next() {
		var g domain.CheckGroup
		if err := rows.Scan(&g.ID, &g.Name, &g.DisplayOrder, &g.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (p *PgStore) UpdateGroup(ctx context.Context, g domain.CheckGroup) error {
	tag, err := p.pool.Exec(ctx,
		`UPDATE check_groups SET name=$2, display_order=$3 WHERE id=$1`,
		g.ID, g.Name, g.DisplayOrder)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *PgStore) DeleteGroup(ctx context.Context, id int64) error {
	tag, err := p.pool.Exec(ctx, `DELETE FROM check_groups WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ─── Users (T3.1) ─────────────────────────────────────────────────────────

func (p *PgStore) CreateUser(ctx context.Context, u domain.User) (int64, error) {
	var id int64
	err := p.pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ($1,$2) RETURNING id`,
		u.Email, u.PasswordHash).Scan(&id)
	if err != nil {
		// 23505 = unique_violation (e-mail duplicado)
		if pgErr, ok := err.(*pgconn.PgError); ok && pgErr.Code == "23505" {
			return 0, ErrConflict
		}
	}
	return id, err
}

func (p *PgStore) GetUserByEmail(ctx context.Context, email string) (domain.User, error) {
	var u domain.User
	err := p.pool.QueryRow(ctx,
		`SELECT id, email, password_hash, created_at FROM users WHERE email=$1`, email).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, ErrNotFound
	}
	return u, err
}

// ─── Consultas (RF-018 / RF-022 / RF-029) ─────────────────────────────────

func (p *PgStore) ListChecks(ctx context.Context, f CheckFilter) ([]domain.Check, error) {
	query := `SELECT id, endpoint_id, checked_at, result, http_status, latency_ms, error_detail
		FROM checks WHERE 1=1`
	args := []any{}
	add := func(cond string, val any) {
		args = append(args, val)
		query += fmt.Sprintf(" AND %s $%d", cond, len(args))
	}
	if f.EndpointID > 0 {
		add("endpoint_id =", f.EndpointID)
	}
	if f.Result != "" {
		add("result =", f.Result)
	}
	if !f.From.IsZero() {
		add("checked_at >=", f.From)
	}
	if !f.To.IsZero() {
		add("checked_at <=", f.To)
	}
	limit, offset := f.Limit, f.Offset
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	query += fmt.Sprintf(" ORDER BY checked_at DESC, id DESC LIMIT $%d OFFSET $%d", len(args)+1, len(args)+2)
	args = append(args, limit, offset)

	rows, err := p.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Check
	for rows.Next() {
		var c domain.Check
		if err := rows.Scan(&c.ID, &c.EndpointID, &c.CheckedAt, &c.Result, &c.HTTPStatus,
			&c.LatencyMS, &c.ErrorDetail); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (p *PgStore) ListRollups(ctx context.Context, endpointID int64, from, to time.Time, limit int) ([]domain.Rollup, error) {
	query := `SELECT endpoint_id, bucket, count, ok_count, sum_latency_ms, p50_latency_ms, p95_latency_ms
		FROM check_rollups_minute WHERE endpoint_id = $1`
	args := []any{endpointID}
	if !from.IsZero() {
		args = append(args, from)
		query += fmt.Sprintf(" AND bucket >= $%d", len(args))
	}
	if !to.IsZero() {
		args = append(args, to)
		query += fmt.Sprintf(" AND bucket <= $%d", len(args))
	}
	query += " ORDER BY bucket"
	if limit > 0 {
		args = append(args, limit)
		query += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	rows, err := p.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Rollup
	for rows.Next() {
		var r domain.Rollup
		if err := rows.Scan(&r.EndpointID, &r.Bucket, &r.Count, &r.OKCount,
			&r.SumLatencyMS, &r.P50LatencyMS, &r.P95LatencyMS); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (p *PgStore) GetIncident(ctx context.Context, id int64) (domain.Incident, error) {
	var inc domain.Incident
	err := p.pool.QueryRow(ctx,
		`SELECT id, endpoint_id, started_at, ended_at, duration_ms, resolution
		 FROM incidents WHERE id=$1`, id).
		Scan(&inc.ID, &inc.EndpointID, &inc.StartedAt, &inc.EndedAt, &inc.DurationMS, &inc.Resolution)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Incident{}, ErrNotFound
	}
	return inc, err
}

func (p *PgStore) ListIncidents(ctx context.Context, openOnly bool, limit int) ([]domain.Incident, error) {
	query := `SELECT id, endpoint_id, started_at, ended_at, duration_ms, resolution
		FROM incidents`
	if openOnly {
		query += ` WHERE ended_at IS NULL`
	}
	query += ` ORDER BY started_at DESC`
	if limit > 0 {
		query += fmt.Sprintf(` LIMIT %d`, limit)
	}
	rows, err := p.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Incident
	for rows.Next() {
		var inc domain.Incident
		if err := rows.Scan(&inc.ID, &inc.EndpointID, &inc.StartedAt, &inc.EndedAt,
			&inc.DurationMS, &inc.Resolution); err != nil {
			return nil, err
		}
		out = append(out, inc)
	}
	return out, rows.Err()
}

func (p *PgStore) CreateNotification(ctx context.Context, n domain.Notification) (int64, error) {
	var id int64
	err := p.pool.QueryRow(ctx, `
		INSERT INTO notifications (endpoint_id, incident_id, channel, payload, delivered_at, status)
		VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
		n.EndpointID, n.IncidentID, n.Channel, []byte(n.Payload), n.DeliveredAt, n.Status).Scan(&id)
	return id, err
}

func (p *PgStore) ListNotifications(ctx context.Context, endpointID int64, limit int) ([]domain.Notification, error) {
	query := `SELECT id, endpoint_id, incident_id, channel, payload, delivered_at, status
		FROM notifications WHERE 1=1`
	args := []any{}
	if endpointID > 0 {
		args = append(args, endpointID)
		query += fmt.Sprintf(" AND endpoint_id = $%d", len(args))
	}
	query += " ORDER BY id DESC"
	if limit > 0 {
		args = append(args, limit)
		query += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	rows, err := p.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Notification
	for rows.Next() {
		var n domain.Notification
		if err := rows.Scan(&n.ID, &n.EndpointID, &n.IncidentID, &n.Channel, &n.Payload,
			&n.DeliveredAt, &n.Status); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// ─── Partições ────────────────────────────────────────────────────────────

// ensurePartition garante que existe partição mensal para a data informada.
func (p *PgStore) ensurePartition(ctx context.Context, at time.Time) error {
	month := at.UTC().Format("2006-01")
	p.partMu.Lock()
	defer p.partMu.Unlock()
	if p.lastPart == month {
		return nil
	}
	start := at.UTC().Truncate(24*time.Hour).AddDate(0, 0, -at.UTC().Day()+1)
	end := start.AddDate(0, 1, 0)
	_, err := p.pool.Exec(ctx, `
		SELECT create_check_partition($1, $2, $3)`,
		"checks_"+month, start.Format("2006-01-02"), end.Format("2006-01-02"))
	if err != nil {
		return err
	}
	p.lastPart = month
	return nil
}
