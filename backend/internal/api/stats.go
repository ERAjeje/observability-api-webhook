package api

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"time"

	"monitor/internal/domain"
	"monitor/internal/storage"
)

// parseTimeParam lê um timestamp RFC3339 da query, ou zero se ausente.
func parseTimeParam(r *http.Request, key string) (time.Time, error) {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, err
	}
	return t, nil
}

// listChecks devolve logs brutos paginados e filtráveis (RF-018).
func (s *Server) listChecks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := storage.CheckFilter{
		Result: q.Get("status"),
		Limit:  50,
	}
	if v := q.Get("endpoint_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "endpoint_id inválido")
			return
		}
		f.EndpointID = id
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 100 {
			writeErr(w, http.StatusBadRequest, "limit deve estar entre 1 e 100")
			return
		}
		f.Limit = n
	}
	if v := q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeErr(w, http.StatusBadRequest, "offset inválido")
			return
		}
		f.Offset = n
	}
	if from, err := parseTimeParam(r, "from"); err != nil {
		writeErr(w, http.StatusBadRequest, "from deve ser RFC3339")
		return
	} else {
		f.From = from
	}
	if to, err := parseTimeParam(r, "to"); err != nil {
		writeErr(w, http.StatusBadRequest, "to deve ser RFC3339")
		return
	} else {
		f.To = to
	}
	checks, err := s.store.ListChecks(r.Context(), f)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "falha ao listar checks")
		return
	}
	out := make([]map[string]any, 0, len(checks))
	for _, c := range checks {
		out = append(out, checkDTO(c))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":  out,
		"limit":  f.Limit,
		"offset": f.Offset,
	})
}

func checkDTO(c domain.Check) map[string]any {
	return map[string]any{
		"id":           c.ID,
		"endpoint_id":  c.EndpointID,
		"checked_at":   c.CheckedAt,
		"result":       c.Result,
		"http_status":  c.HTTPStatus,
		"latency_ms":   c.LatencyMS,
		"error_detail": c.ErrorDetail,
	}
}

// seriesResponse monta a série a partir dos rollups (RNF-014). grain é a
// granularidade de agregação: minute (default), hour, day — mantém o teto de
// pontos (1.440) cobrindo períodos maiores sem perder histórico.
func (s *Server) seriesResponse(ctx context.Context, epID int64, from, to time.Time, limit int, grain string) ([]map[string]any, error) {
	rollups, err := s.store.ListRollups(ctx, epID, from, to, 0)
	if err != nil {
		return nil, err
	}
	out := aggregateGrain(rollups, grain)
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

var grainTrunc = map[string]func(time.Time) time.Time{
	"minute": func(t time.Time) time.Time { return t.Truncate(time.Minute) },
	"hour":   func(t time.Time) time.Time { return t.Truncate(time.Hour) },
	"day":    func(t time.Time) time.Time { return t.Truncate(24 * time.Hour) },
}

// aggregateGrain agrupa rollups por minuto/hora/dia mantendo ok/sum/percentis.
// P50/P95 de um grupo = valor máximo do sub-bucket (mesma aproximação do upsert
// do PostgreSQL — documentada na arquitetura §5.2).
func aggregateGrain(rollups []domain.Rollup, grain string) []map[string]any {
	trunc := grainTrunc[grain]
	if trunc == nil {
		trunc = grainTrunc["minute"]
	}
	type acc struct {
		bucket   time.Time
		count    int64
		okCount  int64
		sum      int64
		p50, p95 int64
	}
	groups := map[int64]acc{}
	var keys []int64
	for _, r := range rollups {
		b := trunc(r.Bucket.UTC())
		key := b.Unix()
		a, ok := groups[key]
		if !ok {
			a.bucket = b
			keys = append(keys, key)
		}
		a.count += r.Count
		a.okCount += r.OKCount
		a.sum += r.SumLatencyMS
		if r.P50LatencyMS > a.p50 {
			a.p50 = r.P50LatencyMS
		}
		if r.P95LatencyMS > a.p95 {
			a.p95 = r.P95LatencyMS
		}
		groups[key] = a
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	out := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		a := groups[k]
		uptime := 0.0
		if a.count > 0 {
			uptime = float64(a.okCount) / float64(a.count) * 100
		}
		avg := int64(0)
		if a.count > 0 {
			avg = int64(float64(a.sum) / float64(a.count))
		}
		out = append(out, map[string]any{
			"bucket":     a.bucket,
			"count":      a.count,
			"ok_count":   a.okCount,
			"avg_ms":     avg,
			"p50_ms":     a.p50,
			"p95_ms":     a.p95,
			"uptime_pct": round2(uptime),
		})
	}
	return out
}

// statsSeries devolve a série de latência/uptime a partir dos rollups
// pré-computados (RNF-014 — nunca logs brutos).
func (s *Server) statsSeries(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	epID, err := strconv.ParseInt(q.Get("endpoint_id"), 10, 64)
	if err != nil || epID <= 0 {
		writeErr(w, http.StatusBadRequest, "endpoint_id obrigatório")
		return
	}
	from, err := parseTimeParam(r, "from")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "from deve ser RFC3339")
		return
	}
	to, err := parseTimeParam(r, "to")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "to deve ser RFC3339")
		return
	}
	limit := 1440 // 1.440 pontos/dia — teto do gráfico (RF-020, RNF-014)
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 1440 {
			writeErr(w, http.StatusBadRequest, "limit deve estar entre 1 e 1440")
			return
		}
		limit = n
	}
	items, err := s.seriesResponse(r.Context(), epID, from, to, limit, grainOf(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "falha ao ler rollups")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "limit": limit})
}

