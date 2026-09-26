package api

import (
	"net/http"

	"monitor/internal/settings"
)

// settingsPayload agrupa branding + alerts para GET/PUT admin.
type settingsPayload struct {
	Branding *settings.Branding `json:"branding"`
	Alerts   *settings.Alerts   `json:"alerts"`
}

// publicConfig expõe SOMENTE o branding da status page (RF-023) — rota
// pública sem qualquer dado sensível (RNF-018).
func (s *Server) publicConfig(w http.ResponseWriter, r *http.Request) {
	if s.settings == nil {
		writeJSON(w, http.StatusOK, map[string]any{"branding": settings.DefaultBranding()})
		return
	}
	b, err := s.settings.Branding(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "falha ao ler configuração")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"branding": b})
}

// getSettings devolve branding + alerts (admin — T4.5/RF-023).
func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	if s.settings == nil {
		writeErr(w, http.StatusServiceUnavailable, "settings indisponível")
		return
	}
	b, err := s.settings.Branding(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "falha ao ler branding")
		return
	}
	a, _, err := s.settings.Alerts(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "falha ao ler alertas")
		return
	}
	writeJSON(w, http.StatusOK, settingsPayload{Branding: &b, Alerts: &a})
}

// putSettings valida e persiste branding/alerts (admin).
func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	if s.settings == nil {
		writeErr(w, http.StatusServiceUnavailable, "settings indisponível")
		return
	}
	var p settingsPayload
	if !decodeJSON(w, r, &p) {
		return
	}
	if p.Branding != nil {
		b, err := settings.ValidateBranding(*p.Branding)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := s.settings.Set(r.Context(), settings.KeyBranding, b); err != nil {
			writeErr(w, http.StatusInternalServerError, "falha ao salvar branding")
			return
		}
	}
	if p.Alerts != nil {
		a, err := settings.ValidateAlerts(*p.Alerts, s.settings.AllowPrivate())
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := s.settings.Set(r.Context(), settings.KeyAlerts, a); err != nil {
			writeErr(w, http.StatusInternalServerError, "falha ao salvar alertas")
			return
		}
	}

	// Devolve o estado completo atualizado.
	b, _ := s.settings.Branding(r.Context())
	a, _, _ := s.settings.Alerts(r.Context())
	writeJSON(w, http.StatusOK, settingsPayload{Branding: &b, Alerts: &a})
}
