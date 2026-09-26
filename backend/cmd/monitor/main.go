// Command monitor é o entrypoint da Central de Monitoramento.
// Inicia store, Core Engine e servidor HTTP com graceful shutdown
// (SIGTERM/SIGINT) — arquitetura §1.2, T2.4.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"monitor/internal/api"
	"monitor/internal/auth"
	"monitor/internal/config"
	"monitor/internal/engine"
	"monitor/internal/migrate"
	"monitor/internal/notifier"
	"monitor/internal/settings"
	"monitor/internal/storage"
)

func main() {
	// Subcomando usado pelo healthcheck do container (imagem distroless
	// não tem shell — arquitetura §6.4).
	if len(os.Args) > 1 && os.Args[1] == "health" {
		os.Exit(0)
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(log); err != nil {
		log.Error("falha fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// ─── Store ───────────────────────────────────────────────────────────
	var store storage.Store
	if cfg.DBDSN == "" {
		log.Warn("DB_DSN vazio — usando store em memória (apenas dev/testes)")
		store = storage.NewMem()
	} else {
		if cfg.AutoMigrate {
			if err := migrate.Run(ctx, cfg.DBDSN); err != nil {
				return err
			}
			log.Info("migrações aplicadas")
		}
		pg, err := storage.NewPg(ctx, cfg.DBDSN)
		if err != nil {
			return err
		}
		defer pg.Close()
		store = pg
	}

	// ─── Core Engine ─────────────────────────────────────────────────────
	eng, err := engine.New(ctx, cfg, store, log)
	if err != nil {
		return err
	}

	// ─── Auth (T3.1) ─────────────────────────────────────────────────────
	authSvc, err := auth.New(auth.Config{
		Secret: cfg.JWTSecret,
		TTL:    cfg.JWTTTL,
		Issuer: "monitor",
	})
	if err != nil {
		log.Warn("auth: desabilitado (JWT_SECRET ausente/inválido) — rotas admin com 401", "err", err)
		// Sem serviço de auth, o middleware nega todas as rotas admin.
	}

	// ─── Settings dinâmicos (RF-023 / T4.5) ─────────────────────────────
	settingsSvc := settings.New(store, log, cfg.AllowPrivateTargets,
		settings.DefaultAlerts(cfg.NotifyWebhookURL, cfg.NotifyToEmail, cfg.NotifySuppression))

	// ─── Notifier (T3.7) ─────────────────────────────────────────────────
	var notif *notifier.Notifier
	if cfg.NotifyEnabled {
		notif = notifier.New(notifier.Config{
			Enabled:      cfg.NotifyEnabled,
			SMTPHost:     cfg.SMTPHost,
			SMTPPort:     cfg.SMTPPort,
			SMTPUser:     cfg.SMTPUser,
			SMTPPass:     cfg.SMTPPass,
			SMTPFrom:     cfg.SMTPFrom,
			ToEmail:      cfg.NotifyToEmail,
			WebhookURL:   cfg.NotifyWebhookURL,
			Suppression:  cfg.NotifySuppression,
			Retries:      cfg.NotifyRetries,
			Timeout:      cfg.NotifyTimeout,
			AllowPrivate: cfg.AllowPrivateTargets,
		}, store, eng.Broker(), log)
		notif.SetAlertsReader(settingsSvc)
		go notif.Run(ctx)
		log.Info("notifier: ativo",
			"email", cfg.SMTPHost != "" && cfg.NotifyToEmail != "",
			"webhook", cfg.NotifyWebhookURL != "")
	} else {
		log.Info("notifier: desabilitado (NOTIFY_ENABLED=false)")
	}

	// ─── Servidor HTTP ───────────────────────────────────────────────────
	srv := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: api.New(api.Config{
			Auth:         authSvc,
			Broker:       eng.Broker(),
			Settings:     settingsSvc,
			SSEHeartbeat: cfg.SSEHeartbeat,
			AllowPrivate: cfg.AllowPrivateTargets,
			AuthRateMax:  cfg.AuthRateMax,
			AuthRateWin:  cfg.AuthRateWin,
		}, store, eng).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 2)
	go func() {
		log.Info("http: ouvindo", "addr", cfg.HTTPAddr)
		errCh <- srv.ListenAndServe()
	}()
	go func() {
		eng.Run(ctx) // bloqueia até shutdown do ctx
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			stop()
			eng.Stop()
			return err
		}
		if err == nil {
			// engine encerrou por cancelamento — derruba o servidor também
		}
	case <-ctx.Done():
		log.Info("sinal recebido — iniciando shutdown")
	}

	// Graceful shutdown (T2.4).
	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutCtx)
	eng.Stop()
	log.Info("shutdown concluído")
	return nil
}
