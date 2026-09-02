package pkg

import (
	"context"
	"sync"
	"time"
)

// Limiter 内存固定窗口限速器(按 key, 通常为 IP)。
// 用于: 注册接口 5 次/分钟/IP、登录 5 次/分钟/IP、全局 API 120 次/分钟/IP(设计文档 8.2)。
// 单机单进程场景, 无外部依赖; Server 重启后窗口清零可接受(限速是防爆破辅助手段)。
type Limiter struct {
	limit  int
	window time.Duration
	now    func() time.Time

	mu   sync.Mutex
	hits map[string]*hitWindow
	// 最新一次 GC 时间, 避免每次 Allow 都扫描
	lastGC time.Time
}

type hitWindow struct {
	start time.Time
	count int
}

func NewLimiter(limit int, window time.Duration) *Limiter {
	return &Limiter{
		limit:  limit,
		window: window,
		now:    time.Now,
		hits:   map[string]*hitWindow{},
	}
}

// Allow 记录一次命中并返回是否放行。
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.allowLocked(key)
}

func (l *Limiter) allowLocked(key string) bool {
	now := l.now()
	w, ok := l.hits[key]
	if !ok || now.Sub(w.start) >= l.window {
		w = &hitWindow{start: now}
		l.hits[key] = w
	}
	w.count++
	if w.count > l.limit {
		l.gcLocked(now)
		return false
	}
	return true
}

// gcLocked 清理早已过期的窗口, 控制内存; 至少间隔一个 window 扫一次。
func (l *Limiter) gcLocked(now time.Time) {
	if now.Sub(l.lastGC) < l.window {
		return
	}
	l.lastGC = now
	for k, w := range l.hits {
		if now.Sub(w.start) >= l.window {
			delete(l.hits, k)
		}
	}
}

// StartGC 后台定时回收过期窗口(审查 LOW #12: 防唯一 IP 慢泄漏)。
func (l *Limiter) StartGC(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			l.mu.Lock()
			now := l.now()
			for k, w := range l.hits {
				if now.Sub(w.start) >= l.window {
					delete(l.hits, k)
				}
			}
			l.mu.Unlock()
		}
	}
}

// Remaining 返回 key 当前窗口剩余额度(测试与可观测用)。
func (l *Limiter) Remaining(key string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	w, ok := l.hits[key]
	if !ok || l.now().Sub(w.start) >= l.window {
		return l.limit
	}
	r := l.limit - w.count
	if r < 0 {
		return 0
	}
	return r
}
