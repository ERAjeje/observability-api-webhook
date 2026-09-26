package domain

import (
	"fmt"
	"net/mail"
	"time"
)

// User é a conta administrativa do painel (RF-006, RNF-019).
type User struct {
	ID           int64
	Email        string
	PasswordHash string
	CreatedAt    time.Time
}

// ValidateEmail valida o formato do e-mail (RFC 5322) e normaliza
// para minúsculas — evita contas duplicadas por caixa.
func ValidateEmail(email string) (string, error) {
	addr, err := mail.ParseAddress(email)
	if err != nil {
		return "", fmt.Errorf("e-mail inválido: %w", err)
	}
	return normalizeEmail(addr.Address), nil
}

func normalizeEmail(e string) string {
	out := make([]byte, 0, len(e))
	for i := 0; i < len(e); i++ {
		c := e[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		out = append(out, c)
	}
	return string(out)
}
