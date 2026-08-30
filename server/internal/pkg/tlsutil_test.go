package pkg

import "testing"

func TestGenerateSelfSigned_StableFingerprint(t *testing.T) {
	certPEM, keyPEM, fp, err := GenerateSelfSigned([]string{"probe.example.com", "203.0.113.10"})
	if err != nil {
		t.Fatalf("GenerateSelfSigned: %v", err)
	}
	if fp == "" || len(fp) != 64 {
		t.Fatalf("fingerprint = %q", fp)
	}
	got, err := LoadCertFingerprint(certPEM)
	if err != nil {
		t.Fatalf("LoadCertFingerprint: %v", err)
	}
	if got != fp {
		t.Fatalf("fingerprint mismatch: %s vs %s", got, fp)
	}
	if string(keyPEM) == "" {
		t.Fatal("key pem empty")
	}
}

func TestLoadCertFingerprint_InvalidPEM(t *testing.T) {
	if _, err := LoadCertFingerprint([]byte("not a pem")); err == nil {
		t.Fatal("invalid pem should error")
	}
}
