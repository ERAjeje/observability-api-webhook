package domain

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)

func newRT() *EndpointRuntime { return &EndpointRuntime{Status: StatusUnknown} }

// UC-02 — uma falha isolada NÃO declara DOWN.
func TestApply_FalhaIsoladaNaoDerrubaEstado(t *testing.T) {
	sm := NewStateMachine(3, 2)
	rt := newRT()

	tr := sm.Apply(rt, ResultFail, t0)
	if tr.StatusChanged || rt.Status != StatusUnknown {
		t.Fatalf("esperado permanecer unknown; got %s (changed=%v)", rt.Status, tr.StatusChanged)
	}
	if rt.ConsecFails != 1 {
		t.Fatalf("ConsecFails esperado 1; got %d", rt.ConsecFails)
	}

	// Checagem seguinte responde 200 (RT ok).
	tr = sm.Apply(rt, ResultOK, t0.Add(time.Minute))
	if tr.Status != StatusUp || rt.Status != StatusUp {
		t.Fatalf("esperado UP após sucesso; got %s", rt.Status)
	}
	if tr.OpenedIncident {
		t.Fatal("não devia abrir incidente para falha isolada")
	}
}

// UC-01 — N falhas consecutivas declaram DOWN e abrem incidente na 1ª falha.
func TestApply_DownAposNFalhas_AbreIncidenteNaPrimeiraFalha(t *testing.T) {
	sm := NewStateMachine(3, 2)
	rt := newRT()

	// Promove a UP primeiro.
	sm.Apply(rt, ResultOK, t0)

	tr1 := sm.Apply(rt, ResultFail, t0.Add(1*time.Minute))
	if tr1.StatusChanged {
		t.Fatalf("1ª falha não deve mudar status")
	}

	sm.Apply(rt, ResultFail, t0.Add(2*time.Minute))
	tr3 := sm.Apply(rt, ResultFail, t0.Add(3*time.Minute))
	if rt.Status != StatusDown || !tr3.StatusChanged {
		t.Fatalf("esperado DOWN após 3 falhas; got %s changed=%v", rt.Status, tr3.StatusChanged)
	}
	if !tr3.OpenedIncident {
		t.Fatal("3ª falha deve abrir incidente")
	}
	if got := rt.IncidentStarted; got != t0.Add(1*time.Minute) {
		t.Fatalf("incidente deve iniciar na 1ª falha confirmada (%v); got %v", t0.Add(time.Minute), got)
	}
}

// UC-03 — M sucessos consecutivos recuperam UP e fecham incidente.
func TestApply_RecuperacaoFechaIncidente(t *testing.T) {
	sm := NewStateMachine(3, 2)
	rt := newRT()

	// Leva a DOWN (3 falhas) e registra incidente.
	for i := 0; i < 3; i++ {
		tr := sm.Apply(rt, ResultFail, t0.Add(time.Duration(i)*time.Minute))
		if tr.OpenedIncident {
			rt.IncidentID = 42
		}
	}
	if rt.Status != StatusDown {
		t.Fatalf("setup falhou: esperado DOWN; got %s", rt.Status)
	}

	// 1º sucesso — ainda recuperando.
	tr1 := sm.Apply(rt, ResultOK, t0.Add(10*time.Minute))
	if rt.Status != StatusDown || tr1.StatusChanged {
		t.Fatalf("1º sucesso não deve declarar UP; got %s", rt.Status)
	}
	if tr1.ClosedIncident {
		t.Fatal("incidente não deve fechar no 1º sucesso")
	}

	// 2º sucesso — UP + fecha incidente.
	tr2 := sm.Apply(rt, ResultOK, t0.Add(11*time.Minute))
	if rt.Status != StatusUp || !tr2.StatusChanged {
		t.Fatalf("esperado UP após 2 sucessos; got %s", rt.Status)
	}
	if !tr2.ClosedIncident {
		t.Fatal("incidente deve fechar na recuperação")
	}
}

// Endpoint novo: 1 sucesso promove a UP (sem exigir janela).
func TestApply_EndpointNovo_SucessoPromoveAUp(t *testing.T) {
	sm := NewStateMachine(3, 2)
	rt := newRT()
	tr := sm.Apply(rt, ResultOK, t0)
	if rt.Status != StatusUp || !tr.StatusChanged {
		t.Fatalf("1º sucesso deve promover a UP; got %s", rt.Status)
	}
}

// DEGRADED — latência acima do limiar muda o rótulo sem incidente.
func TestApply_DegradadoPorLatencia(t *testing.T) {
	sm := NewStateMachine(3, 2)
	rt := newRT()
	sm.Apply(rt, ResultOK, t0) // UP

	tr := sm.Apply(rt, ResultDegraded, t0.Add(time.Minute))
	if rt.Status != StatusDegraded || !tr.StatusChanged {
		t.Fatalf("esperado DEGRADED; got %s", rt.Status)
	}
	if tr.OpenedIncident {
		t.Fatal("degradação não abre incidente")
	}

	// Um resultado ok volta a UP imediatamente.
	tr = sm.Apply(rt, ResultOK, t0.Add(2*time.Minute))
	if rt.Status != StatusUp || !tr.StatusChanged {
		t.Fatalf("esperado UP após ok; got %s", rt.Status)
	}
}

// Enquanto DOWN, falhas adicionais mantêm o incidente sem reabrir.
func TestApply_EnquantoDownNaoReabreIncidente(t *testing.T) {
	sm := NewStateMachine(3, 2)
	rt := newRT()
	for i := 0; i < 3; i++ {
		tr := sm.Apply(rt, ResultFail, t0.Add(time.Duration(i)*time.Minute))
		if tr.OpenedIncident {
			rt.IncidentID = 7
		}
	}
	tr := sm.Apply(rt, ResultFail, t0.Add(time.Hour))
	if rt.Status != StatusDown || tr.OpenedIncident {
		t.Fatalf("falha durante DOWN não deve reabrir incidente (opened=%v)", tr.OpenedIncident)
	}
}