package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/pquerna/otp/totp"
	"probe/common/wire"
	"probe/server/internal/config"
	"probe/server/internal/model"
	"probe/server/internal/proxy"
	"probe/server/internal/repository"
	"probe/server/internal/security"
)

type testEnv struct {
	app    *Server
	http   *httptest.Server
	client *http.Client
	token  string
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()
	cfg.PublicURL = "http://127.0.0.1:8080"
	cfg.Auth.SessionIdle.Duration = time.Hour
	cfg.Auth.SessionMax.Duration = 24 * time.Hour

	db, err := repository.Open(context.Background(), filepath.Join(cfg.DataDir, "probe.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := repository.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	app, err := New(&cfg, db, logger)
	if err != nil {
		t.Fatalf("api: %v", err)
	}
	setupToken, err := app.EnsureSetupToken(context.Background())
	if err != nil {
		t.Fatalf("setup token: %v", err)
	}
	server := httptest.NewServer(app.Handler())
	t.Cleanup(server.Close)
	jar, _ := cookiejar.New(nil)
	return &testEnv{
		app: app, http: server,
		client: &http.Client{Jar: jar, Timeout: 10 * time.Second},
		token:  setupToken,
	}
}

func (e *testEnv) request(t *testing.T, method, path string, body any, headers map[string]string) (*http.Response, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, e.http.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp, raw
}

func TestSetupLoginAndSession(t *testing.T) {
	env := newTestEnv(t)

	resp, _ := env.request(t, http.MethodPost, "/api/v1/setup", map[string]string{
		"token": env.token, "password": "CorrectHorse9",
	}, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("setup status = %d", resp.StatusCode)
	}
	resp, _ = env.request(t, http.MethodPost, "/api/v1/setup", map[string]string{
		"token": env.token, "password": "CorrectHorse9",
	}, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("second setup status = %d, want 404", resp.StatusCode)
	}

	resp, _ = env.request(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "admin", "password": "CorrectHorse9",
	}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d", resp.StatusCode)
	}
	resp, body := env.request(t, http.MethodGet, "/api/v1/auth/me", nil, nil)
	if resp.StatusCode != http.StatusOK || !bytes.Contains(body, []byte(`"username":"admin"`)) {
		t.Fatalf("me status = %d body=%s", resp.StatusCode, body)
	}
	resp, _ = env.request(t, http.MethodPost, "/api/v1/auth/logout", nil, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout status = %d", resp.StatusCode)
	}
	resp, _ = env.request(t, http.MethodGet, "/api/v1/auth/me", nil, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("me after logout status = %d", resp.StatusCode)
	}
}

func TestCrossSiteWriteIsRefused(t *testing.T) {
	env := newTestEnv(t)
	resp, _ := env.request(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "admin", "password": "x",
	}, map[string]string{"Origin": "https://evil.example", "Sec-Fetch-Site": "cross-site"})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-site login status = %d, want 403", resp.StatusCode)
	}
}

