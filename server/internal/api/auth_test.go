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

	"github.com/YCJE/XProbe/server/internal/pkg"
	"github.com/YCJE/XProbe/server/internal/repository"
	"github.com/YCJE/XProbe/server/internal/service"
)

type mgmtFixture struct {
	router *gin.Engine
	jwt    *pkg.JWTManager
}

// newMgmtFixture 带完整管理面依赖的路由夹具。
func newMgmtFixture(t *testing.T) *mgmtFixture {
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
	admins := repository.NewAdminRepo(db)
	sessions := repository.NewSessionRepo(db)
	jwt := pkg.NewJWTManager("test-secret-0123456789abcdef0123456789abcdef", 2*time.Hour, false)
	authSvc := service.NewAuth(admins, sessions, jwt, pkg.NewLimiter(100, time.Minute))

	d := Deps{
		Registry:        service.NewRegistry(agents, repository.NewRegisterCodeRepo(db)),
		Hub:             service.NewHub(agents, 90*time.Second),
		Agents:          agents,
		PingTargets:     repository.NewPingTargetRepo(db),
		Records:         repository.NewRecordRepo(db),
		Auth:            authSvc,
		JWT:             jwt,
		Sessions:        sessions,
		Tags:            repository.NewTagRepo(db),
		GlobalLimiter:   pkg.NewLimiter(1000, time.Minute),
		LoginLimiter:    pkg.NewLimiter(100, time.Minute),
		CertFingerprint: "ab12",
		Now:             time.Now,
	}
	return &mgmtFixture{router: NewRouter(d, nil), jwt: jwt}
}

type setupStateResp struct {
	SetupDone bool `json:"setup_done"`
}

func TestAuthFlow_SetupLoginSessionsLogout(t *testing.T) {
	f := newMgmtFixture(t)

	// 初始化状态
	w := f.get(t, "/api/v1/auth/setup-state", "")
	var st setupStateResp
	_ = json.Unmarshal(w.Body.Bytes(), &st)
	if st.SetupDone {
		t.Fatal("setup should not be done on fresh db")
	}

	// 弱密码拒绝
	if w = f.post(t, "/api/v1/auth/setup", map[string]string{"username": "admin", "password": "short"}); w.Code != http.StatusBadRequest {
		t.Fatalf("weak password = %d, want 400", w.Code)
	}
	// 初始化
	if w = f.post(t, "/api/v1/auth/setup", map[string]string{"username": "admin", "password": "S3curePassw0rd!"}); w.Code != http.StatusOK {
		t.Fatalf("setup = %d, body=%s", w.Code, w.Body.String())
	}
	// 二次初始化 → 409
	if w = f.post(t, "/api/v1/auth/setup", map[string]string{"username": "x", "password": "S3curePassw0rd!"}); w.Code != http.StatusConflict {
		t.Fatalf("second setup = %d, want 409", w.Code)
	}

	// 错误密码 → 401
	if w = f.post(t, "/api/v1/auth/login", map[string]string{"username": "admin", "password": "WrongPassw0rd!"}); w.Code != http.StatusUnauthorized {
		t.Fatalf("bad login = %d, want 401", w.Code)
	}
	// 正确登录 → 200 + Cookie
	w = f.post(t, "/api/v1/auth/login", map[string]string{"username": "admin", "password": "S3curePassw0rd!"})
	if w.Code != http.StatusOK {
		t.Fatalf("login = %d, body=%s", w.Code, w.Body.String())
	}
	cookie := ""
	for _, ck := range w.Result().Cookies() {
		if ck.Name == pkg.SessionCookie {
			cookie = ck.Value
		}
	}
	if cookie == "" {
		t.Fatal("session cookie missing")
	}

	// 会话列表(带 Cookie)
	w = f.get(t, "/api/v1/auth/sessions", cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("sessions = %d", w.Code)
	}
	var list struct {
		Sessions []struct {
			ID      int64 `json:"id"`
			Current bool  `json:"current"`
		} `json:"sessions"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if len(list.Sessions) != 1 || !list.Sessions[0].Current {
		t.Fatalf("sessions = %+v, want 1 current", list.Sessions)
	}

	// 无认证访问受保护路由 → 401
	if w = f.get(t, "/api/v1/servers", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthed servers = %d, want 401", w.Code)
	}

	// 登出后 Cookie 对应会话失效
	if w = f.post(t, "/api/v1/auth/logout", nil, cookie); w.Code != http.StatusOK {
		t.Fatalf("logout = %d, want 200", w.Code)
	}
	if w = f.get(t, "/api/v1/auth/sessions", cookie); w.Code != http.StatusUnauthorized {
		t.Fatalf("after logout = %d, want 401 (session revoked)", w.Code)
	}
}

func (f *mgmtFixture) post(t *testing.T, path string, body any, cookie ...string) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if len(cookie) > 0 && cookie[0] != "" {
		req.AddCookie(&http.Cookie{Name: pkg.SessionCookie, Value: cookie[0]})
	}
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	return w
}

func (f *mgmtFixture) get(t *testing.T, path, cookie string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: pkg.SessionCookie, Value: cookie})
	}
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	return w
}
