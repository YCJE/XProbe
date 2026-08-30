package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/YCJE/XProbe/internal/model"
	"github.com/YCJE/XProbe/server/internal/repository"
)

// WSConn 是 Hub 需要的最小连接接口(*websocket.Conn 天然满足, 测试注入假实现)。
type WSConn interface {
	ReadMessage() (messageType int, p []byte, err error)
	WriteMessage(messageType int, data []byte) error
	SetReadLimit(limit int64)
	SetReadDeadline(t time.Time) error
	SetWriteDeadline(t time.Time) error
	Close() error
}

// Frame 传输限制与离线判定常量(设计文档 5.2/7.6, v1.3)。
const (
	// ReadLimit 单帧读上限: 超出即断开(正常 report 帧 ≤10KB)。
	ReadLimit int64 = 64 << 10
	// WriteTimeout 写超时, 防慢速连接堆积。
	WriteTimeout = 10 * time.Second
	// MinReportInterval report 频率下限(3s±1s 校验区间), 过快拒绝防刷。
	MinReportInterval = 2 * time.Second
)

var (
	ErrReportTooFast  = errors.New("monitor: report frequency too high")
	ErrAgentNotOnline = errors.New("monitor: agent not connected")
)

// Hub 管理 Agent 连接池、实时数据环形缓冲与在线状态(设计文档 5.3)。
//
// 离线检测双路径(v1.3): Detach(WS close 事件, 即时) + RunSweeper(心跳超时兜底)。
// 心跳超时(默认 90s)仅作为网络半开连接的兜底。
type Hub struct {
	mu         sync.Mutex
	conns      map[int64]WSConn
	lastSeen   map[int64]time.Time
	lastReport map[int64]time.Time
	reports    map[int64]*repository.RingBuffer[model.Report]
	pings      map[int64]*repository.RingBuffer[[]model.PingResult]

	repo             *repository.AgentRepo
	heartbeatTimeout time.Duration
	now              func() time.Time
	onOffline        func(agentID int64) // 告警钩子(M5 接线), 可为 nil
}

func NewHub(repo *repository.AgentRepo, heartbeatTimeout time.Duration) *Hub {
	return &Hub{
		conns:            map[int64]WSConn{},
		lastSeen:         map[int64]time.Time{},
		lastReport:       map[int64]time.Time{},
		reports:          map[int64]*repository.RingBuffer[model.Report]{},
		pings:            map[int64]*repository.RingBuffer[[]model.PingResult]{},
		repo:             repo,
		heartbeatTimeout: heartbeatTimeout,
		now:              time.Now,
	}
}

func (h *Hub) SetOnOffline(fn func(agentID int64)) { h.onOffline = fn }

// HeartbeatTimeout 返回心跳超时配置(WS 读截止时间用)。
func (h *Hub) HeartbeatTimeout() time.Duration { return h.heartbeatTimeout }

// SetClock 覆盖时钟(测试用)。
func (h *Hub) SetClock(now func() time.Time) { h.now = now }

// Attach 登记 Agent 连接并标记在线; 同一 Agent 旧连接被踢下线(关闭)。
func (h *Hub) Attach(id int64, c WSConn) error {
	h.mu.Lock()
	old := h.conns[id]
	h.conns[id] = c
	h.lastSeen[id] = h.now()
	h.lastReport[id] = time.Time{}
	if _, ok := h.reports[id]; !ok {
		h.reports[id] = repository.NewRingBuffer[model.Report](3600)   // 3s/点 × 3h
		h.pings[id] = repository.NewRingBuffer[[]model.PingResult](60) // 60s/点 × 1h
	}
	h.mu.Unlock()

	if old != nil {
		_ = old.Close() // 触发旧读循环退出 → 旧 Detach(发现 conn 已替换不会覆盖状态)
	}
	return h.repo.Touch(context.Background(), id, true, h.now().Unix())
}

// Detach 连接关闭时调用: 即时标记离线(v1.3 双路径之主路径)。
func (h *Hub) Detach(id int64, c WSConn) {
	h.mu.Lock()
	if h.conns[id] != c {
		h.mu.Unlock()
		return // 已被新连接替换, 不改状态
	}
	delete(h.conns, id)
	h.mu.Unlock()

	if err := h.repo.Touch(context.Background(), id, false, h.now().Unix()); err != nil {
		log.Printf("[monitor] mark offline agent=%d: %v", id, err)
	}
	if h.onOffline != nil {
		h.onOffline(id)
	}
}