func TestRegisterAgentConfigAndWebSocket(t *testing.T) {
	env := newTestEnv(t)
	if resp, _ := env.request(t, http.MethodPost, "/api/v1/setup", map[string]string{
		"token": env.token, "password": "CorrectHorse9",
	}, nil); resp.StatusCode != http.StatusCreated {
		t.Fatalf("setup = %d", resp.StatusCode)
	}
	if resp, _ := env.request(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "admin", "password": "CorrectHorse9",
	}, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("login = %d", resp.StatusCode)
	}

	resp, body := env.request(t, http.MethodPost, "/api/v1/agents/register-codes", nil, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create code = %d body=%s", resp.StatusCode, body)
	}
	var codeResp struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(body, &codeResp); err != nil || codeResp.Code == "" {
		t.Fatalf("code response: %s %v", body, err)
	}

	fingerprint := strings.Repeat("a", 64)
	registerBody := wire.RegisterRequest{
		Hostname: "web-01", OS: "linux", Arch: "amd64", AgentVersion: "0.2.0",
		HostFingerprint: fingerprint, InstallSalt: "salt",
	}
	resp, body = env.request(t, http.MethodPost, "/api/v1/agent/register", registerBody, map[string]string{
		"Authorization": "Bearer " + codeResp.Code,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register agent = %d body=%s", resp.StatusCode, body)
	}
	var reg wire.RegisterResponse
	if err := json.Unmarshal(body, &reg); err != nil || reg.Token == "" || reg.AgentID == 0 {
		t.Fatalf("agent response: %s %v", body, err)
	}
	var stored string
	if err := env.app.db.QueryRow(`SELECT token_hash FROM agents WHERE id = ?`, reg.AgentID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != security.HashToken(reg.Token) || stored == reg.Token {
		t.Fatal("agent token was not stored as a hash")
	}

	// The same code cannot enroll a second machine.
	resp, _ = env.request(t, http.MethodPost, "/api/v1/agent/register", wire.RegisterRequest{
		Hostname: "web-02", OS: "linux", Arch: "amd64", AgentVersion: "0.2.0",
		HostFingerprint: strings.Repeat("b", 64), InstallSalt: "salt",
	}, map[string]string{"Authorization": "Bearer " + codeResp.Code})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("reused code status = %d, want 401", resp.StatusCode)
	}

	resp, body = env.request(t, http.MethodGet, "/api/v1/agent/config", nil, map[string]string{
		"Authorization": "Bearer " + reg.Token,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("agent config = %d body=%s", resp.StatusCode, body)
	}
	var agentCfg wire.AgentConfig
	if err := json.Unmarshal(body, &agentCfg); err != nil || len(agentCfg.PingTargets) != 5 {
		t.Fatalf("config: %s %v", body, err)
	}
	if resp, _ := env.request(t, http.MethodGet, "/api/v1/agent/config", nil, nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated config = %d", resp.StatusCode)
	}

	wsURL := "ws" + strings.TrimPrefix(env.http.URL, "http") + "/api/v1/agent/report"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, resp, err := websocket.Dial(ctx, wsURL, nil); err == nil {
		t.Fatal("unauthenticated WebSocket was accepted")
	} else if resp != nil {
		_ = resp.Body.Close()
	}
	header := http.Header{}
	header.Set("Authorization", "Bearer "+reg.Token)
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	defer conn.CloseNow()

	heartbeat, _ := json.Marshal(wire.Heartbeat{Type: wire.TypeHeartbeat, Timestamp: time.Now().Unix()})
	if err := conn.Write(ctx, websocket.MessageText, heartbeat); err != nil {
		t.Fatalf("heartbeat write: %v", err)
	}
	_, ack, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("heartbeat ack read: %v", err)
	}
	if !bytes.Contains(ack, []byte(`"heartbeat_ack"`)) {
		t.Fatalf("unexpected ack: %s", ack)
	}

	report := wire.Report{
		Type: wire.TypeReport, Timestamp: time.Now().Unix(), Hostname: "web-01", AgentVersion: "0.2.0",
		Data: wire.ReportData{
			CPU:     wire.CPUMetrics{Usage: 12.5, Cores: 2},
			Memory:  wire.MemoryMetrics{Total: 100, Used: 40},
			Network: wire.NetworkMetrics{RxSpeed: 10, TxSpeed: 20},
		},
	}
	payload, _ := json.Marshal(report)
	if err := conn.Write(ctx, websocket.MessageText, payload); err != nil {
		t.Fatalf("report write: %v", err)
	}
	ping := wire.PingResult{Type: wire.TypePingResult, Data: []wire.PingStat{{
		Target: "1.1.1.1", Name: "test", Method: "tcp", IPVersion: 4,
		AvgLatency: 12.5, MinLatency: 10, MaxLatency: 15, Loss: 0,
		PacketsSent: 5, PacketsRecv: 5,
	}}}
	pingPayload, _ := json.Marshal(ping)
	if err := conn.Write(ctx, websocket.MessageText, pingPayload); err != nil {
		t.Fatalf("ping write: %v", err)
	}

	// Give the server a moment to file the report, then query the admin view.
	time.Sleep(200 * time.Millisecond)
	resp, body = env.request(t, http.MethodGet, "/api/v1/servers", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("servers = %d body=%s", resp.StatusCode, body)
	}
	var servers struct {
		Servers []map[string]any `json:"servers"`
	}
	if err := json.Unmarshal(body, &servers); err != nil || len(servers.Servers) != 1 {
		t.Fatalf("servers: %s %v", body, err)
	}
	if online, _ := servers.Servers[0]["online"].(bool); !online {
		t.Fatalf("agent is not online: %s", body)
	}
	if cpu, _ := servers.Servers[0]["cpu_usage"].(float64); cpu != 12.5 {
		t.Fatalf("cpu not visible: %s", body)
	}

	resp, body = env.request(t, http.MethodGet, "/api/v1/servers/"+strconv.FormatInt(reg.AgentID, 10), nil, nil)
	if resp.StatusCode != http.StatusOK || !bytes.Contains(body, []byte(`"disks"`)) {
		t.Fatalf("detail = %d body=%s", resp.StatusCode, body)
	}
	resp, body = env.request(t, http.MethodGet, "/api/v1/servers/"+strconv.FormatInt(reg.AgentID, 10)+"/history?range=1h", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("history = %d body=%s", resp.StatusCode, body)
	}
	var history struct {
		Metrics []map[string]any `json:"metrics"`
		Pings   []map[string]any `json:"pings"`
	}
	if err := json.Unmarshal(body, &history); err != nil {
		t.Fatalf("history decode: %v", err)
	}
	if len(history.Metrics) == 0 || len(history.Pings) == 0 {
		t.Fatalf("history is empty: %s", body)
	}
}

