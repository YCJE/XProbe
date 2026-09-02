// Package state 实现 Agent 本地状态持久化(state.json, 设计文档 4.3/4.4)。
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/YCJE/XProbe/internal/model"
)

// State 持久化的流量状态。
type State struct {
	Month string `json:"month"` // UTC 月标记 "2026-08"
	// RxBytes/TxBytes 当月累计收发字节数(由每周期增量累加, 跨重启续算)。
	RxBytes uint64 `json:"rx_bytes"`
	TxBytes uint64 `json:"tx_bytes"`
	// LastRxTotal/LastTxTotal 上次保存时观测到的网卡累计原始计数器,
	// 用于检测计数器回绕/重启归零(delta 为负时按归零处理)。
	LastRxTotal uint64 `json:"last_rx_total"`
	LastTxTotal uint64 `json:"last_tx_total"`
	Initialized bool   `json:"initialized"`
}

// Tracker 维护月度流量累计, 实现 collector.TrafficTracker 接口。
type Tracker struct {
	mu   sync.Mutex
	path string // 为空表示内存模式(--once 演示), 不落盘
	now  func() time.Time
	st   State
}

// Load 读取状态文件; 文件不存在或损坏时以当前 UTC 月从零初始化(重新累计, 不阻断采集)。
func Load(path string, now func() time.Time) (*Tracker, error) {
	if now == nil {
		now = time.Now
	}
	t := &Tracker{path: path, now: now, st: State{Month: model.MonthOf(now())}}
	if path == "" {
		return t, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return t, nil
		}
		return nil, fmt.Errorf("read state file: %w", err)
	}
	var st State
	if err := json.Unmarshal(b, &st); err != nil || st.Month == "" {
		return t, nil // 损坏 → 重新累计
	}
	t.st = st
	return t, nil
}

// Update 传入本周期观测到的网卡累计字节数(lo 除外), 返回当前月度累计。
// 每次调用都会原子落盘(上报周期 3s, 写入量可忽略)。
func (t *Tracker) Update(rxTotal, txTotal uint64) model.TrafficMonthly {
	t.mu.Lock()
	defer t.mu.Unlock()

	if cur := model.MonthOf(t.now()); cur != t.st.Month {
		// 跨自然月: 归零重新累计(设计文档 4.4, 月界 UTC)
		t.st.Month = cur
		t.st.RxBytes, t.st.TxBytes = 0, 0
	}

	if t.st.Initialized {
		t.st.RxBytes += delta(t.st.LastRxTotal, rxTotal)
		t.st.TxBytes += delta(t.st.LastTxTotal, txTotal)
	} else {
		// 首次安装: 以当前读数为基线, 不把安装前的历史流量计入
		t.st.Initialized = true
	}
	t.st.LastRxTotal, t.st.LastTxTotal = rxTotal, txTotal

	t.saveLocked()
	return model.TrafficMonthly{Month: t.st.Month, RxBytes: t.st.RxBytes, TxBytes: t.st.TxBytes}
}

// delta 计算计数器增量; cur < last 视为重启归零/回绕, 增量取 cur 全量。
func delta(last, cur uint64) uint64 {
	if cur < last {
		return cur
	}
	return cur - last
}

func (t *Tracker) saveLocked() {
	if t.path == "" {
		return
	}
	b, err := json.Marshal(&t.st)
	if err != nil {
		return
	}
	dir := filepath.Dir(t.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("[state] mkdir %s: %v", dir, err)
		return
	}
	// 原子写: 临时文件 + rename, 避免半写状态(权限 0600, S8)
	tmp := t.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		log.Printf("[state] write %s: %v", tmp, err) // 磁盘满等: 至少留痕, 流量累计仍内存有效
		return
	}
	if err := os.Rename(tmp, t.path); err != nil {
		log.Printf("[state] rename %s: %v", t.path, err)
	}
}
