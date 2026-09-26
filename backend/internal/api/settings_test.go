package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"monitor/internal/auth"
	"monitor/internal/broker"
	"monitor/internal/settings"
	"monitor/internal/storage"
)

// newSettingsTestServer cria um server com settings.Service ativo.
func newSettingsTestServer(t *testing.T) (*Server, *storage.MemStore) {
	t.Helper()
	store := storage.NewMem()
	authSvc, err := auth.New(auth.Config{Secret: "test-secret-0123456789abcdef", TTL: time.Hour, Issuer: "test"})
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := settings.New(store, log, false, settings.DefaultAlerts("", "", 5*time.Minute))
	srv := New(Config{
		Auth:         authSvc,
		Broker:       broker.New(),
		Settings:     svc,
		SSEHeartbeat: time.Minute,
		AllowPrivate: false,
		AuthRateMax:  100,
		AuthRateWin:  time.Minute,
	}, store, &fakeHooks{})
	return srv, store
}

// TestSettings_PublicConfigCompatBranding: /api/v1/config é público e expõe
// somente branding (RF-023), sem nada sensível.
func TestSettings_PublicConfigCompatBranding(t *testing.T) {
	srv, _ := newSettingsTestServer(t)
	rec := do(srv, http.MethodGet, "/api/v1/config", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("config: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Branding settings.Branding `json:"branding"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("json: %v", err)
	}
	if out.Branding.Title == "" || !strings.HasPrefix(out.Branding.PrimaryColor, "#") {
		t.Fatalf("branding default inválido: %+v", out.Branding)
	}
	// O público não pode conter chaves de alerta (RNF-018).
	for _, leaked := range []string{`"alerts"`, `"webhook_url"`, `"to_email"`, `"smtp"`} {
		if strings.Contains(rec.Body.String(), leaked) {
			t.Fatalf("config público vazou %s: %s", leaked, rec.Body.String())
		}
	}
}

// TestSettings_PutPersisteEGetRetorna: PUT branding+alerts veja aparição no
// GET (admin), e o público /config reflete o branding salvo.
func TestSettings_PutPersisteEGetRetorna(t *testing.T) {
	srv, _ := newSettingsTestServer(t)
	token := signupAndLogin(t, srv)

	rec := do(srv, http.MethodPut, "/api/v1/admin/settings", `{
		"branding":{"title":"Status da Empresa","description":"API em tempo real","primary_color":"#0ea5e9"},
		"alerts":{"enabled":true,"webhook_url":"https://example.com/webhook","to_email":"ops@ex.com","suppression_seconds":600}
	}`, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("put: %d %s", rec.Code, rec.Body.String())
	}

	// GET admin reflete.
	rec = do(srv, http.MethodGet, "/api/v1/admin/settings", "", token)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Status da Empresa") {
		t.Fatalf("get admin: %d %s", rec.Code, rec.Body.String())
	}

	// Público reflete branding (título/cor), nunca webhook.
	rec = do(srv, http.MethodGet, "/api/v1/config", "", "")
	if !strings.Contains(rec.Body.String(), "Status da Empresa") ||
		strings.Contains(rec.Body.String(), "example.com/webhook") {
		t.Fatalf("config público errado: %s", rec.Body.String())
	}
}

// TestSettings_ValidaSeguranca: sem token → 401; webhook interno bloqueado
// (anti-SSRF, S-01) e cor inválida rejeitada.
func TestSettings_ValidaSeguranca(t *testing.T) {
	srv, _ := newSettingsTestServer(t)
	token := signupAndLogin(t, srv)

	if rec := do(srv, http.MethodGet, "/api/v1/admin/settings", "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("sem token: %d", rec.Code)
	}

	cases := []struct {
		name string
		body string
	}{
		{"webhook interno", `{"alerts":{"webhook_url":"http://169.254.169.254/meta","enabled":true}}`},
		{"webhook loopback", `{"alerts":{"webhook_url":"http://127.0.0.1:9000/hook","enabled":true}}`},
		{"cor inválida", `{"branding":{"primary_color":"blue"}}`},
		{"email inválido", `{"alerts":{"to_email":"ops@","enabled":true}}`},
		{"suppression alto", `{"alerts":{"suppression_seconds":999999}}`},
	}
	for _, tc := range cases {
		rec := do(srv, http.MethodPut, "/api/v1/admin/settings", tc.body, token)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: esperado 400, got %d %s", tc.name, rec.Code, rec.Body.String())
		}
	}
}

// TestSettings_MemStorePersiste garante que a store de memória mantém os
// valores entre calls.
func TestSettings_MemStorePersiste(t *testing.T) {
	store := storage.NewMem()
	ctx := context.Background()
	if _, err := store.GetSetting(ctx, "branding"); err == nil {
		t.Fatal("esperado ErrNotFound")
	}
	raw := json.RawMessage(`{"title":"X"}`)
	if err := store.SetSetting(ctx, "branding", raw); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetSetting(ctx, "branding")
	if err != nil || string(got) != string(raw) {
		t.Fatalf("roundtrip falhou: %s %v", got, err)
	}
	// Upsert (mesma chave) não explode.
	if err := store.SetSetting(ctx, "branding", json.RawMessage(`{"title":"Y"}`)); err != nil {
		t.Fatal(err)
	}
}
