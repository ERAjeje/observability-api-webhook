// Testes de integração do S-08 — cotas por conta na API admin (HTTP 429):
// limite de endpoints, intervalo mínimo e revalidação no update. Verifica
// também o vínculo owner_id e a independência entre contas.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"monitor/internal/auth"
	"monitor/internal/broker"
	"monitor/internal/quota"
	"monitor/internal/settings"
	"monitor/internal/storage"
)

// newQuotaTestServer monta um server com cotas pequenas (S-08).
func newQuotaTestServer(t *testing.T, l quota.Limits) (*Server, *storage.MemStore) {
	t.Helper()
	store := storage.NewMem()
	authSvc, err := auth.New(auth.Config{Secret: "test-secret-0123456789abcdef", TTL: time.Hour, Issuer: "test"})
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := settings.New(store, log, true, settings.DefaultAlerts("", "", 5*time.Minute))
	srv := New(Config{
		Auth:         authSvc,
		Broker:       broker.New(),
		Settings:     svc,
		Quota:        quota.New(l, store),
		SSEHeartbeat: time.Minute,
		AllowPrivate: true,
		AuthRateMax:  100,
		AuthRateWin:  time.Minute,
	}, store, &fakeHooks{})
	return srv, store
}

func createQEndpoint(t *testing.T, srv *Server, token, url string, intervalSec, wantCode int) string {
	t.Helper()
	body := fmt.Sprintf(`{"name":"q-ep","url":%q,"method":"GET","interval_seconds":%d,"timeout_ms":2000}`, url, intervalSec)
	rec := do(srv, http.MethodPost, "/api/v1/admin/endpoints/", body, token)
	if rec.Code != wantCode {
		t.Fatalf("create: got %d, want %d (%s)", rec.Code, wantCode, rec.Body.String())
	}
	return rec.Body.String()
}

func TestS08_LimiteEndpointsPorConta(t *testing.T) {
	srv, store := newQuotaTestServer(t, quota.Limits{
		MaxEndpointsPerAccount: 2,
		MinInterval:            60 * time.Second,
	})
	token := signupAndLogin(t, srv)
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer sink.Close()

	createQEndpoint(t, srv, token, sink.URL, 60, http.StatusCreated)
	createQEndpoint(t, srv, token, sink.URL, 60, http.StatusCreated)

	// 3º endpoint da MESMA conta → 429 com mensagem amigável.
	body := createQEndpoint(t, srv, token, sink.URL, 60, http.StatusTooManyRequests)
	if !strings.Contains(body, "limite de 2 endpoints") {
		t.Fatalf("mensagem de 429 inesperada: %s", body)
	}
	if _, err := store.GetEndpoint(context.Background(), 3); err == nil {
		t.Fatal("nada além do limite deveria ter sido persistido")
	}
}

func TestS08_IntervaloMinimo429(t *testing.T) {
	srv, _ := newQuotaTestServer(t, quota.Limits{
		MaxEndpointsPerAccount: 10,
		MinInterval:            60 * time.Second,
	})
	token := signupAndLogin(t, srv)
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer sink.Close()

	// 30s < mínimo de 60s → 429 (cota, não validação).
	body := createQEndpoint(t, srv, token, sink.URL, 30, http.StatusTooManyRequests)
	if !strings.Contains(body, "intervalo mínimo por conta") {
		t.Fatalf("mensagem inesperada: %s", body)
	}
	// 60s = mínimo → OK.
	createQEndpoint(t, srv, token, sink.URL, 60, http.StatusCreated)
}

func TestS08_OwnerVinculado_E_ContasIndependentes(t *testing.T) {
	srv, store := newQuotaTestServer(t, quota.Limits{
		MaxEndpointsPerAccount: 1,
		MinInterval:            30 * time.Second,
	})
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer sink.Close()

	ta := signupAndLogin(t, srv)
	tb := signupAndLoginWith(t, srv, "b@test.local")

	createQEndpoint(t, srv, ta, sink.URL, 60, http.StatusCreated)
	// Conta A estourou; conta B ainda cabe (independência).
	createQEndpoint(t, srv, ta, sink.URL, 60, http.StatusTooManyRequests)
	createQEndpoint(t, srv, tb, sink.URL, 60, http.StatusCreated)

	eps, err := store.ListEndpoints(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(eps) != 2 || eps[0].OwnerID == eps[1].OwnerID {
		t.Fatalf("owners esperados distintos, got %+v", eps[0].OwnerID)
	}
	// DTO expõe owner_id (acuidade operacional).
	rec := do(srv, http.MethodGet, "/api/v1/admin/endpoints/", "", ta)
	if !strings.Contains(rec.Body.String(), `"owner_id"`) {
		t.Fatal("DTO admin deveria expor owner_id")
	}
}

func TestS08_UpdateRevalidaCotas(t *testing.T) {
	srv, _ := newQuotaTestServer(t, quota.Limits{
		MaxEndpointsPerAccount: 10,
		MinInterval:            60 * time.Second,
	})
	token := signupAndLogin(t, srv)
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer sink.Close()

	resp := createQEndpoint(t, srv, token, sink.URL, 120, http.StatusCreated)
	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal([]byte(resp), &created); err != nil {
		t.Fatal(err)
	}

	// PUT reduzindo o intervalo abaixo do mínimo → 429.
	upd := fmt.Sprintf(`{"name":"q-ep","url":%q,"method":"GET","interval_seconds":30,"timeout_ms":2000}`, sink.URL)
	rec := do(srv, http.MethodPut, fmt.Sprintf("/api/v1/admin/endpoints/%d", created.ID), upd, token)
	if rec.Code != http.StatusTooManyRequests || !strings.Contains(rec.Body.String(), "intervalo mínimo") {
		t.Fatalf("update: esperava 429 interval_min, got %d %s", rec.Code, rec.Body.String())
	}
	// PUT com intervalo válido → 200 e persistido.
	upd = fmt.Sprintf(`{"name":"q-ep","url":%q,"method":"GET","interval_seconds":120,"timeout_ms":2000}`, sink.URL)
	rec = do(srv, http.MethodPut, fmt.Sprintf("/api/v1/admin/endpoints/%d", created.ID), upd, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("update válido: %d %s", rec.Code, rec.Body.String())
	}
	// 404 antecipado no update de endpoint inexistente.
	rec = do(srv, http.MethodPut, "/api/v1/admin/endpoints/999", upd, token)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("update inexistente: esperava 404, got %d", rec.Code)
	}
}

// signupAndLoginWith cria+loga uma conta com e-mail custom (várias contas no
// mesmo server de teste).
func signupAndLoginWith(t *testing.T, srv *Server, email string) string {
	t.Helper()
	rec := do(srv, http.MethodPost, "/api/v1/auth/signup",
		fmt.Sprintf(`{"email":%q,"password":"senha-segura-123"}`, email), "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("signup %s: %d %s", email, rec.Code, rec.Body.String())
	}
	rec = do(srv, http.MethodPost, "/api/v1/auth/login",
		fmt.Sprintf(`{"email":%q,"password":"senha-segura-123"}`, email), "")
	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Token == "" {
		t.Fatalf("login %s: %v %s", email, err, rec.Body.String())
	}
	return out.Token
}
