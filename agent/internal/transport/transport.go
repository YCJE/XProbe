// Package transport builds the HTTP client used for registration, config
// pulls and the WebSocket handshake. In pinning mode the SPKI hash replaces
// the system CA check, but certificate validity dates are still enforced.
package transport

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"

	"probe/agent/internal/config"
)

func HTTPClient(cfg *config.Config) (*http.Client, error) {
	tlsConfig, err := TLSConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// The token is in the Authorization header; never replay it across
			// a redirect chosen by the server or a proxy.
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			TLSClientConfig:       tlsConfig,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          4,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 15 * time.Second,
		},
	}, nil
}

func TLSConfig(cfg *config.Config) (*tls.Config, error) {
	out := &tls.Config{MinVersion: tls.VersionTLS12}
	if cfg.TLSInsecure {
		out.InsecureSkipVerify = true
		return out, nil
	}
	if cfg.TLSPinMode != "spki_sha256" {
		return out, nil
	}
	allowed := make(map[string]bool, len(cfg.ServerCertPins))
	for _, pin := range cfg.ServerCertPins {
		allowed[pin] = true
	}
	out.InsecureSkipVerify = true
	out.VerifyConnection = func(state tls.ConnectionState) error {
		if len(state.PeerCertificates) == 0 {
			return fmt.Errorf("server presented no certificate")
		}
		cert := state.PeerCertificates[0]
		now := time.Now()
		if now.Before(cert.NotBefore) || now.After(cert.NotAfter) {
			return fmt.Errorf("server certificate is not valid now")
		}
		sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
		pin := hex.EncodeToString(sum[:])
		if !allowed[pin] {
			return fmt.Errorf("server SPKI pin mismatch")
		}
		return nil
	}
	return out, nil
}

// SPKIPin is exported for the installer and tests. It is not used at runtime
// except to compare the configured pin.
func SPKIPin(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return hex.EncodeToString(sum[:])
}
