package service

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/YCJE/XProbe/internal/model"
	"github.com/YCJE/XProbe/server/internal/repository"
)

// fakeConn 模拟 WS 连接: 记录写入、可关闭。
type fakeConn struct {
	mu     sync.Mutex
	writes [][]byte
	closed bool
	closes int
}

func (f *fakeConn) ReadMessage() (int, []byte, error) { return 0, nil, context.Canceled }
func (f *fakeConn) WriteMessage(t int, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes = append(f.writes, data)
	return nil
}
func (f *fakeConn) SetReadLimit(int64)               {}
func (f *fakeConn) SetReadDeadline(time.Time) error  { return nil }
func (f *fakeConn) SetWriteDeadline(time.Time) error { return nil }
func (f *fakeConn) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	f.closes++
	return nil
}
func (f *fakeConn) isClosed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}
func (f *fakeConn) writeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.writes)
}

func newTestHub(t *testing.T) (*Hub, *repository.AgentRepo) {
	t.Helper()
	db, err := repository.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := repository.Migrate(context.Background(), db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	repo := repository.NewAgentRepo(db)
	return NewHub(repo, 90*time.Second), repo
}

func TestHub_AttachOnlineKickOldDetachOffline(t *testing.T) {
	hub, repo := newTestHub(t)
	ctx := context.Background()
	id, _ := repo.Create(ctx, &model.Agent{TokenHash: "h1", Hostname: "a", HostFingerprint: "f1", CreatedAt: 1})

	offlineCh := make(chan int64, 4)
	hub.SetOnOffline(func(agentID int64) { offlineCh <- agentID })

	c1 := &fakeConn{}
	if err := hub.Attach(id, c1); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if !hub.IsOnline(id) || hub.OnlineCount() != 1 {
		t.Fatal("agent should be online")
	}

	// 同一 Agent 二次连接: 旧连接被踢关闭
	c2 := &fakeConn{}
	_ = hub.Attach(id, c2)
	if !c1.isClosed() {
		t.Fatal("old conn should be closed on re-attach")
	}
	if hub.OnlineCount() != 1 {
		t.Fatal("still exactly one online conn")
	}

	// 断开: 即时离线(v1.3 主路径)
	hub.Detach(id, c2)
	if hub.IsOnline(id) {
		t.Fatal("agent should be offline after Detach")
	}
	select {
	case got := <-offlineCh:
		if got != id {
			t.Fatalf("offline hook got %d", got)
		}
	case <-time.After(time.Second):
		t.Fatal("offline hook not called")
	}
	// Detach 旧连接(已被替换)不产生副作用
	hub.Detach(id, c1)
	if hub.IsOnline(id) {
		t.Fatal("stale detach must not resurrect state")
	}
}

func TestHub_HandleReport(t *testing.T) {
	hub, repo := newTestHub(t)
	ctx := context.Background()
	id, _ := repo.Create(ctx, &model.Agent{TokenHash: "h", Hostname: "a", HostFingerprint: "f", CreatedAt: 1})
	c := &fakeConn{}
	_ = hub.Attach(id, c)

	r := validReport()
	if err := hub.HandleReport(id, r); err != nil {
		t.Fatalf("HandleReport: %v", err)
	}
	if got := hub.ReportSnapshot(id); len(got) != 1 {
		t.Fatalf("buffer len = %d, want 1", len(got))
	}
	// 3s±1s: 立即再报 → 过快拒绝
	if err := hub.HandleReport(id, r); err != ErrReportTooFast {
		t.Fatalf("err = %v, want ErrReportTooFast", err)
	}
	// 校验失败 → 拒绝入库
	bad := validReport()
	v := 200.0
	bad.Data.CPU.Usage = &v
	if err := hub.HandleReport(id, bad); err == nil {
		t.Fatal("invalid report should be rejected")
	}
	if got := hub.ReportSnapshot(id); len(got) != 1 {
		t.Fatalf("buffer should stay at 1, got %d", len(got))
	}
}

func TestHub_HeartbeatAck(t *testing.T) {
	hub, repo := newTestHub(t)
	ctx := context.Background()
	id, _ := repo.Create(ctx, &model.Agent{TokenHash: "h", Hostname: "a", HostFingerprint: "f", CreatedAt: 1})
	c := &fakeConn{}
	_ = hub.Attach(id, c)

	if err := hub.HandleHeartbeat(id, c); err != nil {
		t.Fatalf("HandleHeartbeat: %v", err)
	}
	if c.writeCount() != 1 {
		t.Fatal("ack not written")
	}
	var ack model.HeartbeatAck
	if err := json.Unmarshal(c.writes[0], &ack); err != nil {
		t.Fatalf("ack json: %v", err)
	}
	if ack.Type != model.FrameHeartbeatAck {
		t.Fatalf("ack type = %s", ack.Type)
	}
}

func TestHub_SweeperHeartbeatTimeout(t *testing.T) {
	hub, repo := newTestHub(t)
	ctx := context.Background()
	id, _ := repo.Create(ctx, &model.Agent{TokenHash: "h", Hostname: "a", HostFingerprint: "f", CreatedAt: 1})
	c := &fakeConn{}
	_ = hub.Attach(id, c)

	offlineCh := make(chan int64, 4)
	hub.SetOnOffline(func(agentID int64) { offlineCh <- agentID })

	// 时间前进 120s(> 90s 超时)
	base := time.Now()
	hub.SetClock(func() time.Time { return base.Add(120 * time.Second) })
	go hub.RunSweeper(ctx, 10*time.Millisecond)

	deadline := time.After(2 * time.Second)
	for {
		if c.isClosed() && !hub.IsOnline(id) {
			break
		}
		select {
		case <-deadline:
			t.Fatal("sweeper did not close stale conn / mark offline")
		case <-time.After(5 * time.Millisecond):
		}
	}
	select {
	case got := <-offlineCh:
		if got != id {
			t.Fatalf("offline hook got %d", got)
		}
	case <-time.After(time.Second):
		t.Fatal("offline hook not called by sweeper")
	}
}