func TestSetupTokenExpires(t *testing.T) {
	env := newTestEnv(t)
	old := time.Now().Add(-setupTokenTTL - time.Minute)
	if err := os.Chtimes(env.app.setupTokenPath, old, old); err != nil {
		t.Fatalf("age setup token: %v", err)
	}
	resp, _ := env.request(t, http.MethodPost, "/api/v1/setup", map[string]string{
		"token": env.token, "password": "CorrectHorse9",
	}, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expired setup token status = %d, want 401", resp.StatusCode)
	}

	fresh, err := env.app.EnsureSetupToken(context.Background())
	if err != nil || fresh == "" || fresh == env.token {
		t.Fatalf("fresh token: %q %v", fresh, err)
	}
	resp, _ = env.request(t, http.MethodPost, "/api/v1/setup", map[string]string{
		"token": fresh, "password": "CorrectHorse9",
	}, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("fresh setup token status = %d", resp.StatusCode)
	}
}

func TestTOTPEnrollmentAndLogin(t *testing.T) {
	env := newTestEnv(t)
	setupAndLogin(t, env)

	resp, _ := env.request(t, http.MethodPost, "/api/v1/auth/totp/setup", map[string]string{}, nil)
	if resp.StatusCode == http.StatusOK {
		t.Fatal("TOTP setup without the current password was accepted")
	}
	resp, body := env.request(t, http.MethodPost, "/api/v1/auth/totp/setup",
		map[string]string{"password": "CorrectHorse9"}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("totp setup = %d body=%s", resp.StatusCode, body)
	}
	var setup struct {
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(body, &setup); err != nil || setup.Secret == "" {
		t.Fatalf("totp setup body: %s %v", body, err)
	}
	code, err := totp.GenerateCode(setup.Secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	resp, body = env.request(t, http.MethodPost, "/api/v1/auth/totp/enable", map[string]string{"code": code}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("totp enable = %d body=%s", resp.StatusCode, body)
	}
	if resp, _ = env.request(t, http.MethodPost, "/api/v1/auth/logout", nil, nil); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout = %d", resp.StatusCode)
	}
	resp, _ = env.request(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "admin", "password": "CorrectHorse9",
	}, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("login without TOTP = %d, want 401", resp.StatusCode)
	}
	resp, body = env.request(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "admin", "password": "CorrectHorse9", "totp_code": code,
	}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login with TOTP = %d body=%s", resp.StatusCode, body)
	}
}

func TestPublicDashboardAndSettings(t *testing.T) {
	env := newTestEnv(t)
	setupAndLogin(t, env)
	resp, body := env.request(t, http.MethodPut, "/api/v1/config/settings", map[string]any{
		"public_enabled": true, "public_title": "Lab status",
	}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("save settings = %d body=%s", resp.StatusCode, body)
	}
	var settings struct {
		PublicEnabled bool   `json:"public_enabled"`
		ShareID       string `json:"public_share_id"`
	}
	if err := json.Unmarshal(body, &settings); err != nil || !settings.PublicEnabled || settings.ShareID == "" {
		t.Fatalf("settings: %s %v", body, err)
	}
	resp, body = env.request(t, http.MethodGet, "/api/v1/public/"+settings.ShareID, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("public page = %d body=%s", resp.StatusCode, body)
	}
	if bytes.Contains(body, []byte(`"ipv4"`)) || bytes.Contains(body, []byte(`"token"`)) {
		t.Fatalf("public page leaked sensitive fields: %s", body)
	}
	if _, body = env.request(t, http.MethodPut, "/api/v1/config/settings", map[string]any{
		"public_enabled": false,
	}, nil); !bytes.Contains(body, []byte(`"public_enabled":false`)) {
		t.Fatalf("disable response: %s", body)
	}
	resp, _ = env.request(t, http.MethodGet, "/api/v1/public/"+settings.ShareID, nil, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("disabled public page = %d, want 404", resp.StatusCode)
	}
}

