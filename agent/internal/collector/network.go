package collector

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/YCJE/XProbe/internal/model"
)

// Network 采集器: /proc/net/dev 速率差值 + /proc/net/{tcp,tcp6,udp,udp6} 连接数(设计文档 4.1)。
// 速率 = (当前累计字节 - 上次累计字节) / 采样间隔; 首次调用速率为 0。
type Network struct {
	src      Sources
	prev     *netTotals
	prevTime time.Time
	now      func() time.Time
}

type netTotals struct {
	rxBytes uint64
	txBytes uint64
}

func NewNetwork(src Sources) *Network {
	return &Network{src: src, now: time.Now}
}

func (n *Network) Collect() (model.NetworkInfo, error) {
	dev, err := n.src.ReadFile("/proc/net/dev")
	if err != nil {
		return model.NetworkInfo{}, fmt.Errorf("read /proc/net/dev: %w", err)
	}
	perIf, err := parseNetDev(dev)
	if err != nil {
		return model.NetworkInfo{}, err
	}
	cur := sumNetDev(perIf)

	info := model.NetworkInfo{}
	if n.prev != nil {
		dt := n.now().Sub(n.prevTime)
		if dt > 0 {
			info.RxSpeed = rate(n.prev.rxBytes, cur.rxBytes, dt)
			info.TxSpeed = rate(n.prev.txBytes, cur.txBytes, dt)
		}
	}
	n.prev, n.prevTime = &cur, n.now()

	tcp, err := n.countConnections("/proc/net/tcp", "/proc/net/tcp6")
	if err != nil {
		return info, err
	}
	info.TCPConnections = tcp
	udp, err := n.countConnections("/proc/net/udp", "/proc/net/udp6")
	if err != nil {
		return info, err
	}
	info.UDPConnections = udp
	return info, nil
}

// Totals 返回最近一次读取的网卡累计字节数(lo 除外), 供流量月累计 Tracker 使用。
// 未 Collect 过时返回 false。
func (n *Network) Totals() (rx, tx uint64, ok bool) {
	if n.prev == nil {
		return 0, 0, false
	}
	return n.prev.rxBytes, n.prev.txBytes, true
}

// countConnections 统计 v4+v6 两个文件的记录行数(首行表头除外)。
// 计入所有状态(与 Nezha/gopsutil 口径一致)。
func (n *Network) countConnections(paths ...string) (int, error) {
	total := 0
	for _, p := range paths {
		b, err := n.src.ReadFile(p)
		if err != nil {
			if p[len(p)-1] == '6' {
				continue // 无 IPv6 时文件可能不存在, 跳过
			}
			return 0, fmt.Errorf("read %s: %w", p, err)
		}
		total += countProcNetLines(b)
	}
	return total, nil
}

func rate(prev, cur uint64, dt time.Duration) uint64 {
	if cur < prev {
		return 0 // 计数器回绕/重启, 本周期丢弃
	}
	bytes := cur - prev
	secs := dt.Seconds()
	if secs <= 0 {
		return 0
	}
	return uint64(float64(bytes) / secs)
}

type perIfNet struct {
	rxBytes uint64
	txBytes uint64
}

// parseNetDev 解析 /proc/net/dev: iface → 累计字节数。
func parseNetDev(b []byte) (map[string]perIfNet, error) {
	out := map[string]perIfNet{}
	lines := strings.Split(string(b), "\n")
	if len(lines) < 3 {
		return nil, fmt.Errorf("parseNetDev: unexpected format")
	}
	for _, line := range lines[2:] { // 前两行为表头
		i := strings.Index(line, ":")
		if i < 0 {
			continue
		}
		iface := strings.TrimSpace(line[:i])
		if iface == "" || iface == "lo" {
			continue
		}
		fields := strings.Fields(line[i+1:])
		if len(fields) < 10 {
			continue
		}
		rx, err1 := strconv.ParseUint(fields[0], 10, 64)
		tx, err2 := strconv.ParseUint(fields[8], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		out[iface] = perIfNet{rxBytes: rx, txBytes: tx}
	}
	return out, nil
}

func sumNetDev(m map[string]perIfNet) netTotals {
	var t netTotals
	for _, v := range m {
		t.rxBytes += v.rxBytes
		t.txBytes += v.txBytes
	}
	return t
}

// countProcNetLines 统计 /proc/net/tcp 样式文件的数据行(跳过首行表头与空行)。
// 数据行形如 "  0: 0100007F:0035 00000000:0000 0A ...", 首个 token 以冒号结尾。
func countProcNetLines(b []byte) int {
	n := 0
	for i, line := range strings.Split(string(b), "\n") {
		if i == 0 {
			continue // 表头: sl local_address rem_address st ...
		}
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if f := strings.Fields(t); len(f) > 0 && strings.HasSuffix(f[0], ":") {
			n++
		}
	}
	return n
}
