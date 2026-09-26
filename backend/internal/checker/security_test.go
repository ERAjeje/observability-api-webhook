package checker

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Com allowPrivate=false, faixas sensíveis devem ser rejeitadas.
func TestValidateTarget_BloqueiaDestinosInternos(t *testing.T) {
	cases := []string{
		"http://127.0.0.1:8080/healthz",            // loopback IPv4
		"http://localhost/",                        // loopback nome
		"http://10.0.0.5/",                         // RFC1918-A
		"http://192.168.1.10/",                     // RFC1918-C
		"http://[::1]/",                            // loopback IPv6
		"http://169.254.169.254/latest/meta-data/", // link-local / metadata cloud
	}
	for _, raw := range cases {
		if err := ValidateTarget(raw, false); !errors.Is(err, ErrForbiddenTarget) {
			t.Errorf("%s: esperado ErrForbiddenTarget; got %v", raw, err)
		}
	}
}

// Hosts irresolvíveis também são rejeitados (nunca acessar destino incerto).
func TestValidateTarget_RejeitaHostIrresolvivel(t *testing.T) {
	if err := ValidateTarget("https://metadata.google.internal/", false); err == nil {
		t.Fatal("host irresolvível deveria ser rejeitado")
	}
}

func TestValidateTarget_SchemeBloqueado(t *testing.T) {
	if err := ValidateTarget("file:///etc/passwd", false); err == nil {
		t.Fatal("scheme file deveria ser rejeitado")
	}
	if err := ValidateTarget("ftp://example.com/", false); err == nil {
		t.Fatal("scheme ftp deveria ser rejeitado")
	}
}

// A guarda atua no RoundTrip: o client nunca chega a tocar rede interna.
func TestRun_GuardaAtuaNoRoundTrip(t *testing.T) {
	c := New(false) // allowPrivate=false
	_, err := c.client.Get("http://127.0.0.1:9/")
	if !errors.Is(err, ErrForbiddenTarget) {
		t.Fatalf("esperado ErrForbiddenTarget no transporte; got %v", err)
	}
}

// allowPrivate=true libera faixas internas (lab/self-host controlado).
func TestRun_AllowPrivateHabilitaLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(true)
	resp, err := c.client.Get(srv.URL)
	if err != nil {
		t.Fatalf("allowPrivate=true deveria permitir loopback: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status esperado 200; got %d", resp.StatusCode)
	}
}

// Host público válido passa na validação.
func TestValidateTarget_AceitaHostPublico(t *testing.T) {
	if err := ValidateTarget("https://example.com/", false); err != nil {
		t.Fatalf("example.com deveria passar; got %v", err)
	}
}
