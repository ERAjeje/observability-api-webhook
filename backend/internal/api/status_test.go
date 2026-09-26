package api

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"monitor/internal/auth"
	"monitor/internal/broker"
	"monitor/internal/domain"
	"monitor/internal/storage"
)

// setupStatusServer cria store com endpoints e server com broker.
func setupStatusServer(t *testing.T) (*Server, *storage.MemStore, *broker.Broker) {
	t.Helper()
	store := storage.NewMem()
	br := broker.New()
	store.CreateGroup(context.Background(), domain.CheckGroup{Name: "Core", DisplayOrder: 1})
	store.CreateEndpoint(context.Background(), domain.Endpoint{
		Name: "api-gw", URL: "https://example.com/health", Method: "GET",
		Interval: time.Minute, Timeout: 2 * time.Second, Active: true, Status: domain.StatusUp,
	})
	authSvc, err := auth.New(auth.Config{Secret: "test-secret-0123456789abcdef", TTL: time.Hour, Issuer: "test"})
	if err != nil {
		t.Fatal(err)
	}
	srv := New(Config{Auth: authSvc, Broker: br, SSEHeartbeat: 50 * time.Millisecond, AllowPrivate: true,
		AuthRateMax: 100, AuthRateWin: time.Minute}, store, &fakeHooks{})
	return srv, store, br
}

// TestPublicStatus_NaoVazaSegredos: a resposta pública NÃO contém URL/body.
func TestPublicStatus_NaoVazaSegredos(t *testing.T) {
	srv, _, _ := setupStatusServer(t)
	rec := do(srv, http.MethodGet, "/api/v1/status", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "https://example.com") {
		t.Fatal("status público vazou URL do endpoint (RNF-018)")
	}
	if !strings.Contains(body, `"status":"up"`) {
		t.Fatalf("status ausente: %s", body)
	}
	if !strings.Contains(body, "api-gw") {
		t.Fatalf("nome ausente: %s", body)
	}
}

// TestSSE_SnapshotComEventoHeartbeat valida o contrato do stream (T3.5):
// snapshot no connect + evento publicado + heartbeat + sem dados sensíveis.
func TestSSE_SnapshotComEventoHeartbeat(t *testing.T) {
	srv, _, br := setupStatusServer(t)

	// Servidor HTTP real (httptest.NewRecorder não é thread-safe para o stream).
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/v1/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("content-type: %s", resp.Header.Get("Content-Type"))
	}

	// Publica o evento real do engine em loop até aparecer no stream.
	go func() {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			br.Publish(broker.EventStatusChanged, broker.Event{
				Type: broker.EventStatusChanged, Timestamp: time.Now(),
				EndpointID: 1, Name: "api-gw", Status: domain.StatusDown, From: domain.StatusUp,
			})
			time.Sleep(50 * time.Millisecond)
		}
	}()

	// Lê o stream linha a linha procurando os marcadores esperados.
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	var snapshotSeen, eventSeen, pingSeen bool
	var payloadChecks int
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: snapshot"):
			snapshotSeen = true
		case strings.HasPrefix(line, "event: status_changed"):
			eventSeen = true
		case line == ": ping" || line == ":ping":
			pingSeen = true
		case strings.HasPrefix(line, "data: "):
			var m map[string]any
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &m); err != nil {
				t.Fatalf("data inválido: %v", err)
			}
			payloadChecks++
			for _, k := range []string{"url", "headers", "body", "password"} {
				if _, has := m[k]; has {
					t.Fatalf("campo sensível %q no evento", k)
				}
			}
		}
		if snapshotSeen && eventSeen && pingSeen {
			break
		}
	}
	if err := sc.Err(); err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, context.Canceled) {
		t.Fatalf("leitura: %v", err)
	}
	if !snapshotSeen {
		t.Fatal("snapshot ausente")
	}
	if !eventSeen {
		t.Fatal("evento status_changed ausente")
	}
	if !pingSeen {
		t.Fatal("heartbeat ausente")
	}
	if payloadChecks == 0 {
		t.Fatal("nenhum data válido lido")
	}
}

// TestPublicIncidents_ListaTimeline: incidentes abertos e fechados.
func TestPublicIncidents_ListaTimeline(t *testing.T) {
	srv, store, _ := setupStatusServer(t)
	now := time.Now().UTC()
	store.CreateIncident(context.Background(), domain.Incident{EndpointID: 1, StartedAt: now.Add(-time.Hour), Resolution: "auto"})
	closedID, _ := store.CreateIncident(context.Background(), domain.Incident{EndpointID: 1, StartedAt: now.Add(-2 * time.Hour), Resolution: "auto"})
	store.CloseIncident(context.Background(), closedID, now.Add(-time.Hour))

	rec := do(srv, http.MethodGet, "/api/v1/incidents", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("incidents: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("json: %v", err)
	}
	if len(out.Items) != 2 {
		t.Fatalf("esperado 2 incidentes, got %d", len(out.Items))
	}
}

// TestStats_SeriesUsaRollups valida a série a partir de rollups (RNF-014).
func TestStats_SeriesUsaRollups(t *testing.T) {
	srv, store, _ := setupStatusServer(t)
	store.UpsertRollup(context.Background(), domain.Rollup{
		EndpointID: 1, Bucket: time.Now().UTC().Truncate(time.Minute),
		Count: 10, OKCount: 9, SumLatencyMS: 5000, P50LatencyMS: 400, P95LatencyMS: 900,
	})

	// Sem token → 401.
	if rec := do(srv, http.MethodGet, "/api/v1/admin/stats/series?endpoint_id=1", "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("stats sem token: %d", rec.Code)
	}

	token := signupAndLogin(t, srv)
	rec := do(srv, http.MethodGet, "/api/v1/admin/stats/series?endpoint_id=1", "", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("series: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"p95_ms":900`) || !strings.Contains(rec.Body.String(), `"p50_ms":400`) {
		t.Fatalf("percentis ausentes: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"uptime_pct":90`) {
		t.Fatalf("uptime ausente: %s", rec.Body.String())
	}
}

// TestAdmin_ChecksPaginados valida a listagem filtrada de logs (RF-018).
func TestAdmin_ChecksPaginados(t *testing.T) {
	srv, store, _ := setupStatusServer(t)
	token := signupAndLogin(t, srv)
	store.AppendChecks(context.Background(), []domain.Check{
		{EndpointID: 1, CheckedAt: time.Now(), Result: domain.ResultOK, LatencyMS: 10},
		{EndpointID: 1, CheckedAt: time.Now().Add(-time.Minute), Result: domain.ResultFail, LatencyMS: 20},
	})
	rec := do(srv, http.MethodGet, "/api/v1/admin/checks?status=fail", "", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("checks: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"result":"fail"`) || strings.Contains(rec.Body.String(), `"result":"ok"`) {
		t.Fatalf("filtro status falhou: %s", rec.Body.String())
	}
}
