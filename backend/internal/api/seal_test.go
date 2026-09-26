// Testes do S-05 na camada de API: headers cifrados em repouso (jsonb) e
// descriptografia apenas no admin autenticado; pública nunca vaza (RNF-018).
package api

import (
	"context"
	"encoding/hex"
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
	"monitor/internal/seal"
	"monitor/internal/settings"
	"monitor/internal/storage"
)

// newSealedTestServer: server com HEADERS_ENC_KEY ativa (S-05) e
// AllowPrivate para aceitar o httptest de loopback.
func newSealedTestServer(t *testing.T) (*Server, *storage.MemStore) {
	t.Helper()
	store := storage.NewMem()
	authSvc, err := auth.New(auth.Config{Secret: "test-secret-0123456789abcdef", TTL: time.Hour, Issuer: "test"})
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := settings.New(store, log, true, settings.DefaultAlerts("", "", 5*time.Minute))
	key, err := seal.ParseKey(hex.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	srv := New(Config{
		Auth:          authSvc,
		Broker:        broker.New(),
		Settings:      svc,
		HeadersSecret: key,
		SSEHeartbeat:  time.Minute,
		AllowPrivate:  true,
		AuthRateMax:   100,
		AuthRateWin:   time.Minute,
	}, store, &fakeHooks{})
	return srv, store
}

func createEndpointJSON(t *testing.T, srv *Server, token, url, secret string) int64 {
	t.Helper()
	body := fmt.Sprintf(`{"name":"api-secreto","url":%q,"method":"GET","headers":{"Authorization":%q},"interval_seconds":60,"timeout_ms":2000}`, url, secret)
	rec := do(srv, http.MethodPost, "/api/v1/admin/endpoints/", body, token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	return created.ID
}

func TestS05_HeadersCifradosEmRepouso(t *testing.T) {
	srv, store := newSealedTestServer(t)
	token := signupAndLogin(t, srv)
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer sink.Close()

	const secret = "Bearer segredo-123"
	id := createEndpointJSON(t, srv, token, sink.URL, secret)

	// Store: NADA de texto plano, apenas o envelope cifrado.
	stored, err := store.GetEndpoint(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if !seal.IsSealed(stored.Headers) {
		t.Fatal("jsonb deveria conter o envelope cifrado")
	}
	if b, _ := json.Marshal(stored.Headers); strings.Contains(string(b), secret) {
		t.Fatalf("segredo vazou no jsonb: %s", b)
	}

	// Admin autenticado recebe decifrado (list e get).
	rec := do(srv, http.MethodGet, fmt.Sprintf("/api/v1/admin/endpoints/%d", id), "", token)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), secret) {
		t.Fatalf("get admin deveria devolver headers decifrados: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(srv, http.MethodGet, "/api/v1/admin/endpoints/", "", token)
	if !strings.Contains(rec.Body.String(), secret) {
		t.Fatal("list admin sem headers decifrados")
	}

	// Público NUNCA vaza (RNF-018): /status não tem sequer o campo headers.
	rec = do(srv, http.MethodGet, "/api/v1/status", "", "")
	if strings.Contains(rec.Body.String(), secret) {
		t.Fatal("status público vazou o segredo")
	}
	// Update preserva a cifragem (escreve envelope, admin lê decifrado).
	upd := fmt.Sprintf(`{"name":"api-secreto2","url":%q,"method":"GET","headers":{"Authorization":%q},"interval_seconds":60,"timeout_ms":2000}`, sink.URL, "Bearer novo-456")
	rec = do(srv, http.MethodPut, fmt.Sprintf("/api/v1/admin/endpoints/%d", id), upd, token)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Bearer novo-456") {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	stored2, _ := store.GetEndpoint(context.Background(), id)
	if !seal.IsSealed(stored2.Headers) {
		t.Fatal("update deveria persistir envelope cifrado")
	}
}

func TestS05_SemChaveContinuaTextoPlano(t *testing.T) {
	// Backward compatibility: sem HEADERS_ENC_KEY o fluxo antigo persiste.
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
		SSEHeartbeat: time.Minute,
		AllowPrivate: true,
		AuthRateMax:  100,
		AuthRateWin:  time.Minute,
	}, store, &fakeHooks{})
	token := signupAndLogin(t, srv)
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer sink.Close()

	id := createEndpointJSON(t, srv, token, sink.URL, "Bearer plaintext-dev")
	stored, err := store.GetEndpoint(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if seal.IsSealed(stored.Headers) {
		t.Fatal("sem chave deveria persistir texto plano (modo dev)")
	}
	if stored.Headers["Authorization"] != "Bearer plaintext-dev" {
		t.Fatalf("headers não preservados: %v", stored.Headers)
	}
}
