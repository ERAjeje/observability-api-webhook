package api

import (
	"net/http"
	"strings"
	"testing"
)

// TestMetricsEndpoint: /metrics responde no formato Prometheus text mesmo sem
// runtime registrado (nil-safe) — RF-012/RNF-011.
func TestMetricsEndpoint(t *testing.T) {
	srv, _ := newSettingsTestServer(t)
	rec := do(srv, http.MethodGet, "/metrics", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("metrics: %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/plain") {
		t.Fatalf("content-type errado: %s", ct)
	}
	for _, want := range []string{
		"# TYPE monitor_checks_total counter",
		"# TYPE monitor_queue_starved_total counter",
		"monitor_worker_pool_size",
		"monitor_go_goroutines",
	} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("metrics não contém %q:\n%s", want, rec.Body.String())
		}
	}
}
