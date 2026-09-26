package domain

import (
	"fmt"
	"strings"
	"time"
)

// CheckGroup agrupa endpoints para organização na status page (RF-005).
type CheckGroup struct {
	ID           int64
	Name         string
	DisplayOrder int
	CreatedAt    time.Time
}

// Validate valida as regras de cadastro de grupo.
func (g *CheckGroup) Validate() error {
	if strings.TrimSpace(g.Name) == "" {
		return fmt.Errorf("nome do grupo é obrigatório")
	}
	if len([]rune(g.Name)) > 64 {
		return fmt.Errorf("nome do grupo deve ter no máximo 64 caracteres")
	}
	if g.DisplayOrder < 0 {
		return fmt.Errorf("display_order deve ser >= 0")
	}
	return nil
}
