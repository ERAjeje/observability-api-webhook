package notifier

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"monitor/internal/broker"
	"monitor/internal/domain"
	"monitor/internal/storage"
)

func testNotifier(t *testing.T, cfg Config) (*Notifier, *storage.MemStore, *broker.Broker) {
	t.Helper()
	store := storage.NewMem()
	br := broker.New()
	store.CreateEndpoint(context.Background(), domain.Endpoint{
		ID: 100, Name: "api-gw", URL: "https://example.com", Method: "GET",
		Interval: time.Minute, Timeout: 2 * time.Second, Active: true, Status: domain.StatusUp,
	})
	if cfg.Suppression <= 0 {
		cfg.Suppression = 5 * time.Minute
	}
	cfg.Enabled = true
	cfg.AllowPrivate = true // httptest roda em loopback (guardado por padrão)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(cfg, store, br, log), store, br
}

// publishDown publica status_changed→DOWN repetidamente até o notifier
// registrar (a assinatura do broker é assíncrona ao Run).
func publishDown(br *broker.Broker, fn func() bool) {
	for i := 0; i < 100 && !fn(); i++ {
		br.Publish(broker.EventStatusChanged, broker.Event{
			Type: broker.EventStatusChanged, Timestamp: time.Now(),
			EndpointID: 100, Name: "api-gw", Status: domain.StatusDown, From: domain.StatusUp,
		})
		time.Sleep(25 * time.Millisecond)
	}
}

// TestNotifier_WebhookEntregaEAudita: transição DOWN dispara webhook e grava
// auditoria com status sent (RF-026/RF-029).
func TestNotifier_WebhookEntregaEAudita(t *testing.T) {
	var calls int32
	payloads := make(chan map[string]any, 4)
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		var p map[string]any
		_ = json.NewDecoder(r.Body).Decode(&p)
		payloads <- p
		w.WriteHeader(http.StatusOK)
	}))
	defer hook.Close()

	n, store, br := testNotifier(t, Config{WebhookURL: hook.URL, Timeout: 2 * time.Second})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go n.Run(ctx)

	publishDown(br, func() bool { return atomic.LoadInt32(&calls) >= 1 })

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("webhook: esperado 1 chamada, got %d", got)
	}
	select {
	case p := <-payloads:
		if p["text"] == nil {
			t.Fatalf("payload sem texto: %v", p)
		}
		inner, _ := p["payload"].(map[string]any)
		if inner["endpoint"] != "api-gw" {
			t.Fatalf("payload inválido: %v", p)
		}
	case <-time.After(time.Second):
		t.Fatal("payload do webhook não recebido")
	}

	// Auditoria registrada.
	deadline := time.Now().Add(2 * time.Second)
	var notifs []domain.Notification
	for time.Now().Before(deadline) {
		notifs, _ = store.ListNotifications(context.Background(), 100, 10)
		if len(notifs) == 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(notifs) != 1 {
		t.Fatalf("auditoria: esperado 1 registro, got %d", len(notifs))
	}
	if notifs[0].Channel != "webhook" || notifs[0].Status != "sent" {
		t.Fatalf("auditoria: %+v", notifs[0])
	}
	if notifs[0].DeliveredAt == nil {
		t.Fatal("delivered_at ausente")
	}
}

// TestNotifier_Supressao: repetição dentro da janela não gera novo envio
// (RF-027/UC-04).
func TestNotifier_Supressao(t *testing.T) {
	var calls int32
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer hook.Close()

	n, store, br := testNotifier(t, Config{
		WebhookURL: hook.URL, Timeout: 2 * time.Second, Suppression: 10 * time.Minute,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go n.Run(ctx)

	// Publica até 1º envio; depois continua publicando (deve ser suprimido).
	publishDown(br, func() bool { return atomic.LoadInt32(&calls) >= 1 })
	for i := 0; i < 3; i++ {
		br.Publish(broker.EventStatusChanged, broker.Event{
			Type: broker.EventStatusChanged, Timestamp: time.Now(),
			EndpointID: 100, Name: "api-gw", Status: domain.StatusDown, From: domain.StatusUp,
		})
		time.Sleep(30 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("supressão: esperado 1 envio, got %d", got)
	}
	notifs, _ := store.ListNotifications(context.Background(), 100, 10)
	if len(notifs) != 1 {
		t.Fatalf("auditoria suprimida? got %d registros", len(notifs))
	}
}

// TestNotifier_RetryEBackoff: webhook com erro retenta (RNF-006) e audita fail.
func TestNotifier_RetryEBackoff(t *testing.T) {
	var calls int32
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusInternalServerError) // sempre falha
	}))
	defer hook.Close()

	n, store, br := testNotifier(t, Config{
		WebhookURL: hook.URL, Timeout: 500 * time.Millisecond, Retries: 3, Suppression: time.Minute,
		AllowPrivate: true,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go n.Run(ctx)

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		br.Publish(broker.EventStatusChanged, broker.Event{
			Type: broker.EventStatusChanged, Timestamp: time.Now(),
			EndpointID: 100, Name: "api-gw", Status: domain.StatusDown, From: domain.StatusUp,
		})
		notifs, _ := store.ListNotifications(context.Background(), 100, 10)
		if len(notifs) == 1 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	notifs, _ := store.ListNotifications(context.Background(), 100, 10)
	if len(notifs) != 1 || notifs[0].Status != "failed" {
		t.Fatalf("esperado 1 auditoria failed, got %+v", notifs)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("retry: esperado 3 tentativas, got %d", got)
	}
}
