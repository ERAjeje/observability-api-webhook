package api

import "net/http"

// metricsHandler expõe as métricas do runtime no formato Prometheus text
// (RF-012, RNF-011). Rota operacional sem auth — coleta por Prometheus;
// proteção por rede na infraestrutura (S-11).
func (s *Server) metricsHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(s.obs.Render())
}