// grainOf lê o parâmetro ?bucket=minute|hour|day (inválido → minute).
func grainOf(r *http.Request) string {
	g := r.URL.Query().Get("bucket")
	switch g {
	case "hour", "day":
		return g
	default:
		return "minute"
	}
}

// summaryResponse resume uptime (dia/semana/mês) e percentis atuais — RF-020.
func (s *Server) summaryResponse(ctx context.Context, epID int64) (map[string]any, error) {
	now := time.Now().UTC()
	rollups, err := s.store.ListRollups(ctx, epID, time.Time{}, now.Add(time.Hour), 0)
	if err != nil {
		return nil, err
	}
	day := aggregateSince(rollups, now.Add(-24*time.Hour))
	week := aggregateSince(rollups, now.Add(-7*24*time.Hour))
	month := aggregateSince(rollups, now.Add(-30*24*time.Hour))
	return map[string]any{
		"endpoint_id": epID,
		"uptime": map[string]float64{
			"day":   uptimePct(day),
			"week":  uptimePct(week),
			"month": uptimePct(month),
		},
		"latency_ms": map[string]int64{
			"p50": day.p50,
			"p95": day.p95,
			"avg": day.avg(),
		},
	}, nil
}

// statsSummary resume uptime (dia/semana/mês) e percentis atuais — RF-020.
func (s *Server) statsSummary(w http.ResponseWriter, r *http.Request) {
	epID, err := strconv.ParseInt(r.URL.Query().Get("endpoint_id"), 10, 64)
	if err != nil || epID <= 0 {
		writeErr(w, http.StatusBadRequest, "endpoint_id obrigatório")
		return
	}
	out, err := s.summaryResponse(r.Context(), epID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "falha ao ler rollups")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

type rollupAgg struct {
	count, okCount int64
	sum            int64
	p50, p95       int64
}

func (a rollupAgg) avg() int64 {
	if a.count == 0 {
		return 0
	}
	return int64(float64(a.sum) / float64(a.count))
}

// aggregateSince soma rollups a partir de since.
func aggregateSince(rollups []domain.Rollup, since time.Time) rollupAgg {
	var a rollupAgg
	for _, r := range rollups {
		if r.Bucket.Before(since) {
			continue
		}
		a.count += r.Count
		a.okCount += r.OKCount
		a.sum += r.SumLatencyMS
		if r.P50LatencyMS > a.p50 {
			a.p50 = r.P50LatencyMS
		}
		if r.P95LatencyMS > a.p95 {
			a.p95 = r.P95LatencyMS
		}
	}
	return a
}

func uptimePct(a rollupAgg) float64 {
	if a.count == 0 {
		return 0
	}
	return round2(float64(a.okCount) / float64(a.count) * 100)
}

func round2(f float64) float64 {
	return float64(int(f*100+0.5)) / 100
}

// listNotifications devolve o registro de auditoria de alertas (RF-029).
func (s *Server) listNotifications(w http.ResponseWriter, r *http.Request) {
	var epID int64
	if v := r.URL.Query().Get("endpoint_id"); v != "" {
		var err error
		epID, err = strconv.ParseInt(v, 10, 64)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "endpoint_id inválido")
			return
		}
	}
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 500 {
			writeErr(w, http.StatusBadRequest, "limit deve estar entre 1 e 500")
			return
		}
		limit = n
	}
	notifs, err := s.store.ListNotifications(r.Context(), epID, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "falha ao listar notificações")
		return
	}
	out := make([]map[string]any, 0, len(notifs))
	for _, n := range notifs {
		out = append(out, map[string]any{
			"id":           n.ID,
			"endpoint_id":  n.EndpointID,
			"incident_id":  n.IncidentID,
			"channel":      n.Channel,
			"payload":      n.Payload,
			"delivered_at": n.DeliveredAt,
			"status":       n.Status,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}
