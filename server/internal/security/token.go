// Package security holds credential generation and hashing primitives shared
// by registration, agent authentication and admin sessions.
package security

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
)

const (
	tokenBytes            = 32
	registrationCodeBytes = 16
)

// GenerateToken returns a 256-bit opaque token. It is shown to the caller once
// and stored only as HashToken(token).
func GenerateToken() (string, error) {
	b, err := randomBytes(tokenBytes)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// RandomID returns a 128-bit hex identifier for session rows. It is not a
// credential and is safe to show in the session list.
func RandomID() (string, error) {
	b, err := randomBytes(16)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// HashToken returns the lowercase hex SHA-256 of a high-entropy secret.
// SHA-256 is appropriate here because the input is random, not a password.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// GenerateRegistrationCode returns a 128-bit code grouped for hand copying:
// XXXX-XXXX-XXXX-XXXX-XXXX-XXXX-XX.
func GenerateRegistrationCode() (string, error) {
	b, err := randomBytes(registrationCodeBytes)
	if err != nil {
		return "", err
	}
	raw := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)
	return groupRegistrationCode(raw), nil
}

// NormalizeRegistrationCode makes hyphenation and case irrelevant at
// verification time. The normalized form is what gets hashed.
func NormalizeRegistrationCode(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	code = strings.ReplaceAll(code, "-", "")
	code = strings.ReplaceAll(code, " ", "")
	return code
}

// HashRegistrationCode hashes the normalized code so the database never holds
// a value an operator could reuse.
func HashRegistrationCode(code string) string {
	return HashToken(NormalizeRegistrationCode(code))
}

func randomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return nil, fmt.Errorf("read random bytes: %w", err)
	}
	return b, nil
}

func groupRegistrationCode(raw string) string {
	var b strings.Builder
	b.Grow(len(raw) + len(raw)/4)
	for i, r := range raw {
		if i > 0 && i%4 == 0 {
			b.WriteByte('-')
		}
		b.WriteRune(r)
	}
	return b.String()
}
