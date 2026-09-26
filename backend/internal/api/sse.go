package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"monitor/internal/broker"
)

// sseEventTopics são os tópicos repassados pelo stream.
var sseEventTopics = []string{
	broker.EventStatusChanged,
	broker.EventIncidentOpened,
	broker.EventIncidentClosed,
	broker.EventRollupUpdated,
	broker.EventEndpointRemoved,
}

// sseStream mantém uma conexão text/event-stream (RF-021, RNF-009/010):
// envia o SNAPSHOT completo no connect e, depois, os eventos do broker,
// com heartbeat a cada sseHeartbeat. Nada sensível é serializado (RNF-018).
func (s *Server) sseStream(w http.ResponseWriter, r *http.Request) {
	if s.broker == nil {
		writeErr(w, http.StatusServiceUnavailable, "stream indisponível")
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "sse: streaming não suportado")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // nginx: nunca bufferizar (T1.8)
	w.WriteHeader(http.StatusOK)

	// 1. Snapshot atual (RNF-010 — reconexão sem divergência).
	snap := s.snapshot(r)
	writeSSE(w, "snapshot", snap)
	fl.Flush()

	// 2. Assina todos os tópicos e funde os canais.
	merged := make(chan broker.Event, 128)
	done := make(chan struct{})
	defer close(done)
	for _, topic := range sseEventTopics {
		ch, unsub := s.broker.Subscribe(topic)
		defer unsub()
		go func(src <-chan broker.Event) {
			for {
				select {
				case ev, ok := <-src:
					if !ok {
						return
					}
					select {
					case merged <- ev:
					case <-done:
						return
					}
				case <-done:
					return
				}
			}
		}(ch)
	}

	// 3. Loop: heartbeat + eventos até o cliente desconectar.
	hb := time.NewTicker(s.sseHeartbeat)
	defer hb.Stop()
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-hb.C:
			fmt.Fprintf(w, ": ping\n\n")
			fl.Flush()
		case ev := <-merged:
			if err := writeSSE(w, ev.Type, ev); err != nil {
				return
			}
			fl.Flush()
		}
	}
}

// snapshotSerializado é a visão pública do estado atual.
func (s *Server) snapshot(r *http.Request) map[string]any {
	eps, _ := s.store.ListEndpoints(r.Context())
	groups, _ := s.store.ListGroups(r.Context())
	groupName := map[int64]string{}
	for _, g := range groups {
		groupName[g.ID] = g.Name
	}
	out := make([]map[string]any, 0, len(eps))
	for _, e := range eps {
		name := ""
		if e.GroupID != nil {
			name = groupName[*e.GroupID]
		}
		out = append(out, publicEndpointDTO(e, name))
	}
	open, _ := s.store.ListIncidents(r.Context(), true, 100)
	incidents := make([]map[string]any, 0, len(open))
	for _, inc := range open {
		incidents = append(incidents, incidentDTO(inc))
	}
	return map[string]any{
		"generated_at":   time.Now().UTC(),
		"endpoints":      out,
		"incidents_open": incidents,
	}
}

// writeSSE serializa v como "event: <type>\ndata: <json>\n\n".
func writeSSE(w http.ResponseWriter, evType string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evType, data)
	return err
}
