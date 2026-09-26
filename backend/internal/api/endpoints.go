package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"monitor/internal/checker"
	"monitor/internal/domain"
	"monitor/internal/quota"
	"monitor/internal/seal"
	"monitor/internal/storage"
)

// endpointBody é o payload de criação/edição (RF-001..RF-005). Headers e body
// NUNCA são expostos nas rotas públicas (RNF-018) — apenas no admin autenticado.
type endpointBody struct {
	GroupID            *int64            `json:"group_id"`
	Name               string            `json:"name"`
	URL                string            `json:"url"`
	Method             string            `json:"method"`
	Headers            map[string]string `json:"headers"`
	Body               string            `json:"body"`
	IntervalSeconds    int               `json:"interval_seconds"`
	TimeoutMS          int               `json:"timeout_ms"`
	LatencyThresholdMS int               `json:"latency_threshold_ms"`
	ExpectStatus       int               `json:"expect_status"`
	ExpectBody         string            `json:"expect_body"`
	Active             *bool             `json:"active"`
}

// toEndpoint converte o payload em domain.Endpoint com defaults seguros.
func (b endpointBody) toEndpoint() (domain.Endpoint, error) {
	method := b.Method
	if method == "" {
		method = http.MethodGet
	}
	interval := time.Duration(b.IntervalSeconds) * time.Second
	if b.IntervalSeconds == 0 {
		interval = 5 * time.Minute
	}
	timeout := time.Duration(b.TimeoutMS) * time.Millisecond
	if b.TimeoutMS == 0 {
		timeout = 10 * time.Second
	}
	active := true
	if b.Active != nil {
		active = *b.Active
	}
	headers := b.Headers
	if headers == nil {
		headers = map[string]string{} // jsonb NOT NULL — evitar NULL explícito
	}
	e := domain.Endpoint{
		GroupID:          b.GroupID,
		Name:             b.Name,
		URL:              b.URL,
		Method:           method,
		Headers:          headers,
		Body:             b.Body,
		Interval:         interval,
		Timeout:          timeout,
		LatencyThreshold: time.Duration(b.LatencyThresholdMS) * time.Millisecond,
		ExpectStatus:     b.ExpectStatus,
		ExpectBody:       b.ExpectBody,
		Active:           active,
	}
	return e, e.Validate()
}

// endpointDTO é a representação de saída (sem segredos fora do admin).
// S-05: headers estão cifrados no store → descriptografa aqui (painel admin
// autenticado). Fail-closed: erro de decifra → headers vazios (nunca vazam).
func (s *Server) endpointDTO(e domain.Endpoint) map[string]any {
	if seal.IsSealed(e.Headers) {
		if _, err := seal.DecryptEndpointHeaders(&e, s.sealKey); err != nil {
			slog.Warn("api: falha ao decifrar headers do endpoint", "endpoint", e.ID, "err", err)
			e.Headers = map[string]string{}
		}
	}
	return map[string]any{
		"id":                   e.ID,
		"owner_id":             e.OwnerID,
		"group_id":             e.GroupID,
		"name":                 e.Name,
		"url":                  e.URL,
		"method":               e.Method,
		"headers":              e.Headers,
		"body":                 e.Body,
		"interval_seconds":     e.Interval.Seconds(),
		"timeout_ms":           e.Timeout.Milliseconds(),
		"latency_threshold_ms": e.LatencyThreshold.Milliseconds(),
		"expect_status":        e.ExpectStatus,
		"expect_body":          e.ExpectBody,
		"active":               e.Active,
		"status":               e.Status,
		"next_check_at":        e.NextCheckAt,
		"created_at":           e.CreatedAt,
		"updated_at":           e.UpdatedAt,
	}
}

// validateGroupRef confirma que o grupo referenciado existe (RF-005).
func (s *Server) validateGroupRef(ctx context.Context, groupID *int64) bool {
	if groupID == nil {
		return true
	}
	_, err := s.store.GetGroup(ctx, *groupID)
	return err == nil
}

// listEndpoints devolve todos os endpoints (admin).
func (s *Server) listEndpoints(w http.ResponseWriter, r *http.Request) {
	eps, err := s.store.ListEndpoints(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "falha ao listar endpoints")
		return
	}
	out := make([]map[string]any, 0, len(eps))
	for _, e := range eps {
		out = append(out, s.endpointDTO(e))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

// handleQuotaErr mapeia violação de cota (S-08) para HTTP 429 com a mensagem
// amigável; demais erros viram 500. Retorna true quando já respondeu.
func (s *Server) handleQuotaErr(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if quota.IsErrQuotaExceeded(err) {
		writeErr(w, http.StatusTooManyRequests, err.Error())
		return true
	}
	slog.Error("api: falha ao avaliar cota", "err", err)
	writeErr(w, http.StatusInternalServerError, "falha ao avaliar cota")
	return true
}

// createEndpoint valida + persiste + sincroniza o scheduler (RF-016).
func (s *Server) createEndpoint(w http.ResponseWriter, r *http.Request) {
	var b endpointBody
	if !decodeJSON(w, r, &b) {
		return
	}
	e, err := b.toEndpoint()
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.validateGroupRef(r.Context(), e.GroupID) {
		writeErr(w, http.StatusBadRequest, "grupo inexistente")
		return
	}
	if err := validateTargetOrReject(e.URL, s.allowPrivate); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// S-08: vincula a conta autenticada e valida as cotas (429).
	e.OwnerID = userIDFrom(r.Context())
	if s.quota != nil {
		if err := s.quota.CheckCreate(r.Context(), e.OwnerID, e); s.handleQuotaErr(w, err) {
			return
		}
	}
	// S-05: cifra headers antes de persistir (AES-256-GCM em repouso).
	if _, err := seal.EncryptEndpointHeaders(&e, s.sealKey); err != nil {
		slog.Error("api: cifragem de headers falhou", "err", err)
		writeErr(w, http.StatusInternalServerError, "falha ao persistir segredos")
		return
	}
	id, err := s.store.CreateEndpoint(r.Context(), e)
	if err != nil {
		slog.Error("api: criar endpoint falhou", "err", err)
		writeErr(w, http.StatusInternalServerError, "falha ao criar endpoint")
		return
	}
	e.ID = id
	if err := s.eng.SyncEndpoint(r.Context(), e); err != nil {
		s.logCreationSync(w, id)
		return
	}
	writeJSON(w, http.StatusCreated, s.endpointDTO(e))
}

func (s *Server) logCreationSync(w http.ResponseWriter, id int64) {
	// SyncEndpoint falha apenas por problema de storage; o endpoint existe.
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "sync_pending": true})
}

