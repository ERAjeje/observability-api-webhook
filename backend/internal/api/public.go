package api

import (
	"net/http"
	"time"

	"monitor/internal/domain"
)

// publicEndpointDTO é a visão pública de um endpoint (RNF-018 — SEM
// url/headers/body/config).
func publicEndpointDTO(e domain.Endpoint, groupName string) map[string]any {
	return map[string]any{
		"id":         e.ID,
		"name":       e.Name,
		"group_id":   e.GroupID,
		"group_name": groupName,
		"status":     e.Status,
	}
}

// publicStatus devolve o estado atual de todos os endpoints + incidentes
// abertos (RF-019, RF-022) — rota pública, sem auth (RNF-017).
func (s *Server) publicStatus(w http.ResponseWriter, r *http.Request) {
	eps, err := s.store.ListEndpoints(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "falha ao ler endpoints")
		return
	}
	groups, _ := s.store.ListGroups(r.Context())
	groupName := map[int64]string{}
	for _, g := range groups {
		groupName[g.ID] = g.Name
	}
	open, err := s.store.ListIncidents(r.Context(), true, 100)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "falha ao ler incidentes")
		return
	}
	incidents := make([]map[string]any, 0, len(open))
	for _, inc := range open {
		incidents = append(incidents, incidentDTO(inc))
	}
	out := make([]map[string]any, 0, len(eps))
	for _, e := range eps {
		name := ""
		if e.GroupID != nil {
			name = groupName[*e.GroupID]
		}
		out = append(out, publicEndpointDTO(e, name))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"generated_at":   time.Now().UTC(),
		"endpoints":      out,
		"incidents_open": incidents,
	})
}

// publicIncidents devolve a timeline histórica (RF-022).
func (s *Server) publicIncidents(w http.ResponseWriter, r *http.Request) {
	incidents, err := s.store.ListIncidents(r.Context(), false, 200)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "falha ao ler incidentes")
		return
	}
	out := make([]map[string]any, 0, len(incidents))
	for _, inc := range incidents {
		out = append(out, incidentDTO(inc))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func incidentDTO(inc domain.Incident) map[string]any {
	return map[string]any{
		"id":          inc.ID,
		"endpoint_id": inc.EndpointID,
		"started_at":  inc.StartedAt,
		"ended_at":    inc.EndedAt,
		"duration_ms": inc.DurationMS,
		"resolution":  inc.Resolution,
	}
}