func TestAlertAndChannelCRUD(t *testing.T) {
	env := newTestEnv(t)
	setupAndLogin(t, env)
	resp, body := env.request(t, http.MethodPost, "/api/v1/alerts", map[string]any{
		"name": "CPU high", "metric": "cpu", "operator": ">", "threshold": 90,
		"duration": 60, "enabled": true,
	}, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create alert = %d body=%s", resp.StatusCode, body)
	}
	var rule struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(body, &rule); err != nil || rule.ID == 0 {
		t.Fatalf("alert body: %s %v", body, err)
	}
	for _, threshold := range []float64{-1, 101} {
		resp, body = env.request(t, http.MethodPost, "/api/v1/alerts", map[string]any{
			"name": "bad threshold", "metric": "cpu", "operator": ">", "threshold": threshold,
			"duration": 60, "enabled": true,
		}, nil)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("threshold %v accepted: %d %s", threshold, resp.StatusCode, body)
		}
	}
	resp, body = env.request(t, http.MethodGet, "/api/v1/alerts", nil, nil)
	if resp.StatusCode != http.StatusOK || !bytes.Contains(body, []byte(`"CPU high"`)) {
		t.Fatalf("list alerts = %d body=%s", resp.StatusCode, body)
	}
	resp, _ = env.request(t, http.MethodDelete, "/api/v1/alerts/"+strconv.FormatInt(rule.ID, 10), nil, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete alert = %d", resp.StatusCode)
	}
	resp, body = env.request(t, http.MethodPost, "/api/v1/notify/channels", map[string]any{
		"name": "Hook", "type": "webhook", "config": map[string]any{"url": "https://example.com/hook"},
	}, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create channel = %d body=%s", resp.StatusCode, body)
	}
	var channel struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(body, &channel); err != nil || channel.ID == 0 {
		t.Fatalf("channel body: %s %v", body, err)
	}
	var stored string
	if err := env.app.db.QueryRow(`SELECT config FROM notify_channels WHERE id = ?`, channel.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stored, "enc:v1:") || strings.Contains(stored, "example.com") {
		t.Fatalf("channel config was not encrypted at rest: %q", stored)
	}
	resp, body = env.request(t, http.MethodGet, "/api/v1/notify/channels", nil, nil)
	if resp.StatusCode != http.StatusOK || !bytes.Contains(body, []byte(`"Hook"`)) {
		t.Fatalf("list channels = %d body=%s", resp.StatusCode, body)
	}
}

func setupAndLogin(t *testing.T, env *testEnv) {
	t.Helper()
	if resp, _ := env.request(t, http.MethodPost, "/api/v1/setup", map[string]string{
		"token": env.token, "password": "CorrectHorse9",
	}, nil); resp.StatusCode != http.StatusCreated {
		t.Fatalf("setup = %d", resp.StatusCode)
	}
	if resp, _ := env.request(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "admin", "password": "CorrectHorse9",
	}, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("login = %d", resp.StatusCode)
	}
}

