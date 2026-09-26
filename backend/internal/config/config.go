// Package config carrega e valida a configuração da aplicação a partir de
// variáveis de ambiente, com defaults seguros (arquitetura §1.2, T1.3).
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
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

	// State machine — janela de confirmação N/M (RF-013, T2.1).
	FailThreshold    int
	SuccessThreshold int

	// SSE (Fase 3).
	SSEHeartbeat time.Duration

	// Auth (Fase 3).
	JWTSecret string
}

// Load constrói a Config a partir do ambiente com defaults documentados em
// .env.example.
func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:         getEnv("HTTP_ADDR", ":8080"),
		Env:              getEnv("ENV", "development"),
		DBDSN:            os.Getenv("DB_DSN"),
		AutoMigrate:      getBool("AUTO_MIGRATE", true),
		WorkerPoolSize:   getInt("WORKER_POOL_SIZE", 20),
		PoolQueueSize:    getInt("POOL_QUEUE_SIZE", 1000),
		BatchSize:        getInt("MAX_BATCH_SIZE", 100),
		BatchFlush:       time.Duration(getInt("BATCH_FLUSH_SECONDS", 1)) * time.Second,
		SchedulerTick:    time.Duration(getInt("SCHEDULER_TICK_SECONDS", 10)) * time.Second,
		JitterFraction:   getFloat("JITTER_FRACTION", 0.2),
		FailThreshold:    getInt("FAIL_THRESHOLD", 3),
		SuccessThreshold: getInt("SUCCESS_THRESHOLD", 2),
		SSEHeartbeat:     time.Duration(getInt("SSE_HEARTBEAT_SECONDS", 15)) * time.Second,
		JWTSecret:        os.Getenv("JWT_SECRET"),
	}

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