package reporter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/YCJE/XProbe/internal/model"
)

func TestNextBackoff_Sequence(t *testing.T) {
	base := time.Second
	got := time.Duration(0)
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second,
		16 * time.Second, 32 * time.Second, 60 * time.Second, 60 * time.Second}
	for i, w := range want {
		got = NextBackoff(base, got)
		if got != w {
			t.Fatalf("step %d = %v, want %v", i, got, w)
		}
	}
}

func TestWithJitter_Bounds(t *testing.T) {
	d := 10 * time.Second
	for r := 0.0; r < 1.0; r += 0.05 {
		j := WithJitter(d, r)
		if j < time.Duration(float64(d)*0.8) || j > time.Duration(float64(d)*1.2) {
			t.Fatalf("jitter %v out of ±20%% for r=%f", j, r)
		}
	}
	if WithJitter(0, 0.5) != 0 {
		t.Fatal("jitter of 0 should be 0")
	}
}

// countingServer 模拟 Server: 记录 report/heartbeat 帧数与连接次数。
type countingServer struct {
	mu                sync.Mutex
	reports           int
	heartbeats        int
	conns             int
	framesBeforeClose int // 每条连接收满 N 帧后主动断开(测重连), 0=不断
	srv               *httptest.Server
}

func (s *countingServer) handler() http.HandlerFunc {
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		s.mu.Lock()
		s.conns++
		limit := s.framesBeforeClose
		s.mu.Unlock()

		defer c.Close()
		frames := 0
		for {
			_, payload, err := c.ReadMessage()
			if err != nil {
				return
			}
			frames++
			var head struct {
				Type model.FrameType `json:"type"`
			}
			if json.Unmarshal(payload, &head) != nil {
				return
			}
			s.mu.Lock()
			switch head.Type {
			case model.FrameReport:
				s.reports++
			case model.FrameHeartbeat:
				s.heartbeats++
			}
			s.mu.Unlock()

			_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
			ack, _ := json.Marshal(model.HeartbeatAck{Type: model.FrameHeartbeatAck})
			if err := c.WriteMessage(websocket.TextMessage, ack); err != nil {
				return
			}
			if limit > 0 && frames >= limit {
				return // 模拟服务端断开 → 客户端应重连
			}
		}
	}
}

func TestClient_RunReportsHeartbeatsAndReconnects(t *testing.T) {
	cs := &countingServer{framesBeforeClose: 3} // 每连接收 3 帧后断开 → 触发重连
	cs.srv = httptest.NewServer(cs.handler())
	defer cs.srv.Close()

	wsURL := "ws" + cs.srv.URL[len("http"):]
	reports := 0
	client := &Client{
		WSURL:             wsURL,
		Token:             "tok",
		Fingerprint:       "fp",
		ReportInterval:    30 * time.Millisecond,
		HeartbeatInterval: 40 * time.Millisecond,
		BackoffStart:      20 * time.Millisecond, // 加速测试重连
		Dial: func(ctx context.Context, header http.Header) (*websocket.Conn, error) {
			// 注入: 忽略 URL 校验/TLS(测试为 http), 但校验鉴权头已携带
			if header.Get("Authorization") != "Bearer tok" || header.Get("X-Host-Fingerprint") != "fp" {
				t.Errorf("missing auth headers: %v", header)
			}
			conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
			return conn, err
		},
		Collect: func(ctx context.Context) (model.Report, error) {
			reports++
			u := 50.0
			return model.Report{Type: model.FrameReport, Hostname: "t",
				Data: model.ReportData{CPU: model.CPUInfo{Usage: &u}}}, nil
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()
	_ = client.Run(ctx) // 以 ctx 超时结束

	cs.mu.Lock()
	defer cs.mu.Unlock()
	if cs.reports < 3 {
		t.Fatalf("reports = %d, want >=3", cs.reports)
	}
	if cs.heartbeats < 1 {
		t.Fatalf("heartbeats = %d, want >=1", cs.heartbeats)
	}
	if cs.conns < 2 {
		t.Fatalf("connections = %d, want >=2 (reconnect after server close)", cs.conns)
	}
	if reports < 3 {
		t.Fatalf("Collect calls = %d", reports)
	}
}
