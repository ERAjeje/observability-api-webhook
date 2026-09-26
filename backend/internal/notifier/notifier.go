// Package notifier envia alertas em transições de estado por e-mail (SMTP)
// e webhook (Slack/Discord) com supressão, retry/backoff e auditoria
// (RF-025..RF-029, RNF-005..RNF-008).
package notifier

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/smtp"
	"strings"
	"sync"
	"time"

	"monitor/internal/broker"
	"monitor/internal/checker"
	"monitor/internal/domain"
	"monitor/internal/storage"
)

// Config parametriza o notifier.
type Config struct {
	Enabled      bool
	SMTPHost     string
	SMTPPort     int
	SMTPUser     string
	SMTPPass     string
	SMTPFrom     string
	ToEmail      string
	WebhookURL   string
	Suppression  time.Duration // janela anti-enxurrada (RF-027/RNF-007)
	Retries      int           // por canal (RNF-006)
	Timeout      time.Duration // timeout curto por tentativa (RNF-006)
	AllowPrivate bool          // reusa a política anti-SSRF do checker (S-01)
}

// Notifier assina o broker e despacha alertas.
type Notifier struct {
	cfg    Config
	store  storage.Store
	broker *broker.Broker
	log    *slog.Logger
	http   *http.Client

	mu        sync.Mutex
	lastAlert map[int64]time.Time // supressão por endpoint
}

// New constrói o notifier. client nil gera transporte guardado (anti-SSRF).
func New(cfg Config, store storage.Store, br *broker.Broker, log *slog.Logger) *Notifier {
	n := &Notifier{
		cfg:       cfg,
		store:     store,
		broker:    br,
		log:       log,
		lastAlert: map[int64]time.Time{},
	}
	if cfg.Retries <= 0 {
		n.cfg.Retries = 3
	}
	if cfg.Timeout <= 0 {
		n.cfg.Timeout = 5 * time.Second
	}
	n.http = &http.Client{
		Transport: checker.NewGuardedTransport(cfg.AllowPrivate),
		Timeout:   cfg.Timeout,
	}
	return n
}

// Run bloqueia consumindo os eventos de transição até ctx cancel.
func (n *Notifier) Run(ctx context.Context) {
	statusCh, unsubStatus := n.broker.Subscribe(broker.EventStatusChanged)
	defer unsubStatus()
	openedCh, unsubOpened := n.broker.Subscribe(broker.EventIncidentOpened)
	defer unsubOpened()
	closedCh, unsubClosed := n.broker.Subscribe(broker.EventIncidentClosed)
	defer unsubClosed()

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-statusCh:
			if !ok {
				return
			}
			n.handleStatusChanged(ctx, ev)
		case ev, ok := <-openedCh:
			if !ok {
				return
			}
			n.handleIncidentOpened(ctx, ev)
		case ev, ok := <-closedCh:
			if !ok {
				return
			}
			n.handleIncidentClosed(ctx, ev)
		}
	}
}

// ─── Disparos ─────────────────────────────────────────────────────────────

// handleStatusChanged alerta transições para DOWN/DEGRADED (RF-025), com
// janela de supressão (RF-027/UC-04).
func (n *Notifier) handleStatusChanged(ctx context.Context, ev broker.Event) {
	switch ev.Status {
	case domain.StatusDown:
		if !n.allowAlert(ev.EndpointID) {
			return
		}
		n.dispatch(ctx, ev, "down", fmt.Sprintf(
			"🔴 %s está FORA DO AR — status: DOWN", ev.Name))
	case domain.StatusDegraded:
		if !n.allowAlert(ev.EndpointID) {
			return
		}
		n.dispatch(ctx, ev, "degraded", fmt.Sprintf(
			"🟡 %s com latência acima do limiar — status: DEGRADED", ev.Name))
	}
}

// handleIncidentOpened gera o alerta de queda com a 1ª falha confirmada
// (RF-028). O status_changed de DOWN já cobre; aqui garantimos o incident_id.
func (n *Notifier) handleIncidentOpened(ctx context.Context, ev broker.Event) {
	if !n.allowAlert(ev.EndpointID) {
		return
	}
	msg := fmt.Sprintf("🆘 Incidente aberto em %s (incidente #%d — DOWN)", ev.Name, ev.IncidentID)
	n.dispatch(ctx, ev, "incident_opened", msg)
}

// handleIncidentClosed envia o resumo da recuperação (RF-028) — nunca
// suprimido: resolução é informação nova.
func (n *Notifier) handleIncidentClosed(ctx context.Context, ev broker.Event) {
	started := time.Now().Add(-time.Duration(ev.DurationMS) * time.Millisecond)
	if inc, err := n.store.GetIncident(ctx, ev.IncidentID); err == nil && !inc.StartedAt.IsZero() {
		started = inc.StartedAt
	}
	msg := fmt.Sprintf("✅ %s recuperado — incidente #%d encerrado em %d ms (início %s)",
		ev.Name, ev.IncidentID, ev.DurationMS, started.Format(time.RFC3339))
	n.dispatch(ctx, ev, "incident_closed", msg)
}

// allowAlert aplica a janela de supressão por endpoint (RF-027).
func (n *Notifier) allowAlert(endpointID int64) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	now := time.Now()
	last, ok := n.lastAlert[endpointID]
	if ok && now.Sub(last) < n.cfg.Suppression {
		n.log.Debug("notifier: alerta suprimido (janela)", "endpoint", endpointID)
		return false
	}
	n.lastAlert[endpointID] = now
	return true
}

