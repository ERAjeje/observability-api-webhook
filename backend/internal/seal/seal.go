// Package seal protege segredos em repouso (S-05): headers de autenticação
// dos endpoints são cifrados com AES-256-GCM (AEAD) antes de persistir no
// jsonb `endpoints.headers` e descriptografados apenas onde precisam ser
// usados (engine → checker) ou exibidos (painel admin autenticado).
//
// Formato em repouso: {"$seal_v1": "<base64(nonce12+AEAD(JSON))>"} — a chave
// de envelope é improvável em cabeçalhos HTTP reais (nomes são tokens ASCII).
// Sem HEADERS_ENC_KEY configurada o sistema opera em modo texto plano (dev);
// em produção, omitir a chave com dados já cifrados é fail-closed (segue sem
// headers, nunca vaza o blob).
package seal

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"monitor/internal/domain"
)

// envelopeKey marca o blob cifrado dentro do map de headers (jsonb).
const envelopeKey = "$seal_v1"

// Erros públicos (fail-closed em produção).
var (
	ErrNoKey     = errors.New("seal: HEADERS_ENC_KEY não configurada")
	ErrTampered  = errors.New("seal: dados adulterados ou chave incorreta")
	ErrBadKeyLen = fmt.Errorf("seal: chave deve ter 32 bytes")
)

// KeyLen é o tamanho da chave AES-256.
const KeyLen = 32

// ParseKey aceita "hex:<64 hex chars>" ou uma string de exatamente 64 hex
// chars (32 bytes). Vazia → nil (modo plaintext/dev).
func ParseKey(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	s = strings.TrimPrefix(s, "hex:")
	raw, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("seal: HEADERS_ENC_KEY deve ser hex (64 chars): %w", err)
	}
	if len(raw) != KeyLen {
		return nil, ErrBadKeyLen
	}
	return raw, nil
}

// Seal cifra plaintext com AES-256-GCM (nonce aleatório inclusa na saída).
func Seal(plaintext, key []byte) (string, error) {
	if len(key) != KeyLen {
		return "", ErrBadKeyLen
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("seal: falha no RNG: %w", err)
	}
	sealed := gcm.Seal(nil, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(append(nonce, sealed...)), nil
}

// Open decodifica e verifica o blob (integridade + confidencialidade).
func Open(blob string, key []byte) ([]byte, error) {
	if len(key) != KeyLen {
		return nil, ErrBadKeyLen
	}
	raw, err := base64.StdEncoding.DecodeString(blob)
	if err != nil {
		return nil, ErrTampered
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(raw) < gcm.NonceSize() {
		return nil, ErrTampered
	}
	nonce, ct := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, ErrTampered
	}
	return pt, nil
}

// IsSealed reporta se o map de headers contém o envelope cifrado.
func IsSealed(h map[string]string) bool {
	_, ok := h[envelopeKey]
	return ok
}

// EncryptEndpointHeaders cifra o map e o substitui pelo envelope. Retorna
// false (noop) quando não há headers ou não há chave. (S-05 — escrita)
func EncryptEndpointHeaders(ep *domain.Endpoint, key []byte) (bool, error) {
	if len(ep.Headers) == 0 || len(key) != KeyLen {
		return false, nil
	}
	raw, err := json.Marshal(ep.Headers)
	if err != nil {
		return false, err
	}
	blob, err := Seal(raw, key)
	if err != nil {
		return false, err
	}
	ep.Headers = map[string]string{envelopeKey: blob}
	return true, nil
}

// DecryptEndpointHeaders desfaz o envelope. ok=false quando o map não estava
// cifrado (texto plano). Fail-closed: sem chave com blob presente → ErrNoKey
// e o blob é removido (os headers NUNCA vazam descriptografados).
func DecryptEndpointHeaders(ep *domain.Endpoint, key []byte) (bool, error) {
	blob, sealed := ep.Headers[envelopeKey]
	if !sealed {
		return false, nil
	}
	if len(key) != KeyLen {
		delete(ep.Headers, envelopeKey)
		return false, ErrNoKey
	}
	raw, err := Open(blob, key)
	if err != nil {
		return false, err
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		return false, ErrTampered
	}
	if m == nil {
		m = map[string]string{}
	}
	ep.Headers = m
	return true, nil
}
