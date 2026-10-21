package security

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestGenerateTokenIsOpaque256Bit(t *testing.T) {
	token, err := GenerateToken()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatalf("token is not raw URL base64: %v", err)
	}
	if len(raw) != tokenBytes {
		t.Fatalf("token has %d bytes, want %d", len(raw), tokenBytes)
	}
	other, err := GenerateToken()
	if err != nil {
		t.Fatalf("generate second: %v", err)
	}
	if token == other {
		t.Fatal("two generated tokens are equal")
	}
}

func TestHashTokenIsStableAndNotTheToken(t *testing.T) {
	token := "opaque-token"
	first := HashToken(token)
	if first != HashToken(token) {
		t.Fatal("hash is not stable")
	}
	if len(first) != 64 {
		t.Fatalf("hash length = %d, want 64 hex chars", len(first))
	}
	if first == token {
		t.Fatal("hash equals the plaintext token")
	}
	if HashToken("other") == first {
		t.Fatal("different tokens hash to the same value")
	}
}

func TestRegistrationCodeIsNormalizedBeforeHashing(t *testing.T) {
	code, err := GenerateRegistrationCode()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	normalized := NormalizeRegistrationCode(code)
	if len(normalized) != 26 {
		t.Fatalf("normalized code length = %d, want 26", len(normalized))
	}
	for _, r := range normalized {
		if !(r >= 'A' && r <= 'Z') && !(r >= '2' && r <= '7') {
			t.Fatalf("normalized code contains %q", r)
		}
	}
	if HashRegistrationCode(code) != HashRegistrationCode(strings.ToLower(code)) {
		t.Fatal("case changes the registration-code hash")
	}
	withoutHyphens := strings.ReplaceAll(code, "-", "")
	if HashRegistrationCode(code) != HashRegistrationCode(withoutHyphens) {
		t.Fatal("hyphenation changes the registration-code hash")
	}
	other, err := GenerateRegistrationCode()
	if err != nil {
		t.Fatalf("generate second: %v", err)
	}
	if HashRegistrationCode(code) == HashRegistrationCode(other) {
		t.Fatal("different codes hash to the same value")
	}
}
