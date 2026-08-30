package collector

import (
	"bytes"
	"fmt"
	"runtime"
	"strconv"
	"strings"

	"github.com/YCJE/XProbe/internal/model"
)

// CPU 采集器: /proc/stat 使用率(两次采样差值) + /proc/cpuinfo 型号与核心数 + /proc/loadavg 负载。
// 首次 Collect 时 Usage 为 nil(设计文档 4.1: 首采样置空, 避免单采样假值)。
type CPU struct {
	src  Sources
	prev *procStat
}

func NewCPU(src Sources) *CPU { return &CPU{src: src} }

// procStat 是 /proc/stat 首行 "cpu" 的累计节拍计数。
type procStat struct {
	user, nice, system, idle, iowait, irq, softirq, steal uint64
}

func (c *CPU) Collect() (model.CPUInfo, error) {
	b, err := c.src.ReadFile("/proc/stat")
	if err != nil {
		return model.CPUInfo{}, fmt.Errorf("read /proc/stat: %w", err)
	}
	cur, err := parseProcStat(b)
	if err != nil {
		return model.CPUInfo{}, err
	}

	info := c.staticInfo()
	if c.prev != nil {
		u := cpuUsage(*c.prev, cur)
		info.Usage = &u
	}
	c.prev = &cur
	return info, nil
}

// StaticInfo 公开静态信息采集(注册流程用于主机指纹与系统元数据)。
func (c *CPU) StaticInfo() model.CPUInfo { return c.staticInfo() }

// staticInfo 采集核心数/型号/负载, 不依赖采样差值。
func (c *CPU) staticInfo() model.CPUInfo {
	info := model.CPUInfo{}
	if b, err := c.src.ReadFile("/proc/cpuinfo"); err == nil {
		info.Model, info.Cores = parseCPUInfo(b)
	}
	if info.Cores == 0 {
		info.Cores = runtime.NumCPU()
	}
	if b, err := c.src.ReadFile("/proc/loadavg"); err == nil {
		info.Load1, info.Load5, info.Load15, _ = parseLoadavg(b)
	}
	return info
}

func parseProcStat(b []byte) (procStat, error) {
	// 只取首行 "cpu  u n s i i i s s"(聚合值)。
	line := ""
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "cpu ") {
			line = l
			break
		}
	}
	if line == "" {
		return procStat{}, fmt.Errorf("parseProcStat: no aggregate cpu line")
	}
	fields := strings.Fields(line)[1:]
	if len(fields) < 8 {
		return procStat{}, fmt.Errorf("parseProcStat: expected >=8 fields, got %d", len(fields))
	}
	var v [8]uint64
	for i := 0; i < 8; i++ {
		n, err := strconv.ParseUint(fields[i], 10, 64)
		if err != nil {
			return procStat{}, fmt.Errorf("parseProcStat field %d: %w", i, err)
		}
		v[i] = n
	}
	return procStat{user: v[0], nice: v[1], system: v[2], idle: v[3],
		iowait: v[4], irq: v[5], softirq: v[6], steal: v[7]}, nil
}

// cpuUsage 按标准公式: 1 - idleDelta/totalDelta, 结果 0-100。
func cpuUsage(prev, cur procStat) float64 {
	idle := func(s procStat) uint64 { return s.idle + s.iowait }
	total := func(s procStat) uint64 {
		return s.user + s.nice + s.system + s.idle + s.iowait + s.irq + s.softirq + s.steal
	}
	idleDelta := idle(cur) - idle(prev)
	totalDelta := total(cur) - total(prev)
	if totalDelta == 0 {
		return 0
	}
	u := (1 - float64(idleDelta)/float64(totalDelta)) * 100
	if u < 0 {
		u = 0
	}
	if u > 100 {
		u = 100
	}
	return u
}

// parseCPUInfo 返回 (型号, 物理核心计数=processor 行数)。
// ARM 等平台可能没有 "model name" 行, 型号允许为空。
func parseCPUInfo(b []byte) (string, int) {
	modelName := ""
	cores := 0
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "processor"):
			cores++
		case strings.HasPrefix(line, "model name") && modelName == "":
			modelName = valueAfterColon(line)
		case strings.HasPrefix(line, "Model Name") && modelName == "":
			modelName = valueAfterColon(line)
		}
	}
	return modelName, cores
}

func valueAfterColon(line string) string {
	i := strings.Index(line, ":")
	if i < 0 {
		return ""
	}
	return strings.TrimSpace(line[i+1:])
}

func parseLoadavg(b []byte) (l1, l5, l15 float64, err error) {
	fields := strings.Fields(string(bytes.TrimSpace(b)))
	if len(fields) < 3 {
		return 0, 0, 0, fmt.Errorf("parseLoadavg: expected >=3 fields, got %d", len(fields))
	}
	vals := make([]float64, 3)
	for i := 0; i < 3; i++ {
		v, err := strconv.ParseFloat(fields[i], 64)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("parseLoadavg field %d: %w", i, err)
		}
		vals[i] = v
	}
	return vals[0], vals[1], vals[2], nil
}
