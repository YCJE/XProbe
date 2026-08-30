package collector

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/YCJE/XProbe/internal/model"
)

// Memory 采集器: /proc/meminfo 的内存与 Swap 使用(设计文档 4.1)。
type Memory struct {
	src Sources
}

func NewMemory(src Sources) *Memory { return &Memory{src: src} }

func (m *Memory) Collect() (model.MemoryInfo, error) {
	b, err := m.src.ReadFile("/proc/meminfo")
	if err != nil {
		return model.MemoryInfo{}, fmt.Errorf("read /proc/meminfo: %w", err)
	}
	return parseMeminfo(b)
}

// parseMeminfo 解析 MemTotal/MemAvailable/SwapTotal/SwapFree(单位 kB→字节)。
// MemAvailable 缺失(内核 <3.14)时回退 MemFree+Buffers+Cached。
func parseMeminfo(b []byte) (model.MemoryInfo, error) {
	kb := map[string]uint64{}
	for _, line := range strings.Split(string(b), "\n") {
		i := strings.Index(line, ":")
		if i < 0 {
			continue
		}
		key := strings.TrimSpace(line[:i])
		fields := strings.Fields(line[i+1:])
		if len(fields) == 0 {
			continue
		}
		n, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue // 非数值行(如 Hugepagesize 之外的异常)忽略
		}
		kb[key] = n * 1024
	}
	total := kb["MemTotal"]
	avail := kb["MemAvailable"]
	if avail == 0 {
		avail = kb["MemFree"] + kb["Buffers"] + kb["Cached"]
	}
	if total == 0 {
		return model.MemoryInfo{}, fmt.Errorf("parseMeminfo: MemTotal missing")
	}
	swapTotal := kb["SwapTotal"]
	swapFree := kb["SwapFree"]
	return model.MemoryInfo{
		Total:     total,
		Used:      total - avail,
		SwapTotal: swapTotal,
		SwapUsed:  swapTotal - swapFree,
	}, nil
}
