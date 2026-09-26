// Testes de integração do S-05 — headers cifrados em repouso e o fluxo
// completo engine (decifra antes da checagem; fail-closed sem chave).
package engine

import (
	"context"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"monitor/internal/domain"
	"monitor/internal/seal"
	"monitor/internal/storage"
	"monitor/internal/worker"
)

func mustSealKey(t *testing.T) []byte {
	t.Helper()
	k, err := seal.ParseKey(hex.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestEngine_HeadersCifradosUsadosNaChecagem(t *testing.T) {
	key := mustSealKey(t)

	var mu sync.Mutex
	var gotAuth string
	echo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotAuth = r.Header.Get("Authorization")
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer echo.Close()

	store := storage.NewMem()
	cfg := testCfg()
	cfg.HeadersSecret = key
	eng, err := New(context.Background(), cfg, store, testLog())
	if err != nil {
		t.Fatal(err)
	}

	// Endpoint com headers CIFRADOS no store — exatamente como a API persiste
	// com HEADERS_ENC_KEY configurada (S-05).
	ep := domain.Endpoint{
		Name: "secreto", URL: echo.URL, Method: "GET",
		Headers:  map[string]string{"Authorization": "Bearer segredo-123"},
		Interval: time.Minute, Timeout: 2 * time.Second, Active: true,
	}
	if _, err := seal.EncryptEndpointHeaders(&ep, key); err != nil {
		t.Fatal(err)
	}
	id := mustCreateEndpoint(t, store, ep)

	stored, err := store.GetEndpoint(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if !seal.IsSealed(stored.Headers) {
		t.Fatal("store deveria conter envelope cifrado")
	}

	if err := eng.handleJob(context.Background(), worker.Job{Endpoint: stored, Due: time.Now()}); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if gotAuth != "Bearer segredo-123" {
		t.Fatalf("checker recebeu Authorization=%q (deveria estar decifrado)", gotAuth)
	}
}

func TestEngine_HeadersCifradosFailClosedSemChave(t *testing.T) {
	key := mustSealKey(t)

	var mu sync.Mutex
	var gotAuth string
	echo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotAuth = r.Header.Get("Authorization")
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer echo.Close()

	store := storage.NewMem()
	// engine SEM a chave (operador removou HEADERS_ENC_KEY do env).
	eng, err := New(context.Background(), testCfg(), store, testLog())
	if err != nil {
		t.Fatal(err)
	}

	ep := domain.Endpoint{
		Name: "secreto", URL: echo.URL, Method: "GET",
		Headers:  map[string]string{"Authorization": "Bearer segredo-xyz"},
		Interval: time.Minute, Timeout: 2 * time.Second, Active: true,
	}
	if _, err := seal.EncryptEndpointHeaders(&ep, key); err != nil {
		t.Fatal(err)
	}
	id := mustCreateEndpoint(t, store, ep)
	stored, _ := store.GetEndpoint(context.Background(), id)

	if err := eng.handleJob(context.Background(), worker.Job{Endpoint: stored, Due: time.Now()}); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if gotAuth != "" {
		t.Fatalf("fail-closed: header vazou para a rede (Authorization=%q)", gotAuth)
	}
}
