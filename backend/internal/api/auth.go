package api

import (
	"context"
	"net/http"
	"strings"

	"monitor/internal/auth"
	"monitor/internal/domain"
)

// ctxKey evita colisões no request context.
type ctxKey int

const (
	ctxUserID ctxKey = iota
	ctxUserEmail
)

// signupBody é o payload de criação de conta (RF-006).
type signupBody struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// signup cria a conta administrativa (rate limit por IP — RNF-017).
func (s *Server) signup(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		writeErr(w, http.StatusServiceUnavailable, "autenticação não configurada")
		return
	}
	if !s.authLimiter.Allow("signup:" + clientIP(r)) {
		writeErr(w, http.StatusTooManyRequests, "muitas tentativas — aguarde e tente novamente")
		return
	}
	var b signupBody
	if !decodeJSON(w, r, &b) {
		return
	}
	email, err := domain.ValidateEmail(b.Email)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	hash, err := auth.HashPassword(b.Password)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	id, err := s.store.CreateUser(r.Context(), domain.User{Email: email, PasswordHash: hash})
	if mapStoreErr(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "email": email})
}

// loginBody troca e-mail/senha por um JWT (RF-006).
type loginBody struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// login autentica e emite o token (rate limit por IP+e-mail).
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		writeErr(w, http.StatusServiceUnavailable, "autenticação não configurada")
		return
	}
	key := "login:" + clientIP(r)
	var b loginBody
	if !decodeJSON(w, r, &b) {
		return
	}
	email, err := domain.ValidateEmail(b.Email)
	if err != nil {
		// Resposta genérica: não revela se o e-mail existe (RNF-019).
		writeErr(w, http.StatusUnauthorized, "credenciais inválidas")
		return
	}
	if !s.authLimiter.Allow(key + ":" + email) {
		writeErr(w, http.StatusTooManyRequests, "muitas tentativas — aguarde e tente novamente")
		return
	}
	u, err := s.store.GetUserByEmail(r.Context(), email)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "credenciais inválidas")
		return
	}
	if !auth.CheckPassword(u.PasswordHash, b.Password) {
		writeErr(w, http.StatusUnauthorized, "credenciais inválidas")
		return
	}
	token, exp, err := s.auth.Sign(u.ID, u.Email)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "falha ao emitir token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token":      token,
		"expires_at": exp,
	})
}

// requireAuth protege as rotas admin (401 sem token válido — RF-006).
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.auth == nil {
			writeErr(w, http.StatusUnauthorized, "autenticação não configurada")
			return
		}
		tok, ok := auth.BearerToken(r.Header.Get("Authorization"))
		if !ok {
			writeErr(w, http.StatusUnauthorized, "token ausente")
			return
		}
		claims, err := s.auth.Parse(tok)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "token inválido ou expirado")
			return
		}
		id, err := claims.SubjectID()
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "token inválido")
			return
		}
		ctx := context.WithValue(r.Context(), ctxUserID, id)
		ctx = context.WithValue(ctx, ctxUserEmail, claims.Email)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// userIDFrom devolve o dono autenticado montado por requireAuth (S-08).
// 0 = contexto sem identidade (fluxo não autenticado).
func userIDFrom(ctx context.Context) int64 {
	id, _ := ctx.Value(ctxUserID).(int64)
	return id
}

// clientIP devolve o IP do cliente (usa X-Forwarded-For quando atrás do Nginx).
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		for _, part := range strings.Split(xff, ",") {
			if p := strings.TrimSpace(part); p != "" {
				return p
			}
		}
	}
	return r.RemoteAddr
}
