package seal

import (
	"encoding/hex"
	"strings"
	"testing"

	"monitor/internal/domain"
)

func testKey(t *testing.T) []byte {
	t.Helper()
	k, err := ParseKey(hex.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestSealOpenRoundtrip(t *testing.T) {
	k := testKey(t)
	blob, err := Seal([]byte(`{"Authorization":"Bearer abc"}`), k)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Open(blob, k)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"Authorization":"Bearer abc"}` {
		t.Fatalf("roundtrip errado: %s", got)
	}
	// nonces iguais nunca devem repetir (mesmo conteúdo → blobs distintos)
	blob2, _ := Seal([]byte(`x`), k)
	if blob == blob2 {
		t.Fatal("nonce repetido")
	}
}

func TestOpenDetectaAdulteracao(t *testing.T) {
	k := testKey(t)
	blob, _ := Seal([]byte(`secret`), k)
	b := []byte(blob)
	b[len(b)-1] ^= 0x01 // corrompe o ciphertext
	if _, err := Open(string(b), k); err != ErrTampered {
		t.Fatalf("esperava ErrTampered, got %v", err)
	}
	// chave errada também falha
	k2 := make([]byte, 32)
	k2[0] = 0x01
	if _, err := Open(blob, k2); err != ErrTampered {
		t.Fatalf("chave errada devia falhar, got %v", err)
	}
}

func TestParseKey(t *testing.T) {
	if k, err := ParseKey(""); err != nil || k != nil {
		t.Fatalf("vazio deve ser nil: %v %v", k, err)
	}
	if _, err := ParseKey("hex:abcd"); err == nil {
		t.Fatal("hex curto deveria falhar")
	}
	if _, err := ParseKey("hex:" + strings.Repeat("ab", 32)); err != nil {
		t.Fatalf("hex 64 ok: %v", err)
	}
	if _, err := ParseKey(strings.Repeat("cd", 16)); err == nil {
		t.Fatal("tamanho errado deveria falhar")
	}
	if _, err := ParseKey("nothex!"); err == nil {
		t.Fatal("hex inválido deveria falhar")
	}
}

func TestEndpointHeadersCifradoRoundtrip(t *testing.T) {
	k := testKey(t)
	ep := domain.Endpoint{ID: 1, Headers: map[string]string{"Authorization": "Bearer segredo"}}
	enc, err := EncryptEndpointHeaders(&ep, k)
	if err != nil || !enc {
		t.Fatalf("encrypt: %v %v", enc, err)
	}
	if !IsSealed(ep.Headers) {
		t.Fatal("envelope ausente")
	}
	if strings.Contains(ep.Headers[envelopeKey], "segredo") {
		t.Fatal("segredo vazou no blob")
	}
	// noop sem headers / sem chave
	ep2 := domain.Endpoint{Headers: map[string]string{}}
	if enc, _ := EncryptEndpointHeaders(&ep2, k); enc {
		t.Fatal("sem headers não devia cifrar")
	}
	ep3 := domain.Endpoint{Headers: map[string]string{"a": "b"}}
	if enc, _ := EncryptEndpointHeaders(&ep3, nil); enc {
		t.Fatal("sem chave não devia cifrar")
	}

	// decrypt
	blob := ep.Headers[envelopeKey] // guarda antes de descriptografar
	dec, err := DecryptEndpointHeaders(&ep, k)
	if err != nil || !dec {
		t.Fatalf("decrypt: %v %v", dec, err)
	}
	if ep.Headers["Authorization"] != "Bearer segredo" {
		t.Fatalf("headers não restauraram: %v", ep.Headers)
	}

	// fail-closed: sem chave → ErrNoKey e blob removido (nada vaza)
	ep4 := domain.Endpoint{Headers: map[string]string{envelopeKey: blob}}
	if _, err := DecryptEndpointHeaders(&ep4, nil); err != ErrNoKey {
		t.Fatalf("sem chave devia ser ErrNoKey, got %v", err)
	}
	if _, has := ep4.Headers[envelopeKey]; has {
		t.Fatal("blob deveria ser removido no fail-closed")
	}
}
