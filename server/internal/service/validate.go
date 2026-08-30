package service

import (
	"errors"
	"fmt"
	"strings"

	"github.com/YCJE/XProbe/internal/model"
)

// 数据合理性校验(设计文档 7.6, 防伪造上报 T7)。
// 超范围字段 → 丢弃整帧并记录; WS 传输层另有 64KB 单帧上限。

var ErrInvalidFrame = errors.New("service: frame failed sanity validation")

func validateErr(field string) error {
	return fmt.Errorf("%w: %s out of range", ErrInvalidFrame, field)
}

// ValidateReport 校验 report 帧数值范围。
func ValidateReport(r *model.Report) error {
	if r == nil {
		return ErrInvalidFrame
	}
	if r.Hostname == "" || len(r.Hostname) > 255 {
		return validateErr("hostname")
	}
	if r.Data.CPU.Usage != nil {
		if *r.Data.CPU.Usage < 0 || *r.Data.CPU.Usage > 100 {
			return validateErr("cpu.usage")
		}
	}
	m := r.Data.Memory
	if m.Total > 0 && m.Used > m.Total {
		return validateErr("memory.used")
	}
	if m.SwapTotal > 0 && m.SwapUsed > m.SwapTotal {
		return validateErr("memory.swap_used")
	}
	if len(r.Data.Disk) > 64 {
		return validateErr("disk.count")
	}
	for _, d := range r.Data.Disk {
		if d.Total > 0 && d.Used > d.Total {
			return validateErr("disk.used")
		}
	}
	if len(r.Data.TrafficMonthly.Month) > 0 && !isMonthFormat(r.Data.TrafficMonthly.Month) {
		return validateErr("traffic_monthly.month")
	}
	return nil
}

// ValidatePingResults 校验 ping_result 帧(设计文档 7.6: 延迟 0-60000ms, 丢包 0-100%)。
func ValidatePingResults(ps []model.PingResult) error {
	if len(ps) > 64 {
		return validateErr("ping.count")
	}
	for _, p := range ps {
		if p.Target == "" || len(p.Target) > 255 {
			return validateErr("ping.target")
		}
		if p.IPVersion != 4 && p.IPVersion != 6 {
			return validateErr("ping.ip_version")
		}
		if p.AvgLatency < 0 || p.AvgLatency > 60000 ||
			p.MinLatency < 0 || p.MinLatency > 60000 ||
			p.MaxLatency < 0 || p.MaxLatency > 60000 {
			return validateErr("ping.latency")
		}
		if p.Loss < 0 || p.Loss > 100 {
			return validateErr("ping.loss")
		}
		if p.PacketsRecv > p.PacketsSent {
			return validateErr("ping.packets_recv")
		}
	}
	return nil
}

func isMonthFormat(s string) bool {
	if len(s) != 7 {
		return false
	}
	for i, c := range s {
		if i == 4 {
			if c != '-' {
				return false
			}
			continue
		}
		if c < '0' || c > '9' {
			return false
		}
	}
	return strings.Count(s, "-") == 1
}
