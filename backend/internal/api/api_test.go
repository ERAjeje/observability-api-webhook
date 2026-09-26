package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"monitor/internal/auth"
	"monitor/internal/broker"
	"monitor/internal/domain"
	"monitor/internal/storage"
)

// fakeHooks é um EngineHooks de teste que registra sincronizações.
type fakeHooks struct {
	synced  []int64
	dropped []int64
}

func (f *fakeHooks) SyncEndpoint(_ context.Context, ep domain.Endpoint) error {
	f.synced = append(f.synced, ep.ID)
	return nil
}
func (f *fakeHooks) DropEndpoint(id int64) { f.dropped = append(f.dropped, id) }

func newTestServer(t *testing.T) (*Server, *storage.MemStore) {
	t.Helper()
	store := storage.NewMem()
	authSvc, err := auth.New(auth.Config{Secret: "test-secret-0123456789abcdef", TTL: time.Hour, Issuer: "test"})
	if err != nil {
		t.Fatal(err)
	}
	srv := New(Config{
		Auth:         authSvc,
		Broker:       broker.New(),
		SSEHeartbeat: 100 * time.Millisecond,
		AllowPrivate: true,
		AuthRateMax:  100,
		AuthRateWin:  time.Minute,
	}, store, &fakeHooks{})
	return srv, store
}

