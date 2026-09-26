// Package config carrega e valida a configuração da aplicação a partir de
// variáveis de ambiente, com defaults seguros (arquitetura §1.2, T1.3).
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"monitor/internal/seal"
)

// Config concentra toda a configuração de runtime do monitor.
type Config struct {
	HTTPAddr string
	Env      string

	// Banco de dados (T1.4). Vazio em desenvolvimento ativa o store em memória.
	DBDSN       string
	AutoMigrate bool

	// Worker Pool (arquitetura §3.2, T2.3/T2.6).
	WorkerPoolSize int
	PoolQueueSize  int
	BatchSize      int
	BatchFlush     time.Duration

	// Scheduler (T2.3).
	SchedulerTick  time.Duration
	JitterFraction float64

	// Segurança de egress — SSRF guard (checker/security.go).
	// false (default): bloqueia loopback/RFC1918/link-local/metadata cloud.
	AllowPrivateTargets bool

	// State machine — janela de confirmação N/M (RF-013, T2.1).
	FailThreshold    int
	SuccessThreshold int

	// SSE (Fase 3).
	SSEHeartbeat time.Duration

	// Auth (Fase 3).
	JWTSecret   string
	JWTTTL      time.Duration
	AuthRateMax int           // tentativas de login/signup por janela (RNF-017)
	AuthRateWin time.Duration // janela do rate limit

	// Notifier (T3.7).
	NotifyEnabled     bool
	SMTPHost          string
	SMTPPort          int
	SMTPUser          string
	SMTPPass          string
	SMTPFrom          string
	NotifyToEmail     string
	NotifyWebhookURL  string
	NotifySuppression time.Duration
	NotifyRetries     int
	NotifyTimeout     time.Duration

	// S-05 — cifragem dos headers dos endpoints em repouso (AES-256-GCM).
	// nil = modo texto plano (dev). Em produção com blob cifrado no banco e
	// chave ausente, o engine segue fail-closed (sem headers, sem vazar blob).
	HeadersSecret []byte
}

// Load constrói a Config a partir do ambiente com defaults documentados em
// .env.example.
func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:            getEnv("HTTP_ADDR", ":8080"),
		Env:                 getEnv("ENV", "development"),
		DBDSN:               os.Getenv("DB_DSN"),
		AutoMigrate:         getBool("AUTO_MIGRATE", true),
		WorkerPoolSize:      getInt("WORKER_POOL_SIZE", 20),
		PoolQueueSize:       getInt("POOL_QUEUE_SIZE", 1000),
		BatchSize:           getInt("MAX_BATCH_SIZE", 100),
		BatchFlush:          time.Duration(getInt("BATCH_FLUSH_SECONDS", 1)) * time.Second,
		SchedulerTick:       time.Duration(getInt("SCHEDULER_TICK_SECONDS", 10)) * time.Second,
		JitterFraction:      getFloat("JITTER_FRACTION", 0.2),
		AllowPrivateTargets: getBool("ALLOW_PRIVATE_TARGETS", false),
		FailThreshold:       getInt("FAIL_THRESHOLD", 3),
		SuccessThreshold:    getInt("SUCCESS_THRESHOLD", 2),
		SSEHeartbeat:        time.Duration(getInt("SSE_HEARTBEAT_SECONDS", 15)) * time.Second,
		JWTSecret:           os.Getenv("JWT_SECRET"),
		JWTTTL:              time.Duration(getInt("JWT_TTL_HOURS", 24)) * time.Hour,
		AuthRateMax:         getInt("AUTH_RATE_LIMIT", 5),
		AuthRateWin:         time.Duration(getInt("AUTH_RATE_WINDOW_SECONDS", 60)) * time.Second,
		NotifyEnabled:       getBool("NOTIFY_ENABLED", false),
		SMTPHost:            os.Getenv("SMTP_HOST"),
		SMTPPort:            getInt("SMTP_PORT", 587),
		SMTPUser:            os.Getenv("SMTP_USER"),
		SMTPPass:            os.Getenv("SMTP_PASS"),
		SMTPFrom:            os.Getenv("SMTP_FROM"),
		NotifyToEmail:       os.Getenv("NOTIFY_TO_EMAIL"),
		NotifyWebhookURL:    os.Getenv("NOTIFY_WEBHOOK_URL"),
		NotifySuppression:   time.Duration(getInt("NOTIFY_SUPPRESSION_SECONDS", 300)) * time.Second,
		NotifyRetries:       getInt("NOTIFY_RETRIES", 3),
		NotifyTimeout:       time.Duration(getInt("NOTIFY_TIMEOUT_SECONDS", 5)) * time.Second,
	}

	// S-05: chave de cifragem dos headers (hex de 32 bytes). Erro → falha de
	// boot em produção (nunca iniciar com chave inválida).
	headersKey, err := seal.ParseKey(os.Getenv("HEADERS_ENC_KEY"))
	if err != nil {
		return cfg, err
	}
	cfg.HeadersSecret = headersKey

	if cfg.WorkerPoolSize < 1 {
		return cfg, fmt.Errorf("WORKER_POOL_SIZE deve ser >= 1 (got %d)", cfg.WorkerPoolSize)
	}
	if cfg.PoolQueueSize < 1 {
		return cfg, fmt.Errorf("POOL_QUEUE_SIZE deve ser >= 1 (got %d)", cfg.PoolQueueSize)
	}
	if cfg.FailThreshold < 1 || cfg.SuccessThreshold < 1 {
		return cfg, fmt.Errorf("FAIL_THRESHOLD e SUCCESS_THRESHOLD devem ser >= 1")
	}
	if cfg.Env == "production" && cfg.JWTSecret == "" {
		return cfg, fmt.Errorf("JWT_SECRET é obrigatório em produção")
	}
	return cfg, nil
}

func getEnv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func getInt(key string, def int) int {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func getBool(key string, def bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func getFloat(key string, def float64) float64 {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}
