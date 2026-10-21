package security

import (
	"path/filepath"
	"testing"
)

func TestSecretBoxRoundTripAndTamper(t *testing.T) {
	box, err := LoadOrCreateKey(filepath.Join(t.TempDir(), "totp.key"))
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	encoded, err := box.Encrypt("JBSWY3DPEHPK3PXP")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	plain, err := box.Decrypt(encoded)
	if err != nil || plain != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("decrypt = %q, %v", plain, err)
	}
	if _, err := box.Decrypt(encoded[:len(encoded)-2] + "AA"); err == nil {
		t.Fatal("tampered ciphertext was accepted")
	}
}

func TestKeyPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "totp.key")
	first, err := LoadOrCreateKey(path)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := first.Encrypt("secret")
	second, err := LoadOrCreateKey(path)
	if err != nil {
		t.Fatal(err)
	}
	if plain, err := second.Decrypt(encoded); err != nil || plain != "secret" {
		t.Fatalf("key did not persist: %q %v", plain, err)
	}
}
