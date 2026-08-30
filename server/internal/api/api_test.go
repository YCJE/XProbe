package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/YCJE/XProbe/internal/model"
	"github.com/YCJE/XProbe/server/internal/pkg"
	"github.com/YCJE/XProbe/server/internal/repository"
	"github.com/YCJE/XProbe/server/internal/service"
)

// newTestRouter 组装带临时数据库的完整路由, 返回 router 与依赖(供断言)。
type fixture struct {
	router   *gin.Engine
	registry *service.Registry
	hub      *service.Hub
	agents   *repository.AgentRepo
}

func newTestRouter(t *testing.T, registerLimit int) *fixture {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := repository.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := repository.Migrate(context.Background(), db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	agents := repository.NewAgentRepo(db)
	codes := repository.NewRegisterCodeRepo(db)
	targets := repository.NewPingTargetRepo(db)
	registry := service.NewRegistry(agents, codes)
	hub := service.NewHub(agents, 90*time.Second)

	d := Deps{
		Registry:        registry,
		Hub:             hub,
		Agents:          agents,
		PingTargets:     targets,
		CertFingerprint: "ab12",
		RegisterLimiter: pkg.NewLimiter(registerLimit, time.Minute),
		GlobalLimiter:   pkg.NewLimiter(1000, time.Minute),
	}
	return &fixture{router: NewRouter(d, nil), registry: registry, hub: hub, agents: agents}
}

func postJSON(t *testing.T, r http.Handler, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestRegisterEndpoint_HappyPath(t *testing.T) {
	f := newTestRouter(t, 100)
	code, _, err := f.registry.IssueCode(context.Background())
	if err != nil {
		t.Fatalf("IssueCode: %v", err)
	}

	w := postJSON(t, f.router, "/api/v1/agent/register", model.RegisterRequest{
		RegisterCode: code, Hostname: "web-01", HostFingerprint: fpHash("fp-1"),
		OS: "Ubuntu", Arch: "x86_64", AgentVersion: "dev",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	var resp model.RegisterResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil || resp.Token == "" {
		t.Fatalf("resp = %s err=%v", w.Body.String(), err)
	}
}

func TestRegisterEndpoint_ErrorCodes(t *testing.T) {
	f := newTestRouter(t, 100)
	ctx := context.Background()

	// 已用码 → 401
	code, _, _ := f.registry.IssueCode(ctx)
	if w := postJSON(t, f.router, "/api/v1/agent/register",
		model.RegisterRequest{RegisterCode: code, Hostname: "h", HostFingerprint: fpHash("a")}); w.Code != http.StatusOK {
		t.Fatalf("first register = %d", w.Code)
	}
	if w := postJSON(t, f.router, "/api/v1/agent/register",
		model.RegisterRequest{RegisterCode: code, Hostname: "h", HostFingerprint: fpHash("b")}); w.Code != http.StatusUnauthorized {
		t.Fatalf("used code = %d, want 401", w.Code)
	}

	// 未知码 → 401
	if w := postJSON(t, f.router, "/api/v1/agent/register",
		model.RegisterRequest{RegisterCode: "ZZZZZZZZZZ", Hostname: "h", HostFingerprint: fpHash("c")}); w.Code != http.StatusUnauthorized {
		t.Fatalf("unknown code = %d, want 401", w.Code)
	}

	// 指纹冲突 → 409
	code2, _, _ := f.registry.IssueCode(ctx)
	postJSON(t, f.router, "/api/v1/agent/register",
		model.RegisterRequest{RegisterCode: code2, Hostname: "h", HostFingerprint: fpHash("dup")})
	code3, _, _ := f.registry.IssueCode(ctx)
	if w := postJSON(t, f.router, "/api/v1/agent/register",
		model.RegisterRequest{RegisterCode: code3, Hostname: "h2", HostFingerprint: fpHash("dup")}); w.Code != http.StatusConflict {
		t.Fatalf("conflict = %d, want 409", w.Code)
	}

	// 非法请求体 → 400
	if w := postJSON(t, f.router, "/api/v1/agent/register",
		model.RegisterRequest{RegisterCode: "", Hostname: "", HostFingerprint: ""}); w.Code != http.StatusBadRequest {
		t.Fatalf("invalid = %d, want 400", w.Code)
	}
}

func TestRegisterEndpoint_IPRateLimit(t *testing.T) {
	f := newTestRouter(t, 3) // 3 次/分钟/IP
	for i := 0; i < 3; i++ {
		if w := postJSON(t, f.router, "/api/v1/agent/register",
			model.RegisterRequest{RegisterCode: "BADBADBAD0", Hostname: "h", HostFingerprint: fpHash("x")}); w.Code != http.StatusUnauthorized {
			t.Fatalf("req %d = %d, want 401", i+1, w.Code)
		}
	}
	if w := postJSON(t, f.router, "/api/v1/agent/register",
		model.RegisterRequest{RegisterCode: "BADBADBAD0", Hostname: "h", HostFingerprint: fpHash("x")}); w.Code != http.StatusTooManyRequests {
		t.Fatalf("4th req = %d, want 429", w.Code)
	}
}

func TestServerCertEndpoint(t *testing.T) {
	f := newTestRouter(t, 10)
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/server-cert", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var got struct {
		Fingerprint string `json:"fingerprint"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got.Fingerprint != "ab12" {
		t.Fatalf("fingerprint = %q", got.Fingerprint)
	}
}

func TestSecurityHeadersOnAllResponses(t *testing.T) {
	f := newTestRouter(t, 10)
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if w.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatal("security headers missing on /healthz")
	}
}
