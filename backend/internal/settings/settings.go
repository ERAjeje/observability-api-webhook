// Package settings guarda as configurações dinâmicas editáveis pelo painel:
// branding da status page (RF-023) e canais de alerta (T4.5 / RF-026-027).
// São chave/valor jsonb no PostgreSQL — defaults em código, persistência no
// store. SMTP_PASS e JWT_SECRET permanecem SOMENTE em env (S-05/S-10).
package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"time"

	"monitor/internal/checker"
	"monitor/internal/storage"
)

// Chaves persistidas.
const (
	KeyBranding = "branding"
	KeyAlerts   = "alerts"
)

// Branding customiza a status page (RF-023).
type Branding struct {
	Title        string `json:"title"`
	Description  string `json:"description"`
	PrimaryColor string `json:"primary_color"`
}

// Alerts é o que o painel configura para os canais de alerta (RF-026/027).
// A conexão SMTP (host/credenciais) continua em env — aqui só o que pode ser
// editado sem expor segredos.
type Alerts struct {
	Enabled            bool   `json:"enabled"`
	WebhookURL         string `json:"webhook_url"`
	ToEmail            string `json:"to_email"`
	SuppressionSeconds int    `json:"suppression_seconds"`
}

// DefaultBranding é a marca padrão da status page.
func DefaultBranding() Branding {
	return Branding{
		Title:        "Central de Monitoramento",
		Description:  "Status em tempo real das APIs e webhooks monitorados.",
		PrimaryColor: "#2563eb",
	}
}

// DefaultAlerts é a configuração padrão (espelha env quando nada salvo).
func DefaultAlerts(webhookURL, toEmail string, suppression time.Duration) Alerts {
	return Alerts{
		Enabled:            false,
		WebhookURL:         webhookURL,
		ToEmail:            toEmail,
		SuppressionSeconds: int(suppression.Seconds()),
	}
}

var colorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// ValidateBranding valida e normaliza o branding (título, descrição, cor).
func ValidateBranding(b Branding) (Branding, error) {
	b.Title = strings.TrimSpace(b.Title)
	b.Description = strings.TrimSpace(b.Description)
	if b.Title == "" {
		b.Title = DefaultBranding().Title
	}
	if len([]rune(b.Title)) > 80 {
		return b, fmt.Errorf("title deve ter no máximo 80 caracteres")
	}
	if len([]rune(b.Description)) > 300 {
		return b, fmt.Errorf("description deve ter no máximo 300 caracteres")
	}
	if b.PrimaryColor == "" {
		b.PrimaryColor = DefaultBranding().PrimaryColor
	}
	if !colorRe.MatchString(b.PrimaryColor) {
		return b, fmt.Errorf("primary_color deve ser HEX (#RRGGBB)")
	}
	return b, nil
}

// ValidateAlerts valida e normaliza os canais de alerta. O webhook segue a
// política anti-SSRF do checker (S-01) já na escrita da config.
func ValidateAlerts(a Alerts, allowPrivate bool) (Alerts, error) {
	a.WebhookURL = strings.TrimSpace(a.WebhookURL)
	a.ToEmail = strings.TrimSpace(strings.ToLower(a.ToEmail))
	if a.WebhookURL != "" {
		u, err := url.Parse(a.WebhookURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return a, fmt.Errorf("webhook_url deve ser http(s) válido")
		}
		if err := checker.ValidateTarget(a.WebhookURL, allowPrivate); err != nil {
			return a, fmt.Errorf("webhook_url: %w", err)
		}
	}
	if a.ToEmail != "" {
		if _, err := mail.ParseAddress(a.ToEmail); err != nil {
			return a, fmt.Errorf("to_email inválido: %v", err)
		}
	}
	if a.SuppressionSeconds < 0 || a.SuppressionSeconds > 86400 {
		return a, fmt.Errorf("suppression_seconds deve estar entre 0 e 86400")
	}
	return a, nil
}

// Service lê/grava settings no store (sem cache — leitura barata e sempre atual).
type Service struct {
	store        storage.Store
	log          *slog.Logger
	allowPrivate bool
	defaults     Alerts // espelha env (NOTIFY_*) como fallback
}

// New constrói o serviço de settings.
func New(store storage.Store, log *slog.Logger, allowPrivate bool, envDefaults Alerts) *Service {
	return &Service{
		store:        store,
		log:          log,
		allowPrivate: allowPrivate,
		defaults:     envDefaults,
	}
}

// Get decodifica uma chave; retorna ErrNotFound se ausente.
func (s *Service) Get(ctx context.Context, key string, dst any) error {
	raw, err := s.store.GetSetting(ctx, key)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, dst)
}

// Set valida e grava uma chave (marshal do valor).
func (s *Service) Set(ctx context.Context, key string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.store.SetSetting(ctx, key, raw)
}

// Branding devolve o branding atual (defaults quando nada salvo).
func (s *Service) Branding(ctx context.Context) (Branding, error) {
	b := DefaultBranding()
	if err := s.Get(ctx, KeyBranding, &b); err != nil && err != storage.ErrNotFound {
		s.log.Warn("settings: falha ao ler branding", "err", err)
	}
	return b, nil
}

// Alerts devolve a config de alertas. O segundo retorno indica se existe uma
// configuração SALVA no store (false = nada salvo → vale o fallback env).
// Semântica do notifier: campos do valor salvo são autoritativos quando salvos;
// Enabled só desliga se salvo como false (env NOTIFY_ENABLED ainda é o gate).
func (s *Service) Alerts(ctx context.Context) (Alerts, bool, error) {
	a := s.defaults
	err := s.Get(ctx, KeyAlerts, &a)
	if err == storage.ErrNotFound {
		return a, false, nil
	}
	if err != nil {
		s.log.Warn("settings: falha ao ler alerts", "err", err)
		return a, false, err
	}
	return a, true, nil
}

// AllowPrivate expõe a política para validação do webhook.
func (s *Service) AllowPrivate() bool { return s.allowPrivate }
