package collector

import (
	"fmt"
	"sync"

	"github.com/YCJE/XProbe/internal/model"
)

// Agent 编排全部采集器, 输出一帧完整上报数据。
// 并发安全: 采集器内部各自持状态, Agent 串行调用 CollectReport(上报间隔 3s, 远大于采集耗时)。
type Agent struct {
	cpu     *CPU
	mem     *Memory
	disk    *Disk
	net     *Network
	sys     *System
	traffic TrafficTracker

	mu       sync.Mutex
	hostname string
}

// TrafficTracker 是月度流量累计的最小接口(实现在 internal/state, 避免 collector 依赖磁盘持久化)。
type TrafficTracker interface {
	Update(rxTotal, txTotal uint64) model.TrafficMonthly
}

func NewAgent(src Sources, traffic TrafficTracker) *Agent {
	return &Agent{
		cpu:     NewCPU(src),
		mem:     NewMemory(src),
		disk:    NewDisk(src),
		net:     NewNetwork(src),
		sys:     NewSystem(src),
		traffic: traffic,
	}
}

// SetHostname 由调用方注入(与 src.Hostname 一致), 输出帧顶层使用。
func (a *Agent) SetHostname(h string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.hostname = h
}

// CollectReport 采集一帧完整数据。
// 单个子采集器失败不阻塞其余字段: 返回错误仅表示本帧完全不可用。
func (a *Agent) CollectReport() (model.ReportData, error) {
	data := model.ReportData{}

	cpu, err := a.cpu.Collect()
	if err != nil {
		return data, fmt.Errorf("cpu: %w", err)
	}
	data.CPU = cpu

	if mem, err := a.mem.Collect(); err == nil {
		data.Memory = mem
	}
	if disks, err := a.disk.Collect(); err == nil {
		data.Disk = disks
	}

	net, nerr := a.net.Collect()
	if nerr == nil {
		data.Network = net
	}

	// 月度流量: 基于网卡累计字节数(跨重启续算, UTC 月界), 依赖 net 已读取累计值。
	if rx, tx, ok := a.net.Totals(); ok && a.traffic != nil {
		data.TrafficMonthly = a.traffic.Update(rx, tx)
	}

	data.Uptime, data.System, data.ProcessCount, _ = a.sys.Collect()
	return data, nil
}

// Hostname 返回注入的主机名。
func (a *Agent) Hostname() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.hostname
}
