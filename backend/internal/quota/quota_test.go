package quota

import (
	"context"
	"testing"
	"time"

	"monitor/internal/domain"
	"monitor/internal/storage"
)

func mustCreate(t *testing.T, s *storage.MemStore, ownerID int64, interval time.Duration) int64 {
	t.Helper()
	id, err := s.CreateEndpoint(context.Background(), domain.Endpoint{
		OwnerID: ownerID,
		Name:    "ep", URL: "https://example.com/h", Method: "GET",
		Interval: interval, Timeout: time.Second, Active: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func epFor(interval time.Duration) domain.Endpoint {
	return domain.Endpoint{
		Name: "novo", URL: "https://example.com/h", Method: "GET",
		Interval: interval, Timeout: time.Second, Active: true,
	}
}

func TestCheckCreate_MaxEndpoints(t *testing.T) {
	store := storage.NewMem()
	svc := New(Limits{MaxEndpointsPerAccount: 2, MinInterval: 5 * time.Second}, store)

	for i := 0; i < 2; i++ {
		if err := svc.CheckCreate(context.Background(), 7, epFor(time.Minute)); err != nil {
			t.Fatalf("create %d não deveria estourar: %v", i, err)
		}
		mustCreate(t, store, 7, time.Minute)
	}

	err := svc.CheckCreate(context.Background(), 7, epFor(time.Minute))
	qe, ok := err.(*ErrQuotaExceeded)
	if !ok || qe.Code != "endpoints_max" || qe.Current != 2 || qe.Limit != 2 {
		t.Fatalf("esperava endpoints_max, got %v", err)
	}

	// Contas diferentes não interferem (owner 8 ainda cabe).
	if err := svc.CheckCreate(context.Background(), 8, epFor(time.Minute)); err != nil {
		t.Fatalf("outra conta não devia ser atingida: %v", err)
	}
}

func TestCheckCreate_IntervaloMinimo(t *testing.T) {
	svc := New(Limits{MaxEndpointsPerAccount: 10, MinInterval: 30 * time.Second}, storage.NewMem())
	err := svc.CheckCreate(context.Background(), 1, epFor(5*time.Second))
	if qe, ok := err.(*ErrQuotaExceeded); !ok || qe.Code != "interval_min" || qe.Current != 5 || qe.Limit != 30 {
		t.Fatalf("esperava interval_min 5<30, got %v", err)
	}
	if err := svc.CheckCreate(context.Background(), 1, epFor(30*time.Second)); err != nil {
		t.Fatalf("intervalo = mínimo deveria passar: %v", err)
	}
}

func TestCheckCreate_ProjecaoMensal(t *testing.T) {
	store := storage.NewMem()
	const quota = int64(90000) // cabe: 1×43.200 (60s) + 1×21.600 (120s) = 64.800
	svc := New(Limits{MaxEndpointsPerAccount: 10, MinInterval: 10 * time.Second, ChecksPerMonth: quota}, store)

	mustCreate(t, store, 3, time.Minute)   // 43.200
	mustCreate(t, store, 3, 2*time.Minute) // 21.600

	// +1 endpoint de 60s: 64.800+43.200 = 108.000 > 90.000 → estoura.
	err := svc.CheckCreate(context.Background(), 3, epFor(time.Minute))
	if qe, ok := err.(*ErrQuotaExceeded); !ok || qe.Code != "checks_month" {
		t.Fatalf("esperava checks_month, got %v", err)
	}
	// Intervalo maior cabe: 64.800+7.200 (3600s) = 72.000 ≤ 90.000.
	if err := svc.CheckCreate(context.Background(), 3, epFor(time.Hour)); err != nil {
		t.Fatalf("projeção menor deveria passar: %v", err)
	}
}

func TestCheckUpdate_SubstituiIntervalo(t *testing.T) {
	store := storage.NewMem()
	const quota = int64(90000)
	svc := New(Limits{MaxEndpointsPerAccount: 10, MinInterval: 10 * time.Second, ChecksPerMonth: quota}, store)

	id1 := mustCreate(t, store, 5, time.Minute) // 43.200
	_ = mustCreate(t, store, 5, 2*time.Minute)  // 21.600 → total 64.800

	// Update do id1 para 30s: substitui 43.200 por 86.400 → total 108.000 > 90k.
	newEp := domain.Endpoint{
		Name: "ep", URL: "https://example.com/h", Method: "GET",
		Interval: 30 * time.Second, Timeout: time.Second, Active: true,
	}
	err := svc.CheckUpdate(context.Background(), 5, id1, newEp)
	if qe, ok := err.(*ErrQuotaExceeded); !ok || qe.Code != "checks_month" {
		t.Fatalf("esperava checks_month no update, got %v", err)
	}

	// Update para 120s: substitui 43.200 por 21.600 → total 43.200 ≤ 90k.
	newEp.Interval = 2 * time.Minute
	if err := svc.CheckUpdate(context.Background(), 5, id1, newEp); err != nil {
		t.Fatalf("update dentro da cota deveria passar: %v", err)
	}

	// Intervalo abaixo do mínimo no update → interval_min.
	newEp.Interval = 2 * time.Second
	err = svc.CheckUpdate(context.Background(), 5, id1, newEp)
	if qe, ok := err.(*ErrQuotaExceeded); !ok || qe.Code != "interval_min" {
		t.Fatalf("esperava interval_min no update, got %v", err)
	}
}

func TestQuota_SemDonoNaoCotado(t *testing.T) {
	svc := New(Limits{MaxEndpointsPerAccount: 1, MinInterval: time.Hour, ChecksPerMonth: 1}, storage.NewMem())
	// ownerID 0 (fluxos internos) ignora todas as cotas — inclusive intervalo.
	if err := svc.CheckCreate(context.Background(), 0, epFor(time.Second)); err != nil {
		t.Fatalf("sem dono não devia ser cotado: %v", err)
	}
}

func TestEffectiveChecksPerMonth_Derivada(t *testing.T) {
	derived := New(Limits{MaxEndpointsPerAccount: 10, MinInterval: time.Minute}, storage.NewMem())
	// 10 × (30d/60s = 43.200) = 432.000
	if got := derived.EffectiveChecksPerMonth(); got != 432000 {
		t.Fatalf("derivada errada: %d (esperava 432000)", got)
	}
	explicit := New(Limits{MaxEndpointsPerAccount: 10, MinInterval: time.Minute, ChecksPerMonth: 500}, storage.NewMem())
	if got := explicit.EffectiveChecksPerMonth(); got != 500 {
		t.Fatalf("explicita errada: %d", got)
	}
}
