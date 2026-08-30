package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"

	"github.com/YCJE/XProbe/internal/model"
	"github.com/YCJE/XProbe/server/internal/repository"
	"github.com/YCJE/XProbe/server/internal/service"
)

type wsEnv struct {
	server  *httptest.Server
	hub     *service.Hub
	agents  *repository.AgentRepo
	agentID int64
	token   string
	fp      string
}

func startWSServer(t *testing.T) *wsEnv {
	t.Helper()
	f := newTestRouter(t, 100)
	srv := httptest.NewServer(f.router)
	t.Cleanup(srv.Close)

	env := &wsEnv{
		server: srv,
		hub:    f.hub,
		agents: f.agents,
		token:  "test-token-" + fpHash("t"),
		fp:     fpHash("agent-fp"),
	}
	ctx := context.Background()
	id, err := env.agents.Create(ctx, &model.Agent{
		TokenHash:       fpHash(env.token), // S9: 哈希入库
		Hostname:        "web-01",
		HostFingerprint: env.fp,
		CreatedAt:       time.Now().Unix(),
	})
	if err != nil {
		t.Fatalf("Create agent: %v", err)
	}
	env.agentID = id
	return env
}

func (e *wsEnv) dialURL() string {
	return "ws" + strings.TrimPrefix(e.server.URL, "http") + "/api/v1/agent/report"
}

func (e *wsEnv) dial(t *testing.T, token, fp string) (*gorilla.Conn, *http.Response, error) {
	t.Helper()
	h := http.Header{}
	if token != "" {
		h.Set("Authorization", "Bearer "+token)
	}
	if fp != "" {
		h.Set("X-Host-Fingerprint", fp)
	}
	return gorilla.DefaultDialer.Dial(e.dialURL(), h)
}

func (e *wsEnv) send(t *testing.T, c *gorilla.Conn, v any) {
	t.Helper()
	b, _ := json.Marshal(v)
	if err := c.WriteMessage(gorilla.TextMessage, b); err != nil {
		t.Fatalf("send: %v", err)
	}
}

func waitFor(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cond()
}

func sampleReport() model.Report {
	u := 55.5
	return model.Report{
		Type:      model.FrameReport,
		Timestamp: time.Now().Unix(),
		Hostname:  "web-01",
		Data: model.ReportData{
			CPU:    model.CPUInfo{Usage: &u},
			Memory: model.MemoryInfo{Total: 100, Used: 50},
		},
	}
}

func TestWS_AuthRejected(t *testing.T) {
	env := startWSServer(t)

	// 错误 Token → 401
	if _, resp, err := env.dial(t, "wrong-token", env.fp); err == nil {
		t.Fatal("bad token should fail handshake")
	} else if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	// 指纹不匹配 → 403(设计文档 7.5)
	if _, resp, err := env.dial(t, env.token, fpHash("other-fp")); err == nil {
		t.Fatal("bad fingerprint should fail handshake")
	} else if resp != nil && resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	if env.hub.IsOnline(env.agentID) {
		t.Fatal("agent must not be online after failed handshakes")
	}
}

func TestWS_ReportHeartbeatAndImmediateOffline(t *testing.T) {
	env := startWSServer(t)
	conn, resp, err := env.dial(t, env.token, env.fp)
	if err != nil {
		t.Fatalf("dial: %v (status=%v)", err, resp)
	}
	defer conn.Close()
	if !waitFor(time.Second, func() bool { return env.hub.IsOnline(env.agentID) }) {
		t.Fatal("agent not marked online after WS attach")
	}

	// report 帧入库环形缓冲
	env.send(t, conn, sampleReport())
	if !waitFor(time.Second, func() bool { return len(env.hub.ReportSnapshot(env.agentID)) == 1 }) {
		t.Fatal("report not ingested into ring buffer")
	}

	// heartbeat → heartbeat_ack(Server→Agent 唯一帧)
	env.send(t, conn, model.Heartbeat{Type: model.FrameHeartbeat, Timestamp: time.Now().Unix()})
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read ack: %v", err)
	}
	var ack model.HeartbeatAck
	if json.Unmarshal(payload, &ack) != nil || ack.Type != model.FrameHeartbeatAck {
		t.Fatalf("ack = %s", payload)
	}

	// 客户端断开 → 即时离线(v1.3 主路径)
	conn.Close()
	if !waitFor(time.Second, func() bool { return !env.hub.IsOnline(env.agentID) }) {
		t.Fatal("agent not marked offline immediately after WS close")
	}
}

func TestWS_OversizedFrameDisconnected(t *testing.T) {
	env := startWSServer(t)
	conn, _, err := env.dial(t, env.token, env.fp)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// >64KB 单帧 → 传输层断开(设计文档 5.2/7.6)
	big := bytes.Repeat([]byte("a"), int(service.ReadLimit)+1024)
	_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if err := conn.WriteMessage(gorilla.TextMessage, big); err != nil {
		t.Fatalf("write big frame: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("connection should be closed by server after oversized frame")
	}
}

func TestWS_UnknownFrameTypeCloses(t *testing.T) {
	env := startWSServer(t)
	conn, _, err := env.dial(t, env.token, env.fp)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// 未知帧 = 协议违规(S1: 不存在任何命令帧) → 断开并记安全日志
	env.send(t, conn, map[string]any{"type": "exec_command", "cmd": "id"})
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("server should close conn on unknown frame type")
	}
}
