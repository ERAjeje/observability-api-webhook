// Package quota impõe limites de recurso por conta (S-08): máximo de
// endpoints por conta, intervalo mínimo de checagem e projeção de
// checks/mês. Violações viram *ErrQuotaExceeded → HTTP 429 na API admin.
//
// A projeção mensal impede auto-DoS mesmo com poucos endpoints usando
// intervalos agressivos (RF-007 sem teto): soma-se 30d/intervalo de cada
// endpoint da conta e compara-se com a cota (configurada ou derivada).
package quota

import (
	"context"
	"errors"
	"fmt"
	"time"

	"monitor/internal/domain"
)

// MonthlySeconds é a base do teto mensal (30 dias) — constante, simples e
// determinística para o cálculo projetado (o real é validado por monitoramento).
const MonthlySeconds = 30 * 24 * 3600

// Limits são as cotas por conta. ChecksPerMonth == 0 deriva da combinação
// MaxEndpointsPerAccount × MonthlySeconds/MinInterval (teto do pior caso).
type Limits struct {
	MaxEndpointsPerAccount int
	MinInterval            time.Duration
	ChecksPerMonth         int64
}

// ErrQuotaExceeded descreve a violação de cota (→ 429 com mensagem amigável).
type ErrQuotaExceeded struct {
	Code    string // endpoints_max | interval_min | checks_month
	Msg     string
	Current int64
	Limit   int64
}

func (e *ErrQuotaExceeded) Error() string { return e.Msg }

// IsErrQuotaExceeded reporta se o erro é violação de cota.
func IsErrQuotaExceeded(err error) bool {
	var qe *ErrQuotaExceeded
	return errors.As(err, &qe)
}

// Store é a leitura mínima necessária para a contabilidade (S-08).
type Store interface {
	ListEndpointsByOwner(ctx context.Context, ownerID int64) ([]domain.Endpoint, error)
}

// Service aplica as cotas por conta (S-08).
type Service struct {
	limits Limits
	store  Store
}

// New cria o serviço de cotas.
func New(limits Limits, store Store) *Service { return &Service{limits: limits, store: store} }

// EffectiveChecksPerMonth devolve a cota mensal efetiva (configurada ou
// derivada do pior caso). 0 = sem teto mensal adicional.
func (s *Service) EffectiveChecksPerMonth() int64 {
	if s.limits.ChecksPerMonth > 0 {
		return s.limits.ChecksPerMonth
	}
	if s.limits.MaxEndpointsPerAccount <= 0 || s.limits.MinInterval <= 0 {
		return 0
	}
	return int64(s.limits.MaxEndpointsPerAccount) * monthlyChecks(s.limits.MinInterval)
}

// CheckCreate valida um cadastro novo: intervalo mínimo, teto de endpoints e
// projeção mensal considerando o endpoint que está sendo adicionado.
func (s *Service) CheckCreate(ctx context.Context, ownerID int64, e domain.Endpoint) error {
	if ownerID <= 0 {
		return nil // fluxos internos/legado (sem conta) não são cotados
	}
	if err := s.checkMinInterval(ownerID, e); err != nil {
		return err
	}
	eps, err := s.store.ListEndpointsByOwner(ctx, ownerID)
	if err != nil {
		return fmt.Errorf("quota: listar endpoints: %w", err)
	}
	if s.limits.MaxEndpointsPerAccount > 0 &&
		len(eps) >= s.limits.MaxEndpointsPerAccount {
		return &ErrQuotaExceeded{
			Code:    "endpoints_max",
			Msg:     fmt.Sprintf("limite de %d endpoints por conta atingido", s.limits.MaxEndpointsPerAccount),
			Current: int64(len(eps)),
			Limit:   int64(s.limits.MaxEndpointsPerAccount),
		}
	}
	return s.checkMonthly(ctx, eps, -1, e)
}

// CheckUpdate valida edição: substitui o intervalo do endpoint existente na
// conta (não soma em dobro) e reaplica o intervalo mínimo.
func (s *Service) CheckUpdate(ctx context.Context, ownerID, endpointID int64, e domain.Endpoint) error {
	if ownerID <= 0 {
		return nil
	}
	if err := s.checkMinInterval(ownerID, e); err != nil {
		return err
	}
	eps, err := s.store.ListEndpointsByOwner(ctx, ownerID)
	if err != nil {
		return fmt.Errorf("quota: listar endpoints: %w", err)
	}
	return s.checkMonthly(ctx, eps, endpointID, e)
}

// checkMinInterval força o intervalo mínimo por conta (429, não 400 — é cota).
func (s *Service) checkMinInterval(ownerID int64, e domain.Endpoint) error {
	if s.limits.MinInterval <= 0 || e.Interval >= s.limits.MinInterval {
		return nil
	}
	return &ErrQuotaExceeded{
		Code:    "interval_min",
		Msg:     fmt.Sprintf("intervalo mínimo por conta: %s", s.limits.MinInterval),
		Current: int64(e.Interval.Seconds()),
		Limit:   int64(s.limits.MinInterval.Seconds()),
	}
}

// checkMonthly soma a projeção mensal da conta. Se replaceID > 0, o intervalo
// de e substitui o do endpoint existente (fluxo de update).
func (s *Service) checkMonthly(_ context.Context, eps []domain.Endpoint, replaceID int64, e domain.Endpoint) error {
	quota := s.EffectiveChecksPerMonth()
	if quota <= 0 {
		return nil
	}
	var total int64
	replaced := false
	for _, x := range eps {
		if x.ID == replaceID {
			total += monthlyChecks(e.Interval)
			replaced = true
		} else {
			total += monthlyChecks(x.Interval)
		}
	}
	if !replaced {
		total += monthlyChecks(e.Interval)
	}
	if total > quota {
		return &ErrQuotaExceeded{
			Code:    "checks_month",
			Msg:     fmt.Sprintf("projeção de %d checagens/mês excede a cota da conta (%d)", total, quota),
			Current: total,
			Limit:   quota,
		}
	}
	return nil
}

func monthlyChecks(interval time.Duration) int64 {
	if interval <= 0 {
		return 0
	}
	if secs := int64(interval.Seconds()); secs > 0 {
		return MonthlySeconds / secs
	}
	return 0
}
