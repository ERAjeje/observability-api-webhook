// Package domain contém as entidades puras e a state machine de status —
// sem dependência de I/O, testáveis isoladamente (arquitetura §3.3, T2.1).
package domain

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// StatusClass é o estado agregado de um endpoint exibido na status page
// (RF-008).
type StatusClass string

const (
	StatusUnknown  StatusClass = "unknown"
	StatusUp       StatusClass = "up"
	StatusDown     StatusClass = "down"
	StatusDegraded StatusClass = "degraded"
)

// ResultClass é a classificação crua de UMA checagem (RF-008, RF-010).
type ResultClass string

const (
	ResultOK       ResultClass = "ok"
	ResultDegraded ResultClass = "degraded"
	ResultFail     ResultClass = "fail"
)

// Endpoint representa um serviço monitorado cadastrado pelo usuário
// (RF-001..RF-005).
type Endpoint struct {
	ID               int64
	GroupID          *int64
	Name             string
	URL              string
	Method           string
	Headers          map[string]string
	Body             string
	Interval         time.Duration
	Timeout          time.Duration
	LatencyThreshold time.Duration // 0 = desabilitado (RF-004)
	ExpectStatus     int           // 0 = qualquer 2xx/3xx (RF-010)
	ExpectBody       string        // vazio = sem validação de conteúdo
	Active           bool
	Status           StatusClass // último estado persistido (snapshot/recovery)
	NextCheckAt      time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Validate valida as regras de negócio de cadastro (RF-003).
func (e *Endpoint) Validate() error {
	if strings.TrimSpace(e.Name) == "" {
		return fmt.Errorf("nome é obrigatório")
	}
	u, err := url.Parse(e.URL)
	if err != nil || u.Host == "" {
		return fmt.Errorf("URL inválida: %q", e.URL)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme deve ser http/https: %q", e.URL)
	}
	if e.Method == "" {
		return fmt.Errorf("método HTTP é obrigatório")
	}
	if e.Interval < time.Second {
		return fmt.Errorf("intervalo mínimo é 1s")
	}
	if e.Timeout <= 0 {
		return fmt.Errorf("timeout deve ser > 0")
	}
	return nil
}

// Check é o log bruto de uma checagem (RF-014).
type Check struct {
	ID          int64
	EndpointID  int64
	CheckedAt   time.Time
	Result      ResultClass
	HTTPStatus  int
	LatencyMS   int64
	ErrorDetail string
}

// Rollup é a agregação por minuto usada nos gráficos (RF-015, RNF-014).
type Rollup struct {
	EndpointID   int64
	Bucket       time.Time // alinhado ao minuto
	Count        int64
	OKCount      int64
	SumLatencyMS int64
	P50LatencyMS int64
	P95LatencyMS int64
}

// AvgLatencyMS média aritmética da janela.
func (r Rollup) AvgLatencyMS() float64 {
	if r.Count == 0 {
		return 0
	}
	return float64(r.SumLatencyMS) / float64(r.Count)
}

// Incident representa uma janela de downtime consolidada (RF-017).
type Incident struct {
	ID         int64
	EndpointID int64
	StartedAt  time.Time
	EndedAt    *time.Time
	DurationMS *int64
	Resolution string
}
