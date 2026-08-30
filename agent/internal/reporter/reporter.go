// Package reporter 实现 Agent WebSocket 上报客户端(设计文档 5.2):
// 强制 wss、断线指数退避 + ±20% jitter、3s 上报、30s 心跳、读侧仅消费 heartbeat_ack。
package reporter

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"log"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/YCJE/XProbe/internal/model"
)

const (
	MaxBackoff = 60 * time.Second
	// WriteTimeout 写超时, 防慢速连接(设计文档 5.2 v1.3)
	WriteTimeout = 10 * time.Second
)

// DefaultDial 生产环境拨号器: wss + 证书 Pinning TLS 配置 + 可选压缩。
func DefaultDial(wsURL string, tlsConf *tls.Config, compression bool) DialFunc {
	d := websocket.Dialer{
		TLSClientConfig:   tlsConf,
		EnableCompression: compression,
		HandshakeTimeout:  15 * time.Second,
	}
	return func(ctx context.Context, header http.Header) (*websocket.Conn, error) {
		conn, _, err := d.Dial(wsURL, header)
		return conn, err
	}
}

// NextBackoff 断线重连退避序列: base→2×→4×→…→60s 上限。
func NextBackoff(base, prev time.Duration) time.Duration {
	if prev <= 0 {
		if base <= 0 {
			base = time.Second
		}
		return base
	}
	next := prev * 2
	if next > MaxBackoff || next <= 0 {
		return MaxBackoff
	}
	return next
}

// WithJitter 加 ±20% 抖动, r∈[0,1)(设计文档 5.2: 防重连风暴)。
func WithJitter(d time.Duration, r float64) time.Duration {
	if d <= 0 {
		return 0
	}
	return time.Duration(float64(d) * (0.8 + 0.4*r))
}

// DialFunc 建立到 Server 的 WebSocket 连接(生产实现校验 wss+TLS Pinning)。
type DialFunc func(ctx context.Context, header http.Header) (*websocket.Conn, error)

// CollectFunc 采集一帧上报数据。
type CollectFunc func(ctx context.Context) (model.Report, error)

// PingCollectFunc 执行一轮完整探测并返回结果(设计文档 4.6)。
type PingCollectFunc func(ctx context.Context) ([]model.PingResult, error)

type Client struct {
	WSURL             string
	Token             string
	Fingerprint       string
	ReportInterval    time.Duration
	HeartbeatInterval time.Duration
	// BackoffStart 重连退避基数, 生产默认 1s(设计文档 5.2); 测试可调小。
	BackoffStart time.Duration
	PingInterval time.Duration // 探测周期, 默认 60s(设计文档 4.6)
	Dial         DialFunc
	Collect      CollectFunc
	PingCollect  PingCollectFunc
}

func (c *Client) logf(format string, args ...any) {
	log.Printf("[reporter] "+format, args...)
}

// Run 主循环: 连接 → 会话(上报+心跳) → 断开后带 jitter 退避重连, 直至 ctx 取消。
func (c *Client) Run(ctx context.Context) error {
	base := c.BackoffStart
	if base <= 0 {
		base = time.Second
	}
	var backoff time.Duration
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		conn, err := c.dial(ctx)
		if err != nil {
			c.logf("dial failed: %v", err)
			backoff = NextBackoff(base, backoff)
			if !c.sleep(ctx, WithJitter(backoff, rand.Float64())) {
				return ctx.Err()
			}
			continue
		}
		backoff = 0
		c.session(ctx, conn)
		if err := ctx.Err(); err != nil {
			return err
		}
		backoff = NextBackoff(base, backoff)
		if !c.sleep(ctx, WithJitter(backoff, rand.Float64())) {
			return ctx.Err()
		}
	}
}

func (c *Client) dial(ctx context.Context) (*websocket.Conn, error) {
	h := http.Header{}
	h.Set("Authorization", "Bearer "+c.Token)
	if c.Fingerprint != "" {
		h.Set("X-Host-Fingerprint", c.Fingerprint)
	}
	return c.Dial(ctx, h)
}

// session 单连接生命周期: 立即上报一帧 → 定时上报/心跳; 读侧只消费 ack, 出错即结束。
func (c *Client) session(ctx context.Context, conn *websocket.Conn) {
	defer conn.Close()

	var writeMu sync.Mutex
	send := func(v any) error {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(WriteTimeout))
		return conn.WriteMessage(websocket.TextMessage, b)
	}

	errCh := make(chan error, 2)
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				errCh <- err
				return
			}
			// 读侧仅丢弃(heartbeat_ack), 协议上不存在其他下行帧(设计文档 5.2)
		}
	}()

	reportT := time.NewTicker(c.ReportInterval)
	defer reportT.Stop()
	hbT := time.NewTicker(c.HeartbeatInterval)
	defer hbT.Stop()
	pingInterval := c.PingInterval
	if pingInterval <= 0 {
		pingInterval = 60 * time.Second
	}
	pingT := time.NewTicker(pingInterval)
	defer pingT.Stop()

	// 连接建立立即上报首帧
	if r, err := c.Collect(ctx); err == nil {
		if serr := send(r); serr != nil {
			c.logf("send first report: %v", serr)
			return
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-errCh:
			return
		case <-reportT.C:
			r, err := c.Collect(ctx)
			if err != nil {
				c.logf("collect: %v", err)
				continue
			}
			if err := send(r); err != nil {
				c.logf("send report: %v", err)
				return
			}
		case <-hbT.C:
			if err := send(model.Heartbeat{Type: model.FrameHeartbeat, Timestamp: time.Now().Unix()}); err != nil {
				c.logf("send heartbeat: %v", err)
				return
			}
		case <-pingT.C:
			if c.PingCollect == nil {
				continue
			}
			results, perr := c.PingCollect(ctx)
			if perr != nil {
				c.logf("ping collect: %v", perr)
				continue
			}
			if len(results) == 0 {
				continue
			}
			if err := send(model.PingReport{Type: model.FramePingResult, Data: results}); err != nil {
				c.logf("send ping_result: %v", err)
				return
			}
		}
	}
}

func (c *Client) sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
