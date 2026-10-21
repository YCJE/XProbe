package collector

import (
	"errors"
	"fmt"
	"time"

	"probe/agent/internal/state"
	"probe/common/wire"
)

// errUnsupported is returned by the non-Linux source stub; v1 targets Linux.
var errUnsupported = errors.New("probe-agent collectors require Linux (v1 platform target)")

// systemReader abstracts platform access so Collect stays compilable and the
// parsers testable on any OS (design doc §4.1: v1 Linux-only).
type systemReader interface {
	readFile(name string) ([]byte, error)
	statfs(path string) (totalBytes, availBytes uint64, err error)
	countProcesses() (int, error)
}

type cpuSample struct {
	busy  float64
	total float64
}

type netSample struct {
	rx uint64
	tx uint64
}

// Collector assembles one wire.ReportData per call. The first call after
// startup reports zero CPU usage and network speeds (no baseline yet) —
// callers sample twice before sending.
type Collector struct {
	version string
	tracker *state.Tracker
	src     systemReader
	now     func() time.Time

	lastCPU  cpuSample
	lastNet  map[string]netSample
	lastTime time.Time
	hasLast  bool
}

// New creates a collector using the real platform source.
func New(version string, tracker *state.Tracker) *Collector {
	return &Collector{
		version: version,
		tracker: tracker,
		src:     systemSource{},
		now:     time.Now,
	}
}

// Collect gathers all M1 metrics (§4.1 collection list minus GPU/ping,
// which arrive with later milestones).
func (c *Collector) Collect() (wire.ReportData, error) {
	now := c.now()
	var data wire.ReportData

	// CPU: usage needs two samples; first call reports 0.
	stat, err := c.src.readFile("/proc/stat")
	if err != nil {
		return data, fmt.Errorf("read /proc/stat: %w", err)
	}
	busy, total, cores, err := ParseStat(stat)
	if err != nil {
		return data, err
	}
	cpuinfo, err := c.src.readFile("/proc/cpuinfo")
	if err != nil {
		return data, fmt.Errorf("read /proc/cpuinfo: %w", err)
	}
	model, infoCores, err := ParseCPUInfo(cpuinfo)
	if err != nil {
		return data, err
	}
	if infoCores > 0 {
		cores = infoCores
	}
	loadRaw, err := c.src.readFile("/proc/loadavg")
	if err != nil {
		return data, fmt.Errorf("read /proc/loadavg: %w", err)
	}
	l1, l5, l15, err := ParseLoadavg(loadRaw)
	if err != nil {
		return data, err
	}

	usage := 0.0
	if c.hasLast {
		if dt := now.Sub(c.lastTime).Seconds(); dt > 0 && total > c.lastCPU.total {
			if db := busy - c.lastCPU.busy; db > 0 {
				usage = db / (total - c.lastCPU.total) * 100
			}
		}
		if usage < 0 {
			usage = 0
		}
		if usage > 100 {
			usage = 100
		}
	}
	data.CPU = wire.CPUMetrics{
		Usage: usage, Cores: cores, Model: model, Load1: l1, Load5: l5, Load15: l15,
	}
	c.lastCPU = cpuSample{busy: busy, total: total}

	// Memory.
	memRaw, err := c.src.readFile("/proc/meminfo")
	if err != nil {
		return data, fmt.Errorf("read /proc/meminfo: %w", err)
	}
	memTotal, memAvail, swapTotal, swapFree, err := ParseMemInfo(memRaw)
	if err != nil {
		return data, err
	}
	data.Memory = wire.MemoryMetrics{
		Total:     memTotal,
		Used:      memTotal - memAvail,
		SwapTotal: swapTotal,
		SwapUsed:  swapTotal - swapFree,
	}

	// Disk: real mounts only, capped at the wire limit (§7.6).
	mountsRaw, err := c.src.readFile("/proc/mounts")
	if err != nil {
		return data, fmt.Errorf("read /proc/mounts: %w", err)
	}
	for _, m := range ParseMounts(mountsRaw) {
		if len(data.Disk) >= wire.MaxDiskEntries {
			break
		}
		totalBytes, availBytes, err := c.src.statfs(m.MountPoint)
		if err != nil || totalBytes == 0 {
			continue
		}
		used := uint64(0)
		if availBytes < totalBytes {
			used = totalBytes - availBytes
		}
		data.Disk = append(data.Disk, wire.DiskMetrics{
			Device: m.Device, Total: totalBytes, Used: used,
		})
	}

	// Network: speeds from per-interface deltas; loopback excluded everywhere.
	devRaw, err := c.src.readFile("/proc/net/dev")
	if err != nil {
		return data, fmt.Errorf("read /proc/net/dev: %w", err)
	}
	devs, err := ParseNetDev(devRaw)
	if err != nil {
		return data, err
	}
	var totalRx, totalTx uint64
	for name, d := range devs {
		if name == "lo" {
			continue
		}
		totalRx += d.RxBytes
		totalTx += d.TxBytes
	}

	var rxSpeed, txSpeed float64
	if c.hasLast {
		if dt := now.Sub(c.lastTime).Seconds(); dt > 0 {
			for name, d := range devs {
				if name == "lo" {
					continue
				}
				last, ok := c.lastNet[name]
				if !ok {
					continue // interface appeared mid-run: skip one cycle
				}
				if d.RxBytes >= last.rx {
					rxSpeed += float64(d.RxBytes-last.rx) / dt
				}
				if d.TxBytes >= last.tx {
					txSpeed += float64(d.TxBytes-last.tx) / dt
				}
			}
		}
	}
	c.lastNet = make(map[string]netSample, len(devs))
	for name, d := range devs {
		if name == "lo" {
			continue
		}
		c.lastNet[name] = netSample{rx: d.RxBytes, tx: d.TxBytes}
	}
	c.lastTime = now

	data.Network.RxSpeed = rxSpeed
	data.Network.TxSpeed = txSpeed

	tcp4, err := c.src.readFile("/proc/net/tcp")
	if err != nil {
		return data, fmt.Errorf("read /proc/net/tcp: %w", err)
	}
	data.Network.TCPConnections = ParseTCPEstablished(tcp4)
	if tcp6, err := c.src.readFile("/proc/net/tcp6"); err == nil {
		data.Network.TCPConnections += ParseTCPEstablished(tcp6)
	}
	udp4, err := c.src.readFile("/proc/net/udp")
	if err != nil {
		return data, fmt.Errorf("read /proc/net/udp: %w", err)
	}
	data.Network.UDPConnections = ParseUDPCount(udp4)
	if udp6, err := c.src.readFile("/proc/net/udp6"); err == nil {
		data.Network.UDPConnections += ParseUDPCount(udp6)
	}

	// Monthly traffic counters (agent-local timezone, §4.4).
	traffic, err := c.tracker.Update(now, totalRx, totalTx)
	if err != nil {
		return data, fmt.Errorf("update traffic state: %w", err)
	}
	data.TrafficMonth = traffic

	// Uptime & process count.
	upRaw, err := c.src.readFile("/proc/uptime")
	if err != nil {
		return data, fmt.Errorf("read /proc/uptime: %w", err)
	}
	uptime, err := ParseUptime(upRaw)
	if err != nil {
		return data, err
	}
	data.Uptime = int64(uptime)

	procs, err := c.src.countProcesses()
	if err != nil {
		return data, fmt.Errorf("count processes: %w", err)
	}
	data.ProcessCount = procs

	c.hasLast = true
	return data, nil
}