// HandleReport 校验并写入 report 帧; 频率过快拒绝(防刷, 设计文档 7.6)。
func (h *Hub) HandleReport(id int64, r *model.Report) error {
	if err := ValidateReport(r); err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now()
	if !h.lastReport[id].IsZero() && now.Sub(h.lastReport[id]) < MinReportInterval {
		return ErrReportTooFast
	}
	h.lastReport[id] = now
	h.lastSeen[id] = now
	if buf, ok := h.reports[id]; ok {
		buf.Push(*r)
	}
	return nil
}

// HandlePing 校验并写入 ping_result 帧。
func (h *Hub) HandlePing(id int64, ps []model.PingResult) error {
	if err := ValidatePingResults(ps); err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lastSeen[id] = h.now()
	if buf, ok := h.pings[id]; ok {
		buf.Push(ps)
	}
	return nil
}

// HandleHeartbeat 更新活跃时间并回 heartbeat_ack(Server→Agent 唯一帧)。
func (h *Hub) HandleHeartbeat(id int64, c WSConn) error {
	h.mu.Lock()
	h.lastSeen[id] = h.now()
	h.mu.Unlock()

	ack, _ := json.Marshal(model.HeartbeatAck{Type: model.FrameHeartbeatAck})
	_ = c.SetWriteDeadline(h.now().Add(WriteTimeout))
	return c.WriteMessage(1, ack)
}

func (h *Hub) IsOnline(id int64) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.conns[id]
	return ok
}

func (h *Hub) OnlineCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.conns)
}

// RunSweeper 心跳超时兜底(设计文档 5.2): lastSeen 超时的连接主动 Close 并直接落库离线——
// 连接已从池中删除, 其后读循环触发的 Detach 会因 conn 不匹配而跳过, 不能依赖它标记离线。
func (h *Hub) RunSweeper(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			h.mu.Lock()
			now := h.now()
			var stale []struct {
				id int64
				c  WSConn
			}
			for id, c := range h.conns {
				if now.Sub(h.lastSeen[id]) > h.heartbeatTimeout {
					stale = append(stale, struct {
						id int64
						c  WSConn
					}{id, c})
					delete(h.conns, id)
					log.Printf("[monitor][security] heartbeat timeout agent=%d", id)
				}
			}
			h.mu.Unlock()
			for _, s := range stale {
				if err := h.repo.Touch(context.Background(), s.id, false, now.Unix()); err != nil {
					log.Printf("[monitor] mark offline agent=%d: %v", s.id, err)
				}
				if h.onOffline != nil {
					h.onOffline(s.id)
				}
				_ = s.c.Close() // 读循环退出; 其后的 Detach 为无副作用空操作
			}
		}
	}
}

func (h *Hub) PingSnapshot(id int64) []model.PingResult {
	h.mu.Lock()
	defer h.mu.Unlock()
	if buf, ok := h.pings[id]; ok {
		// RingBuffer[[]PingResult] 展平: 返回最近一次每目标的最新结果
		var latest []model.PingResult
		if buf.Len() > 0 {
			snap := buf.Latest(1)
			if len(snap) > 0 {
				latest = snap[0]
			}
		}
		return latest
	}
	return nil
}

// ReportSnapshot 返回全部实时帧(旧→新, 含时间戳)。
func (h *Hub) ReportSnapshot(id int64) []model.Report {
	h.mu.Lock()
	defer h.mu.Unlock()
	if buf, ok := h.reports[id]; ok {
		return buf.Snapshot()
	}
	return nil
}

// LatestReport 返回该 Agent 最新一帧实时数据。
func (h *Hub) LatestReport(id int64) (model.ReportData, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	buf, ok := h.reports[id]
	if !ok {
		return model.ReportData{}, false
	}
	snap := buf.Latest(1)
	if len(snap) == 0 {
		return model.ReportData{}, false
	}
	return snap[0].Data, true
}

// LatestPing 返回该 Agent 最近一轮探测结果。
func (h *Hub) LatestPing(id int64) ([]model.PingResult, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	buf, ok := h.pings[id]
	if !ok {
		return nil, false
	}
	snap := buf.Latest(1)
	if len(snap) == 0 {
		return nil, false
	}
	return snap[0], true
}

// Drop 服务端主动断开某 Agent(删除记录后调用)。
func (h *Hub) Drop(id int64) {
	h.mu.Lock()
	c := h.conns[id]
	delete(h.conns, id)
	delete(h.reports, id)
	delete(h.pings, id)
	delete(h.lastSeen, id)
	delete(h.lastReport, id)
	h.mu.Unlock()
	if c != nil {
		_ = c.Close()
	}
}

// Describe 连接调试信息。
func (h *Hub) Describe(id int64) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, online := h.conns[id]
	return fmt.Sprintf("agent=%d online=%t", id, online)
}
