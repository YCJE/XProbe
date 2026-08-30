package tlsconf

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// sha256Hex 计算证书 DER 指纹(与 Server 端 /server-cert 口径一致)。
func sha256Hex(der []byte) string {
	h := sha256.Sum256(der)
	return hex.EncodeToString(h[:])
}

// genSelfSigned 生成测试用自签证书(含 localhost 与 127.0.0.1)。
func genSelfSigned(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "XProbe Test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1)},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: cert}
}

func TestValidateServerURL(t *testing.T) {
	if _, err := ValidateServerURL("https://probe.example.com"); err != nil {
		t.Fatalf("https ok: %v", err)
	}
	for _, bad := range []string{"http://probe.example.com", "ws://x", "probe.example.com", "ftp://x", "https://"} {
		if _, err := ValidateServerURL(bad); err == nil {
			t.Fatalf("%q should be rejected (S2)", bad)
		}
	}
}

func TestWSURL(t *testing.T) {
	u, _ := ValidateServerURL("https://probe.example.com:8443/base")
	if got := WSURL(u); got != "wss://probe.example.com:8443/base" {
		t.Fatalf("ws url = %s", got)
	}
}

func TestNew_PinnedFingerprintAccepts(t *testing.T) {
	cert := genSelfSigned(t)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	srv.StartTLS()
	defer srv.Close()

	fp := sha256Hex(srv.TLS.Certificates[0].Certificate[0])
	conf := New([]string{fp, "stale-fingerprint-placeholder"}, "127.0.0.1")
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: conf}, Timeout: 5 * time.Second}

	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("pinned fingerprint should pass handshake: %v", err)
	}
	resp.Body.Close()
}

func TestNew_WrongFingerprintRejected(t *testing.T) {
	cert := genSelfSigned(t)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	srv.StartTLS()
	defer srv.Close()

	conf := New([]string{"0000000000000000000000000000000000000000000000000000000000000000"}, "127.0.0.1")
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: conf}, Timeout: 5 * time.Second}
	if _, err := client.Get(srv.URL); err == nil {
		t.Fatal("unpinned self-signed must be rejected (S2)")
	}
}
