package pkg

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestRandomToken_UniqueAndLength(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		tok, err := RandomToken()
		if err != nil {
			t.Fatalf("RandomToken: %v", err)
		}
		if len(tok) != 64 {
			t.Fatalf("token length = %d, want 64 hex chars", len(tok))
		}
		if seen[tok] {
			t.Fatal("token collision")
		}
		seen[tok] = true
	}
}

func TestSHA256Hex_KnownVector(t *testing.T) {
	// sha256("abc") = ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad
	if got := SHA256Hex("abc"); got != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatalf("sha256 = %s", got)
	}
}

func TestConstantTimeHexEqual(t *testing.T) {
	a := SHA256Hex("x")
	if !ConstantTimeHexEqual(a, a) {
		t.Fatal("equal values should match")
	}
	if ConstantTimeHexEqual(a, SHA256Hex("y")) {
		t.Fatal("different values should not match")
	}
	if ConstantTimeHexEqual(a, "short") {
		t.Fatal("length mismatch should not match")
	}
}

func TestPasswordHashRoundTrip(t *testing.T) {
	h, err := HashPassword("S3curePassw0rd!")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !CheckPassword(h, "S3curePassw0rd!") {
		t.Fatal("correct password should verify")
	}
	if CheckPassword(h, "wrong") {
		t.Fatal("wrong password should not verify")
	}
}

func TestLimiter_FixedWindow(t *testing.T) {
	l := NewLimiter(3, time.Minute)
	now := time.Unix(0, 0)
	l.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		if !l.Allow("ip1") {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
	if l.Allow("ip1") {
		t.Fatal("4th request in window should be blocked")
	}
	if l.Remaining("ip1") != 0 {
		t.Fatalf("remaining = %d, want 0", l.Remaining("ip1"))
	}

	// 窗口过期后重置
	now = now.Add(2 * time.Minute)
	if !l.Allow("ip1") {
		t.Fatal("request after window should be allowed")
	}
	// 不同 key 互不影响
	if !l.Allow("ip2") {
		t.Fatal("other key should be allowed")
	}
}

func TestSecurityHeaders_Present(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(SecurityHeaders())
	r.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))

	want := map[string]string{
		"Strict-Transport-Security": "max-age=31536000; includeSubDomains",
		"X-Frame-Options":           "DENY",
		"X-Content-Type-Options":    "nosniff",
		"Referrer-Policy":           "strict-origin-when-cross-origin",
	}
	for k, v := range want {
		if w.Header().Get(k) != v {
			t.Fatalf("header %s = %q, want %q", k, w.Header().Get(k), v)
		}
	}
	if !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatal("CSP missing frame-ancestors")
	}
}
