// Guarda contra SSRF (Server-Side Request Forgery).
//
// O checker executa requisições para URLs cadastradas pelo usuário. Em um
// deploy público isso permite apontar o monitor para a rede interna do host
// (loopback, RFC1918) ou para o metadata cloud (169.254.169.254) — exfiltração
// de credenciais IAM e varredura interna. Este módulo valida o destino
// resolvido por IP e bloqueia faixas sensíveis por padrão.
package checker

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"
)

// ErrForbiddenTarget é retornado quando o destino é uma faixa bloqueada.
var ErrForbiddenTarget = errors.New("alvo bloqueado: endereço interno/privado não permitido (defina ALLOW_PRIVATE_TARGETS=true para ambientes controlados)")

// ValidateTarget resolve o host e rejeita IPs sensíveis quando allowPrivate
// é falso: loopback, link-local, multicast, RFC1918/ULA e metadata cloud.
func ValidateTarget(rawURL string, allowPrivate bool) error {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return fmt.Errorf("destino inválido: %q", rawURL)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme não permitido: %s", u.Scheme)
	}
	if allowPrivate {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	host := u.Hostname()
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(addrs) == 0 {
		return fmt.Errorf("falha ao resolver destino %q: %v", host, err)
	}
	for _, a := range addrs {
		if ipBlocked(a.IP) {
			return fmt.Errorf("%w: %s → %s", ErrForbiddenTarget, host, a.IP)
		}
	}
	return nil
}

func ipBlocked(ip net.IP) bool {
	switch {
	case ip.IsLoopback(), ip.IsLinkLocalUnicast(), ip.IsLinkLocalMulticast(),
		ip.IsMulticast(), ip.IsUnspecified(), ip.IsPrivate():
		return true
	case ip.Equal(net.ParseIP("169.254.169.254")): // metadata cloud — redundância explícita
		return true
	}
	return false
}

// guardedTransport valida o destino a cada RoundTrip (momento do dial),
// mitigando DNS rebinding entre a validação e a conexão real. O controle
// definitivo é um firewall de egress na VPS — documentado no security-review.
type guardedTransport struct {
	base         http.RoundTripper
	allowPrivate bool
}

func (g *guardedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := ValidateTarget(req.URL.String(), g.allowPrivate); err != nil {
		return nil, err
	}
	return g.base.RoundTrip(req)
}