// getEndpoint devolve um endpoint pelo id.
func (s *Server) getEndpoint(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	e, err := s.store.GetEndpoint(r.Context(), id)
	if mapStoreErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, s.endpointDTO(e))
}

// updateEndpoint edita e invalida o cache de agendamento (T3.2).
func (s *Server) updateEndpoint(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	var b endpointBody
	if !decodeJSON(w, r, &b) {
		return
	}
	e, err := b.toEndpoint()
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	e.ID = id
	// Carrega o registro atual cedo: dá 404 imediato, preserva o dono (S-08)
	// e evita zerar o status na persistência.
	cur, err := s.store.GetEndpoint(r.Context(), id)
	if mapStoreErr(w, err) {
		return
	}
	e.OwnerID = cur.OwnerID
	if !s.validateGroupRef(r.Context(), e.GroupID) {
		writeErr(w, http.StatusBadRequest, "grupo inexistente")
		return
	}
	if err := validateTargetOrReject(e.URL, s.allowPrivate); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// S-08: revalida as cotas com o novo intervalo (substituindo o atual).
	if s.quota != nil {
		if err := s.quota.CheckUpdate(r.Context(), cur.OwnerID, id, e); s.handleQuotaErr(w, err) {
			return
		}
	}
	// S-05: cifra headers antes de persistir.
	if _, err := seal.EncryptEndpointHeaders(&e, s.sealKey); err != nil {
		slog.Error("api: cifragem de headers falhou", "endpoint", id, "err", err)
		writeErr(w, http.StatusInternalServerError, "falha ao persistir segredos")
		return
	}
	if err := s.store.UpdateEndpoint(r.Context(), e); mapStoreErr(w, err) {
		if err != nil && !errors.Is(err, storage.ErrNotFound) {
			slog.Error("api: atualizar endpoint falhou", "endpoint", id, "err", err)
		}
		return
	}
	// Recarrega o estado persistido (status) para manter o runtime coerente.
	// (o UPDATE não altera status; cur foi lido no início do handler).
	e.Status = cur.Status
	_ = s.eng.SyncEndpoint(r.Context(), e)
	writeJSON(w, http.StatusOK, s.endpointDTO(e))
}

// deleteEndpoint remove e notifica o engine (RNF-012).
func (s *Server) deleteEndpoint(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteEndpoint(r.Context(), id); mapStoreErr(w, err) {
		return
	}
	s.eng.DropEndpoint(id)
	writeJSON(w, http.StatusNoContent, nil)
}

// testBody valida conectividade sem persistir (RF-001..RF-005).
type testBody struct {
	URL       string            `json:"url"`
	Method    string            `json:"method"`
	Headers   map[string]string `json:"headers"`
	Body      string            `json:"body"`
	TimeoutMS int               `json:"timeout_ms"`
}

// testEndpoint executa UMA checagem manual e devolve o resultado.
func (s *Server) testEndpoint(w http.ResponseWriter, r *http.Request) {
	var b testBody
	if !decodeJSON(w, r, &b) {
		return
	}
	ep := domain.Endpoint{
		URL:     b.URL,
		Method:  b.Method,
		Headers: b.Headers,
		Body:    b.Body,
		Timeout: time.Duration(b.TimeoutMS) * time.Millisecond,
	}
	if ep.Method == "" {
		ep.Method = http.MethodGet
	}
	if ep.Timeout <= 0 {
		ep.Timeout = 10 * time.Second
	}
	if err := validateTargetOrReject(ep.URL, s.allowPrivate); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	check := checker.New(s.allowPrivate)
	ctx, cancel := context.WithTimeout(r.Context(), ep.Timeout+2*time.Second)
	defer cancel()
	out := check.Run(ctx, ep)
	writeJSON(w, http.StatusOK, map[string]any{
		"result":      out.Result,
		"http_status": out.HTTPStatus,
		"latency_ms":  out.LatencyMS,
		"error":       out.ErrorDetail,
	})
}

// validateTargetOrReject bloqueia URLs apontando para faixas internas -
// replica a política anti-SSRF do checker (S-01) na validação de escrita.
var validateTargetOrReject = func(rawURL string, allowPrivate bool) error {
	if err := checker.ValidateTarget(rawURL, allowPrivate); err != nil {
		return err
	}
	return nil
}

// parseID lê o parâmetro {id} da rota.
func parseID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "id inválido")
		return 0, false
	}
	return id, true
}
