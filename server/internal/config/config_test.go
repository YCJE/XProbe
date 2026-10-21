package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultsAreValidAndLoopbackOnly(t *testing.T) {
	cfg := Defaults()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("defaults must validate: %v", err)
	}
	if cfg.PublicURL != "http://127.0.0.1:8080" {
		t.Fatalf("public_url default = %q", cfg.PublicURL)
	}
}

func TestPublicListenerRequiresTLS(t *testing.T) {
	cfg := Defaults()
	cfg.Listen = "0.0.0.0:8080"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected tls.mode=off on a public listener to be refused")
	}
	cfg.TLS = TLSConfig{Mode: "files", Cert: "cert.pem", Key: "key.pem"}
	cfg.PublicURL = "https://monitor.example.com"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("files TLS should validate: %v", err)
	}
}

func TestCloudflareTunnelRequiresLoopbackAndNoTLS(t *testing.T) {
	cfg := Defaults()
	cfg.Proxy.Mode = "cloudflare_tunnel"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("loopback tunnel should validate: %v", err)
	}
	cfg.Listen = "0.0.0.0:8080"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected public listener to be refused")
	}
}

func TestCloudflareProxyRequiresOriginTLSAndCIDRs(t *testing.T) {
	cfg := Defaults()
	cfg.Listen = "0.0.0.0:443"
	cfg.PublicURL = "https://monitor.example.com"
	cfg.TLS = TLSConfig{Mode: "files", Cert: "cert.pem", Key: "key.pem"}
	cfg.Proxy.Mode = "cloudflare_proxy"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected missing trusted CIDRs to be refused")
	}
	cfg.Proxy.TrustedCIDRs = []string{"173.245.48.0/20"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("cloudflare_proxy should validate: %v", err)
	}
}

func TestHTTPPublicURLIsRefused(t *testing.T) {
	cfg := Defaults()
	cfg.PublicURL = "http://monitor.example.com"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected public http URL to be refused")
	}
}

func TestLoadPartialYAMLKeepsDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	body := []byte("listen: 127.0.0.1:9090\nauth:\n  session_idle: 30m\n")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Listen != "127.0.0.1:9090" {
		t.Fatalf("listen = %q", cfg.Listen)
	}
	if cfg.Auth.SessionIdle.Duration.Minutes() != 30 {
		t.Fatalf("session_idle = %s", cfg.Auth.SessionIdle.Duration)
	}
	if cfg.Auth.SessionMax.Duration.Hours() != 168 {
		t.Fatalf("session_max default lost: %s", cfg.Auth.SessionMax.Duration)
	}
}
