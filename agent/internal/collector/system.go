package collector

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"

	"github.com/YCJE/XProbe/internal/model"
	"github.com/YCJE/XProbe/internal/version"
)

// System 采集器: 运行时间 / 系统信息 / 进程数(设计文档 4.1)。
// 只统计进程数量, 不读取其他用户进程详情(S7 最小权限采集)。
type System struct {
	src Sources
}

func NewSystem(src Sources) *System { return &System{src: src} }

func (s *System) Collect() (uptime uint64, info model.SystemInfo, procCount int, err error) {
	uptime, err = s.uptime()
	if err != nil {
		return 0, info, 0, err
	}
	info = model.SystemInfo{
		Kernel:       runtime.GOARCH,
		Arch:         runtime.GOARCH,
		AgentVersion: version.Version,
	}
	if u, uerr := s.src.Uname(); uerr == nil {
		info.Kernel = u.Release
	}
	if b, rerr := s.src.ReadFile("/etc/os-release"); rerr == nil {
		info.OS = parseOSRelease(b)
	}
	procCount, perr := s.processCount()
	if perr != nil {
		return uptime, info, 0, nil // 进程数采集失败不阻塞整体上报
	}
	return uptime, info, procCount, nil
}

func (s *System) uptime() (uint64, error) {
	b, err := s.src.ReadFile("/proc/uptime")
	if err != nil {
		return 0, fmt.Errorf("read /proc/uptime: %w", err)
	}
	return parseUptime(b)
}

func (s *System) processCount() (int, error) {
	ents, err := s.src.ReadDir("/proc")
	if err != nil {
		return 0, fmt.Errorf("read /proc: %w", err)
	}
	n := 0
	for _, e := range ents {
		if _, err := strconv.Atoi(e.Name); err == nil {
			n++
		}
	}
	return n, nil
}

func parseUptime(b []byte) (uint64, error) {
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return 0, fmt.Errorf("parseUptime: empty input")
	}
	f, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, fmt.Errorf("parseUptime: %w", err)
	}
	if f < 0 {
		return 0, nil
	}
	return uint64(f), nil
}

// parseOSRelease 提取 /etc/os-release 的 PRETTY_NAME。
func parseOSRelease(b []byte) string {
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			v := strings.TrimPrefix(line, "PRETTY_NAME=")
			return strings.Trim(v, `"'`)
		}
	}
	return ""
}
