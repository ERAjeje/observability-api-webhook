// Package checker executa o HTTP request de health-check e classifica o
// resultado (arquitetura §3.1 / T2.5).
package checker

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"monitor/internal/domain"
)

const (
	defaultTimeout = 10 * time.Second
	maxBodyRead    = 64 << 10 // 64 KiB — validação de conteúdo (RF-010)
)

// Outcome é o resultado cru classificado de uma checagem.
type Outcome struct {
	Result      domain.ResultClass
	HTTPStatus  int
	LatencyMS   int64
	ErrorDetail string
}

// Checker executa os requests com um transporte reutilizado e otimizado
// (pool de conexões por host — arquitetura §3.1).
type Checker struct {
	client *http.Client
}

// New cria um Checker com transporte tunado para checagens concorrentes.
func New() *Checker {
	tr := &http.Transport{
		MaxIdleConns:        256,
		MaxIdleConnsPerHost: 64,
		IdleConnTimeout:     90 * time.Second,
	}
	return &Checker{client: &http.Client{Transport: tr}}
}

// Run executa a checagem com timeout do endpoint (RF-009) e aplica as
// validações de status/conteúdo (RF-010) e latência (RF-004/RF-008).
func (c *Checker) Run(ctx context.Context, ep domain.Endpoint) Outcome {
	timeout := ep.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var body io.Reader
	if ep.Body != "" {
		body = strings.NewReader(ep.Body)
	}
	req, err := http.NewRequestWithContext(ctx, ep.Method, ep.URL, body)
	if err != nil {
		return Outcome{Result: domain.ResultFail, ErrorDetail: "request inválido: " + err.Error()}
	}
	for k, v := range ep.Headers {
		req.Header.Set(k, v)
	}

	start := time.Now()
	resp, err := c.client.Do(req)
	latency := time.Since(start).Milliseconds()

	if err != nil {
		detail := "falha de rede: " + err.Error()
		if ctx.Err() == context.DeadlineExceeded {
			detail = "timeout após " + timeout.String()
		}
		return Outcome{Result: domain.ResultFail, LatencyMS: latency, ErrorDetail: detail}
	}
	defer resp.Body.Close()

	statusOK := statusAccepted(ep, resp.StatusCode)
	bodyOK := true
	if ep.ExpectBody != "" && statusOK {
		b, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyRead))
		if err != nil {
			bodyOK = false
		} else if !strings.Contains(string(b), ep.ExpectBody) {
			bodyOK = false
		}
	}

	switch {
	case !statusOK || !bodyOK:
		detail := "status não esperado: " + resp.Status
		if !bodyOK {
			detail = "conteúdo esperado não encontrado: " + ep.ExpectBody
		}
		return Outcome{Result: domain.ResultFail, HTTPStatus: resp.StatusCode,
			LatencyMS: latency, ErrorDetail: detail}
	case ep.LatencyThreshold > 0 && time.Duration(latency)*time.Millisecond > ep.LatencyThreshold:
		return Outcome{Result: domain.ResultDegraded, HTTPStatus: resp.StatusCode,
			LatencyMS: latency, ErrorDetail: "latência acima do limiar"}
	default:
		return Outcome{Result: domain.ResultOK, HTTPStatus: resp.StatusCode,
			LatencyMS: latency}
	}
}

// statusAccepted valida o código HTTP conforme expect_status (RF-010).
func statusAccepted(ep domain.Endpoint, code int) bool {
	if ep.ExpectStatus > 0 {
		return code == ep.ExpectStatus
	}
	return code >= 200 && code < 400
}