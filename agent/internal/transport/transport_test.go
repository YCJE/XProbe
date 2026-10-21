package transport

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"probe/agent/internal/config"
)

func TestSPKIPinning(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	pin := SPKIPin(server.Certificate())
	client, err := HTTPClient(&config.Config{
		TLSPinMode:     "spki_sha256",
		ServerCertPins: []string{pin},
	})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("pinned request failed: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	wrong, err := HTTPClient(&config.Config{
		TLSPinMode:     "spki_sha256",
		ServerCertPins: []string{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wrong.Get(server.URL); err == nil {
		t.Fatal("request with a wrong pin succeeded")
	}
}
