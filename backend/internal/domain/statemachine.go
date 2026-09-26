package domain

import "time"

// EndpointRuntime é o estado volátil da state machine de UM endpoint,
// mantido em memória e materializado no banco a cada transição
// (arquitetura §3.3).
type EndpointRuntime struct {
	Status          StatusClass
	ConsecFails     int
	ConsecSuccesses int
	FailWindowStart time.Time // horário da 1ª falha da janela consecutiva
	IncidentID      int64     // 0 = sem incidente aberto
	IncidentStarted time.Time
	InFlight        bool // guarda anti-sobreposição (RF-011)
}

// Transition descreve o resultado de Apply para o engine agir.
type Transition struct {
	Status         StatusClass
	StatusChanged  bool
	OpenedIncident bool
	ClosedIncident bool
}

// StateMachine implementa a janela de confirmação N/M contra falso positivo
// (RF-013): DOWN após N falhas consecutivas, UP após M sucessos consecutivos.
// DEGRADED é detectado a partir de UM resultado acima do limiar de latência
// (RF-004/RF-008), sem janela.
type StateMachine struct {
	FailThreshold    int
	SuccessThreshold int
}

// NewStateMachine retorna uma máquina com os limiares informados.
func NewStateMachine(fail, success int) *StateMachine {
	return &StateMachine{FailThreshold: fail, SuccessThreshold: success}
}

// Apply processa o resultado cru de uma checagem e evolui o runtime.
// at é o horário da checagem (base para janelas e incidentes).
func (sm *StateMachine) Apply(rt *EndpointRuntime, res ResultClass, at time.Time) Transition {
	tr := Transition{Status: rt.Status}

	switch res {
	case ResultOK:
		rt.ConsecFails = 0
		rt.FailWindowStart = time.Time{}
		switch rt.Status {
		case StatusUnknown:
			// Endpoint novo: 1 sucesso já o promove a UP.
			rt.Status = StatusUp
			tr.Status = StatusUp
			tr.StatusChanged = true
		case StatusDown:
			rt.ConsecSuccesses++
			if rt.ConsecSuccesses >= sm.SuccessThreshold {
				rt.ConsecSuccesses = 0
				rt.Status = StatusUp
				tr.Status = StatusUp
				tr.StatusChanged = true
				if rt.IncidentID != 0 {
					tr.ClosedIncident = true
				}
			} else {
				// Ainda em recuperação — mantém estado.
				tr.Status = rt.Status
			}
		case StatusDegraded:
			// Estado "soft": 1 sucesso já restaura UP (sem janela).
			rt.Status = StatusUp
			tr.Status = StatusUp
			tr.StatusChanged = true
		default: // já UP
			tr.Status = StatusUp
		}

	case ResultDegraded:
		rt.ConsecFails = 0
		rt.ConsecSuccesses = 0
		// Degradação não abre incidente; só muda o rótulo quando vindo de UP/unknown.
		if rt.Status == StatusUp || rt.Status == StatusUnknown {
			rt.Status = StatusDegraded
			tr.Status = StatusDegraded
			tr.StatusChanged = true
		} else {
			tr.Status = rt.Status
		}

	case ResultFail:
		rt.ConsecSuccesses = 0
		if rt.ConsecFails == 0 {
			rt.FailWindowStart = at
		}
		rt.ConsecFails++
		switch rt.Status {
		case StatusDown:
			// Continua em incidente aberto.
			tr.Status = StatusDown
		default: // UP, DEGRADED ou UNKNOWN
			if rt.ConsecFails >= sm.FailThreshold {
				rt.ConsecFails = 0
				rt.Status = StatusDown
				tr.Status = StatusDown
				tr.StatusChanged = true
				if rt.IncidentID == 0 {
					rt.IncidentStarted = rt.FailWindowStart
					tr.OpenedIncident = true
				}
			} else {
				// Ainda não confirmou — mantém o estado atual (anti falso positivo).
				tr.Status = rt.Status
			}
		}
	}

	return tr
}

// Reset limpa o runtime (usado ao reativar um endpoint).
func (rt *EndpointRuntime) Reset() {
	rt.ConsecFails, rt.ConsecSuccesses = 0, 0
	rt.FailWindowStart = time.Time{}
	rt.IncidentID = 0
	rt.IncidentStarted = time.Time{}
}