func TestMetadataAndPingTargetCRUD(t *testing.T) {
	env := newTestEnv(t)
	setupAndLogin(t, env)
	agents := repository.NewAgentRepo(env.app.db)
	agentID, err := agents.Create(t.Context(), &model.Agent{
		TokenHash: security.HashToken("meta-token"), Hostname: "meta-01",
		HostFingerprint: strings.Repeat("c", 64), TagIDs: "[]",
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, body := env.request(t, http.MethodPut, "/api/v1/servers/"+strconv.FormatInt(agentID, 10)+"/meta",
		map[string]any{
			"display_name": "HK-01", "region": "Hong Kong", "country_code": "HK",
			"isp": "Example", "expires_at": int64(0), "price_amount": 5,
			"price_currency": "USD", "price_cycle": "monthly", "traffic_quota_bytes": int64(100 << 30),
		}, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("update meta = %d body=%s", resp.StatusCode, body)
	}
	resp, body = env.request(t, http.MethodGet, "/api/v1/servers/"+strconv.FormatInt(agentID, 10), nil, nil)
	if resp.StatusCode != http.StatusOK || !bytes.Contains(body, []byte(`"display_name":"HK-01"`)) ||
		!bytes.Contains(body, []byte(`"traffic_quota_bytes":107374182400`)) {
		t.Fatalf("detail after meta: %d %s", resp.StatusCode, body)
	}

	resp, body = env.request(t, http.MethodPost, "/api/v1/config/ping-targets", map[string]any{
		"target": "1.1.1.1", "name": "CF", "ip_version": 4, "protocol": "icmp", "enabled": true,
	}, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create target = %d body=%s", resp.StatusCode, body)
	}
	var target struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(body, &target); err != nil || target.ID == 0 {
		t.Fatalf("target body: %s %v", body, err)
	}
	resp, _ = env.request(t, http.MethodPut, "/api/v1/config/ping-targets/"+strconv.FormatInt(target.ID, 10),
		map[string]any{"target": "1.1.1.1", "name": "CF", "ip_version": 4, "protocol": "icmp", "enabled": false}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update target = %d", resp.StatusCode)
	}
	resp, _ = env.request(t, http.MethodDelete, "/api/v1/config/ping-targets/"+strconv.FormatInt(target.ID, 10), nil, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete target = %d", resp.StatusCode)
	}
	// Seeded default targets are not deletable.
	resp, _ = env.request(t, http.MethodDelete, "/api/v1/config/ping-targets/1", nil, nil)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("delete default target = %d, want 409", resp.StatusCode)
	}

	for name, targetBody := range map[string]map[string]any{
		"unsupported http scheme": {
			"target": "file:///etc/passwd", "name": "file", "ip_version": 4,
			"protocol": "http", "enabled": true,
		},
		"oversized region": {
			"target": "1.1.1.1", "name": "CF", "region": strings.Repeat("x", 65),
			"ip_version": 4, "protocol": "icmp", "enabled": true,
		},
		"control character": {
			"target": "1.1.1.1", "name": "bad\nname", "ip_version": 4,
			"protocol": "icmp", "enabled": true,
		},
	} {
		resp, body = env.request(t, http.MethodPost, "/api/v1/config/ping-targets", targetBody, nil)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s accepted: %d %s", name, resp.StatusCode, body)
		}
	}
}

func TestDashboardOriginRules(t *testing.T) {
	env := newTestEnv(t)
	r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:18081/ws/dashboard", nil)
	r.Host = "127.0.0.1:18081"
	r = r.WithContext(context.WithValue(r.Context(), metaKey{}, proxy.Info{Scheme: "http"}))
	if !env.app.dashboardOriginAllowed(r, "http://127.0.0.1:18081") {
		t.Fatal("loopback development origin was rejected")
	}
	if env.app.dashboardOriginAllowed(r, "https://evil.example") {
		t.Fatal("cross-site origin was accepted")
	}

	env.app.cfg.PublicURL = "https://monitor.example.com"
	r.Host = "monitor.example.com"
	if !env.app.dashboardOriginAllowed(r, "https://monitor.example.com") {
		t.Fatal("HTTPS public origin was rejected")
	}
	if env.app.dashboardOriginAllowed(r, "http://monitor.example.com") {
		t.Fatal("plain HTTP origin was accepted for an HTTPS hub")
	}
}

