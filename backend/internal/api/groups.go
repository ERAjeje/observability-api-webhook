package api

import (
	"net/http"

	"monitor/internal/domain"
)

// groupBody é o payload de criação/edição de grupo (RF-005).
type groupBody struct {
	Name         string `json:"name"`
	DisplayOrder int    `json:"display_order"`
}

// listGroups devolve os grupos (admin).
func (s *Server) listGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := s.store.ListGroups(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "falha ao listar grupos")
		return
	}
	out := make([]map[string]any, 0, len(groups))
	for _, g := range groups {
		out = append(out, groupDTO(g))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (s *Server) createGroup(w http.ResponseWriter, r *http.Request) {
	var b groupBody
	if !decodeJSON(w, r, &b) {
		return
	}
	g := domain.CheckGroup{Name: b.Name, DisplayOrder: b.DisplayOrder}
	if err := g.Validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	id, err := s.store.CreateGroup(r.Context(), g)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "falha ao criar grupo")
		return
	}
	g.ID = id
	writeJSON(w, http.StatusCreated, groupDTO(g))
}

func (s *Server) getGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	g, err := s.store.GetGroup(r.Context(), id)
	if mapStoreErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, groupDTO(g))
}

func (s *Server) updateGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	var b groupBody
	if !decodeJSON(w, r, &b) {
		return
	}
	g := domain.CheckGroup{ID: id, Name: b.Name, DisplayOrder: b.DisplayOrder}
	if err := g.Validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.UpdateGroup(r.Context(), g); mapStoreErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, groupDTO(g))
}

func (s *Server) deleteGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteGroup(r.Context(), id); mapStoreErr(w, err) {
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}

func groupDTO(g domain.CheckGroup) map[string]any {
	return map[string]any{
		"id":            g.ID,
		"name":          g.Name,
		"display_order": g.DisplayOrder,
		"created_at":    g.CreatedAt,
	}
}
