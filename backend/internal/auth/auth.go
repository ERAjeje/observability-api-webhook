// Package auth implementa autenticação por JWT + bcrypt para o painel
// administrativo (T3.1, RF-006, RNF-019). Sem estado no servidor além da
// chave de assinatura — apenas assinar/verificar tokens.
package auth

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// Claims são os dados embarcados no token JWT.
type Claims struct {
	Email string `json:"email"`
	jwt.RegisteredClaims
}

// Config parametriza a emissão/validação de tokens.
type Config struct {
	Secret string        // chave HMAC — obrigatória (matamos em prod se vazia)
	TTL    time.Duration // expiração do token (default 24h)
	Issuer string
}

// Service assina e valida JWTs.
type Service struct {
	secret []byte
	ttl    time.Duration
	issuer string
}

// New constrói o serviço de auth.
func New(cfg Config) (*Service, error) {
	if len(cfg.Secret) < 16 {
		return nil, fmt.Errorf("auth: JWT_SECRET deve ter pelo menos 16 bytes (got %d)", len(cfg.Secret))
	}
	ttl := cfg.TTL
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &Service{secret: []byte(cfg.Secret), ttl: ttl, issuer: cfg.Issuer}, nil
}

// Sign emite um token para o usuário; devolve token e expiração.
func (s *Service) Sign(userID int64, email string) (string, time.Time, error) {
	exp := time.Now().Add(s.ttl)
	claims := Claims{
		Email: email,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   fmt.Sprintf("%d", userID),
			Issuer:    s.issuer,
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
	return tok, exp, err
}

// Parse valida a assinatura e a expiração; devolve os claims.
func (s *Service) Parse(token string) (*Claims, error) {
	parsed, err := jwt.ParseWithClaims(token, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("método de assinatura inesperado: %v", t.Header["alg"])
		}
		return s.secret, nil
	}, jwt.WithIssuer(s.issuer), jwt.WithExpirationRequired())
	if err != nil {
		return nil, err
	}
	claims, ok := parsed.Claims.(*Claims)
	if !ok || !parsed.Valid {
		return nil, errors.New("auth: token inválido")
	}
	return claims, nil
}

// SubjectID devolve o user id dos claims (string) ou erro.
func (c *Claims) SubjectID() (int64, error) {
	if c.Subject == "" {
		return 0, errors.New("auth: subject ausente")
	}
	var id int64
	if _, err := fmt.Sscanf(c.Subject, "%d", &id); err != nil {
		return 0, errors.New("auth: subject inválido")
	}
	return id, nil
}

// HashPassword gera o hash bcrypt (custo default 10).
func HashPassword(pw string) (string, error) {
	if len(pw) < 8 {
		return "", errors.New("senha deve ter pelo menos 8 caracteres")
	}
	if len(pw) > 72 {
		return "", errors.New("senha deve ter no máximo 72 bytes (limite bcrypt)")
	}
	h, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(h), err
}

// CheckPassword compara senha/hash sem vazar qual parte falhou.
func CheckPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

// BearerToken extrai o token do header Authorization ("Bearer <tok>").
func BearerToken(header string) (string, bool) {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return "", false
	}
	tok := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	return tok, tok != ""
}