// ─── Canais ───────────────────────────────────────────────────────────────

// dispatch envia por todos os canais configurados em paralelo (RNF-008 —
// falha de um canal não impede o outro) e audita cada tentativa.
func (n *Notifier) dispatch(ctx context.Context, ev broker.Event, eventType, text string) {
	payload := map[string]any{
		"event":       eventType,
		"endpoint_id": ev.EndpointID,
		"endpoint":    ev.Name,
		"status":      ev.Status,
		"incident_id": ev.IncidentID,
		"text":        text,
		"timestamp":   ev.Timestamp,
	}
	var wg sync.WaitGroup
	if n.cfg.SMTPHost != "" && n.cfg.ToEmail != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n.deliverWithAudit(ctx, ev, "email", payload, func(ctx context.Context) error {
				return n.sendEmail(ctx, text)
			})
		}()
	}
	if n.cfg.WebhookURL != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n.deliverWithAudit(ctx, ev, "webhook", payload, func(ctx context.Context) error {
				return n.sendWebhook(ctx, payload)
			})
		}()
	}
	wg.Wait()
}

// deliverWithAudit executa a entrega com retry/backoff e registra em
// notifications (RF-029).
func (n *Notifier) deliverWithAudit(ctx context.Context, ev broker.Event, channel string, payload map[string]any, send func(context.Context) error) {
	var incidentID *int64
	if ev.IncidentID > 0 {
		incidentID = &ev.IncidentID
	}
	pb, _ := json.Marshal(payload)
	err := n.withRetry(channel, func(ctx context.Context) error { return send(ctx) })
	status := "sent"
	var deliveredAt *time.Time
	if err != nil {
		status = "failed"
		n.log.Error("notifier: entrega falhou", "channel", channel, "err", err)
	} else {
		t := time.Now()
		deliveredAt = &t
	}
	if _, err := n.store.CreateNotification(ctx, domain.Notification{
		EndpointID:  ev.EndpointID,
		IncidentID:  incidentID,
		Channel:     channel,
		Payload:     string(pb),
		DeliveredAt: deliveredAt,
		Status:      status,
	}); err != nil {
		n.log.Error("notifier: auditoria falhou", "channel", channel, "err", err)
	}
}

// withRetry aplica até cfg.Retries tentativas com backoff exponencial
// 1s, 2s, 4s (cap 30s) — RNF-006.
func (n *Notifier) withRetry(channel string, fn func(context.Context) error) error {
	var err error
	base := time.Second
	for attempt := 1; attempt <= n.cfg.Retries; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), n.cfg.Timeout)
		err = fn(ctx)
		cancel()
		if err == nil {
			return nil
		}
		if attempt < n.cfg.Retries {
			backoff := base << (attempt - 1)
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
			n.log.Warn("notifier: retry", "channel", channel, "attempt", attempt, "backoff", backoff, "err", err)
			time.Sleep(backoff)
		}
	}
	return err
}

// ─── Webhook (Slack/Discord) ──────────────────────────────────────────────

func (n *Notifier) sendWebhook(ctx context.Context, payload map[string]any) error {
	body, _ := json.Marshal(map[string]any{"text": payload["text"], "payload": payload})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.cfg.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook: status %d", resp.StatusCode)
	}
	return nil
}

// ─── E-mail (SMTP) ─────────────────────────────────────────────────────────

func (n *Notifier) sendEmail(ctx context.Context, text string) error {
	addr := net.JoinHostPort(n.cfg.SMTPHost, fmt.Sprintf("%d", n.cfg.SMTPPort))

	dialer := &net.Dialer{Timeout: n.cfg.Timeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(n.cfg.Timeout))

	client, err := smtp.NewClient(conn, n.cfg.SMTPHost)
	if err != nil {
		return err
	}
	defer client.Close()

	// STARTTLS (porta 587/25). Fail-closed: só autenticamos em canal cifrado —
	// nunca enviamos credenciais em claro.
	tlsOK := false
	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(tlsConfigFor(addr)); err != nil {
			return fmt.Errorf("smtp: starttls: %w", err)
		}
		tlsOK = true
	}
	if n.cfg.SMTPUser != "" {
		if !tlsOK {
			return errors.New("smtp: recusa autenticar sem STARTTLS (fail-closed)")
		}
		auth := smtp.PlainAuth("", n.cfg.SMTPUser, n.cfg.SMTPPass, n.cfg.SMTPHost)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp: auth: %w", err)
		}
	}
	if err := client.Mail(n.cfg.SMTPFrom); err != nil {
		return err
	}
	if err := client.Rcpt(n.cfg.ToEmail); err != nil {
		return err
	}
	wr, err := client.Data()
	if err != nil {
		return err
	}
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: Central de Monitoramento\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s\r\n",
		n.cfg.SMTPFrom, n.cfg.ToEmail, text)
	if _, err := wr.Write([]byte(msg)); err != nil {
		_ = wr.Close()
		return err
	}
	if err := wr.Close(); err != nil {
		return err
	}
	return client.Quit()
}

func tlsConfigFor(addr string) *tls.Config {
	return &tls.Config{ServerName: serverName(addr), MinVersion: tls.VersionTLS12}
}

func serverName(addr string) string {
	if i := strings.LastIndex(addr, ":"); i > 0 {
		return addr[:i]
	}
	return addr
}