// do executa uma request contra o servidor de teste.
func do(srv *Server, method, path, body, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func signupAndLogin(t *testing.T, srv *Server) string {
	t.Helper()
	if rec := do(srv, http.MethodPost, "/api/v1/auth/signup", `{"email":"admin@ex.com","password":"senha-segura-123"}`, ""); rec.Code != http.StatusCreated {
		t.Fatalf("signup: status %d: %s", rec.Code, rec.Body.String())
	}
	rec := do(srv, http.MethodPost, "/api/v1/auth/login", `{"email":"admin@ex.com","password":"senha-segura-123"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login: status %d: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Token == "" {
		t.Fatalf("login: token ausente %v %s", err, rec.Body.String())
	}
	return out.Token
}

// ─── Auth (T3.1) ──────────────────────────────────────────────────────────

func TestAuth_FluxoCompleto(t *testing.T) {
	srv, _ := newTestServer(t)

	// Signup.
	if rec := do(srv, http.MethodPost, "/api/v1/auth/signup", `{"email":"Admin@Ex.com","password":"senha-segura-123"}`, ""); rec.Code != http.StatusCreated {
		t.Fatalf("signup: %d %s", rec.Code, rec.Body.String())
	}
	// Login case-insensitive (e-mail normalizado).
	rec := do(srv, http.MethodPost, "/api/v1/auth/login", `{"email":"admin@ex.com","password":"senha-segura-123"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login: %d %s", rec.Code, rec.Body.String())
	}
	// Senha errada → 401 genérico.
	if rec := do(srv, http.MethodPost, "/api/v1/auth/login", `{"email":"admin@ex.com","password":"errada"}`, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("login errado: %d", rec.Code)
	}
	// Email inexistente → 401 genérico (não enumera contas — RNF-019).
	if rec := do(srv, http.MethodPost, "/api/v1/auth/login", `{"email":"ghost@ex.com","password":"x"}`, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("login ghost: %d", rec.Code)
	}
	// Duplicado → 409.
	if rec := do(srv, http.MethodPost, "/api/v1/auth/signup", `{"email":"admin@ex.com","password":"outra-senha-456"}`, ""); rec.Code != http.StatusConflict {
		t.Fatalf("signup duplicado: %d", rec.Code)
	}
	// Senha curta → 400.
	if rec := do(srv, http.MethodPost, "/api/v1/auth/signup", `{"email":"curta@ex.com","password":"123"}`, ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("senha curta: %d", rec.Code)
	}
}

func TestAuth_RotaAdminRequerToken(t *testing.T) {
	srv, _ := newTestServer(t)
	if rec := do(srv, http.MethodGet, "/api/v1/admin/endpoints/", "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("sem token: esperado 401, got %d", rec.Code)
	}
	if rec := do(srv, http.MethodGet, "/api/v1/admin/endpoints/", "", "token.invalido.xx"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("token inválido: esperado 401, got %d", rec.Code)
	}
}

func TestAuth_RateLimitLogin(t *testing.T) {
	srv, _ := newTestServer(t)
	// Limita o limiter para simular brute force (RNF-017).
	srv.authLimiter = NewRateLimiter(3, time.Minute)
	for i := 0; i < 3; i++ {
		if rec := do(srv, http.MethodPost, "/api/v1/auth/login", `{"email":"a@b.com","password":"errada"}`, ""); rec.Code != http.StatusUnauthorized {
			t.Fatalf("try %d: %d", i, rec.Code)
		}
	}
	if rec := do(srv, http.MethodPost, "/api/v1/auth/login", `{"email":"a@b.com","password":"errada"}`, ""); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("esperado 429, got %d", rec.Code)
	}
}

// ─── CRUD (T3.2) ──────────────────────────────────────────────────────────

func TestEndpoints_CRUDComScheduler(t *testing.T) {
	srv, store := newTestServer(t)
	token := signupAndLogin(t, srv)

	// Cria grupo.
	rec := do(srv, http.MethodPost, "/api/v1/admin/groups/", `{"name":"Core","display_order":1}`, token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("grupo: %d %s", rec.Code, rec.Body.String())
	}
	var g struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &g)

	body := `{"group_id":` + itoa(g.ID) + `,"name":"api-gw","url":"https://example.com/health","method":"GET","interval_seconds":60,"timeout_ms":5000}`
	rec = do(srv, http.MethodPost, "/api/v1/admin/endpoints/", body, token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var ep struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &ep)
	if ep.ID == 0 {
		t.Fatal("endpoint id ausente")
	}

	// Scheduler sincronizado (fake hooks) + persistido.
	if got := len(srv.eng.(*fakeHooks).synced); got != 1 {
		t.Fatalf("sync esperado 1, got %d", got)
	}
	if _, err := store.GetEndpoint(context.Background(), ep.ID); err != nil {
		t.Fatalf("persistido? %v", err)
	}

	// Lista.
	rec = do(srv, http.MethodGet, "/api/v1/admin/endpoints/", "", token)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "api-gw") {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}

	// Update (desativa → inativa imediatamente).
	rec = do(srv, http.MethodPut, "/api/v1/admin/endpoints/"+itoa(ep.ID),
		`{"name":"api-gw","url":"https://example.com/health","method":"GET","interval_seconds":30,"active":false}`, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	saved, _ := store.GetEndpoint(context.Background(), ep.ID)
	if saved.Active {
		t.Fatal("esperado active=false")
	}

	// Delete → hook DropEndpoint.
	rec = do(srv, http.MethodDelete, "/api/v1/admin/endpoints/"+itoa(ep.ID), "", token)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", rec.Code)
	}
	if len(srv.eng.(*fakeHooks).dropped) != 1 {
		t.Fatal("drop hook não chamado")
	}
	if _, err := store.GetEndpoint(context.Background(), ep.ID); err == nil {
		t.Fatal("endpoint deveria ter sido removido")
	}
}

func TestEndpoints_RejeitaURLNaoHTTP(t *testing.T) {
	srv, _ := newTestServer(t)
	token := signupAndLogin(t, srv)
	rec := do(srv, http.MethodPost, "/api/v1/admin/endpoints/",
		`{"name":"x","url":"ftp://evil.com/","method":"GET"}`, token)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400 por scheme, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestEndpoints_TesteDeConectividade(t *testing.T) {
	srv, _ := newTestServer(t)
	token := signupAndLogin(t, srv)

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	rec := do(srv, http.MethodPost, "/api/v1/admin/endpoints/test",
		`{"url":"`+backend.URL+`","method":"GET","timeout_ms":2000}`, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("test: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"result":"ok"`) {
		t.Fatalf("esperado result ok: %s", rec.Body.String())
	}
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}
