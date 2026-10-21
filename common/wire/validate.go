package wire

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ErrRejected wraps all validation failures; the server logs the cause and
// drops the frame (§7.6: 超范围丢弃，记录安全日志).
var ErrRejected = errors.New("report rejected by validation")

func rejectf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrRejected, fmt.Sprintf(format, args...))
}

// Validate enforces the reasonableness rules from design doc §7.6.
// Zero values are allowed (first sample after startup has no deltas).
func (r Report) Validate() error {
	if len(r.Hostname) == 0 || len(r.Hostname) > MaxHostnameLen {
		return rejectf("hostname empty or longer than %d bytes", MaxHostnameLen)
	}
	if r.Data.CPU.Usage < 0 || r.Data.CPU.Usage > 100 {
		return rejectf("cpu usage out of range: %v", r.Data.CPU.Usage)
	}
	if r.Data.CPU.Cores < 0 {
		return rejectf("negative CPU core count")
	}
	if r.Data.CPU.Load1 < 0 || r.Data.CPU.Load5 < 0 || r.Data.CPU.Load15 < 0 {
		return rejectf("negative load average")
	}
	if r.Data.Memory.Used > r.Data.Memory.Total {
		return rejectf("memory used %d exceeds total %d", r.Data.Memory.Used, r.Data.Memory.Total)
	}
	if r.Data.Memory.SwapUsed > r.Data.Memory.SwapTotal {
		return rejectf("swap used %d exceeds total %d", r.Data.Memory.SwapUsed, r.Data.Memory.SwapTotal)
	}
	if len(r.Data.Disk) > MaxDiskEntries {
		return rejectf("disk entries %d exceeds limit %d", len(r.Data.Disk), MaxDiskEntries)
	}
	for _, d := range r.Data.Disk {
		if len(d.Device) > MaxDiskDeviceLen || !utf8.ValidString(d.Device) ||
			strings.IndexFunc(d.Device, unicode.IsControl) >= 0 {
			return rejectf("invalid disk device name")
		}
		if d.Used > d.Total {
			return rejectf("disk %s used %d exceeds total %d", d.Device, d.Used, d.Total)
		}
	}
	if r.Data.Network.RxSpeed < 0 || r.Data.Network.TxSpeed < 0 ||
		r.Data.Network.RxSpeed > MaxNetSpeedBytes || r.Data.Network.TxSpeed > MaxNetSpeedBytes {
		return rejectf("network speed out of range")
	}
	if r.Data.Network.TCPConnections < 0 || r.Data.Network.UDPConnections < 0 {
		return rejectf("negative connection count")
	}
	if r.Data.Uptime < 0 || r.Data.ProcessCount < 0 {
		return rejectf("negative uptime or process count")
	}
	return validateIP(r.Data.IPInfo)
}

func validateIP(info IPInfo) error {
	if info.IPv4 != "" {
		if ip := net.ParseIP(info.IPv4); ip == nil || ip.To4() == nil {
			return rejectf("invalid ipv4: %q", info.IPv4)
		}
	}
	if info.IPv6 != "" {
		if ip := net.ParseIP(info.IPv6); ip == nil || ip.To4() != nil {
			return rejectf("invalid ipv6: %q", info.IPv6)
		}
	}
	return nil
}

// Validate enforces the ping result rules from §7.6.
func (p PingResult) Validate() error {
	if len(p.Data) > MaxPingEntries {
		return rejectf("ping entries %d exceeds limit %d", len(p.Data), MaxPingEntries)
	}
	for _, s := range p.Data {
		switch {
		case s.Target == "" || len(s.Target) > 255:
			return rejectf("target is empty or too long")
		case len(s.Name) > 64 || len(s.Method) > 16:
			return rejectf("ping name or method too long for %s", s.Target)
		case s.IPVersion != 4 && s.IPVersion != 6:
			return rejectf("invalid IP version %d for %s", s.IPVersion, s.Target)
		case !utf8.ValidString(s.Target) || !utf8.ValidString(s.Name) ||
			!utf8.ValidString(s.Method) ||
			strings.IndexFunc(s.Target, unicode.IsControl) >= 0 ||
			strings.IndexFunc(s.Name, unicode.IsControl) >= 0 ||
			strings.IndexFunc(s.Method, unicode.IsControl) >= 0:
			return rejectf("ping text contains invalid characters")
		case s.AvgLatency < 0 || s.AvgLatency > MaxLatencyMs ||
			s.MinLatency < 0 || s.MinLatency > MaxLatencyMs ||
			s.MaxLatency < s.MinLatency || s.MaxLatency > MaxLatencyMs:
			return rejectf("latency out of range for %s: %v", s.Target, s.AvgLatency)
		case s.Jitter < 0 || s.Jitter > MaxLatencyMs:
			return rejectf("jitter out of range for %s", s.Target)
		case s.Loss < 0 || s.Loss > 100:
			return rejectf("loss out of range for %s: %v", s.Target, s.Loss)
		case s.PacketsSent < 0 || s.PacketsRecv < 0 || s.PacketsRecv > s.PacketsSent:
			return rejectf("packet counts inconsistent for %s", s.Target)
		}
	}
	return nil
}