func TestValidateRegistrationRejectsControlCharacters(t *testing.T) {
	valid := wire.RegisterRequest{
		Hostname: "web-01", OS: "linux", Arch: "amd64", AgentVersion: "0.2.0",
		HostFingerprint: strings.Repeat("a", 64), InstallSalt: "salt",
	}
	if err := validateRegistration(valid); err != nil {
		t.Fatalf("valid registration rejected: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*wire.RegisterRequest)
	}{
		{"hostname newline", func(r *wire.RegisterRequest) { r.Hostname = "web\n01" }},
		{"os control", func(r *wire.RegisterRequest) { r.OS = "linux\x00" }},
		{"arch control", func(r *wire.RegisterRequest) { r.Arch = "amd\n64" }},
		{"version control", func(r *wire.RegisterRequest) { r.AgentVersion = "0.2\t.0" }},
		{"salt control", func(r *wire.RegisterRequest) { r.InstallSalt = "salt\n" }},
	}
	for _, tc := range cases {
		req := valid
		tc.mutate(&req)
		if err := validateRegistration(req); err == nil {
			t.Errorf("%s: expected rejection", tc.name)
		}
	}
}

func TestPageCSPRestrictsWebSocketOrigins(t *testing.T) {
	env := newTestEnv(t)
	env.app.cfg.PublicURL = "https://monitor.example.com"
	resp, _ := env.request(t, http.MethodGet, "/", nil, nil)
	csp := resp.Header.Get("Content-Security-Policy")
	if !strings.Contains(csp, "wss://monitor.example.com") {
		t.Fatalf("CSP does not allow the configured WebSocket origin: %s", csp)
	}
	if strings.Contains(csp, "wss:;") || strings.Contains(csp, "ws: wss:") {
		t.Fatalf("CSP still allows arbitrary WebSocket hosts: %s", csp)
	}
	if !strings.Contains(csp, "script-src 'self'") || strings.Contains(csp, "unsafe-eval") {
		t.Fatalf("unexpected script policy: %s", csp)
	}
}

func TestPageCSPRejectsMalformedHost(t *testing.T) {
	env := newTestEnv(t)
	env.app.cfg.PublicURL = "https://monitor.example.com"
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "evil.example.com'; script-src *"
	w := httptest.NewRecorder()
	env.app.Handler().ServeHTTP(w, req)
	csp := w.Header().Get("Content-Security-Policy")
	if strings.Contains(csp, "script-src *") || strings.Contains(csp, "evil.example.com") {
		t.Fatalf("malformed Host leaked into CSP: %s", csp)
	}
}

func TestValidHostHeader(t *testing.T) {
	valid := []string{"monitor.example.com", "monitor.example.com:8443", "[::1]:8080", "127.0.0.1", "internal_host"}
	for _, host := range valid {
		if !validHostHeader(host) {
			t.Errorf("validHostHeader(%q) = false, want true", host)
		}
	}
	invalid := []string{"", "evil host", "evil;host", "evil'host", "evil\"host", "evil,host",
		"evil*host", "evil(host)", "host/path", strings.Repeat("a", 301)}
	for _, host := range invalid {
		if validHostHeader(host) {
			t.Errorf("validHostHeader(%q) = true, want false", host)
		}
	}
}

func TestSessionCookieFlags(t *testing.T) {
	env := newTestEnv(t)
	r := httptest.NewRequest(http.MethodGet, "http://monitor.example.com/", nil)
	r = r.WithContext(context.WithValue(r.Context(), metaKey{}, proxy.Info{Scheme: "https"}))
	w := httptest.NewRecorder()
	env.app.setSessionCookie(w, r, "token", time.Now().Add(time.Hour))
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %d", len(cookies))
	}
	c := cookies[0]
	if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode || c.Path != "/" {
		t.Fatalf("unexpected cookie flags: %+v", c)
	}

	// A TLS-terminating reverse proxy may hand the process plain HTTP while
	// the public URL is HTTPS. The cookie must still be Secure.
	env.app.cfg.PublicURL = "https://monitor.example.com"
	r2 := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/", nil)
	r2 = r2.WithContext(context.WithValue(r2.Context(), metaKey{}, proxy.Info{Scheme: "http"}))
	w2 := httptest.NewRecorder()
	env.app.setSessionCookie(w2, r2, "token", time.Now().Add(time.Hour))
	c2 := w2.Result().Cookies()[0]
	if !c2.Secure {
		t.Fatal("cookie was not Secure behind an HTTPS reverse proxy")
	}
}

func TestEmptyHistoryResponsesAreArrays(t *testing.T) {
	env := newTestEnv(t)
	setupAndLogin(t, env)
	resp, body := env.request(t, http.MethodGet, "/api/v1/alerts/history", nil, nil)
	if resp.StatusCode != http.StatusOK || !bytes.Contains(body, []byte(`"history":[]`)) {
		t.Fatalf("empty alert history = %d %s", resp.StatusCode, body)
	}
	if _, err := env.app.db.ExecContext(t.Context(), `DELETE FROM audit_log`); err != nil {
		t.Fatal(err)
	}
	resp, body = env.request(t, http.MethodGet, "/api/v1/audit", nil, nil)
	if resp.StatusCode != http.StatusOK || !bytes.Contains(body, []byte(`"events":[]`)) {
		t.Fatalf("empty audit log = %d %s", resp.StatusCode, body)
	}
}
