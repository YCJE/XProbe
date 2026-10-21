// Package collector gathers system metrics for the report frame.
//
// Parsing functions live here without build tags so they are unit-testable on
// any OS; the actual /proc & statfs access is isolated in procfs_linux.go
// (v1 platform target is Linux-only, design doc §4.1).
package collector

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ParseStat extracts cumulative CPU jiffies from /proc/stat.
// busy excludes idle+iowait; total sums all fields; cores counts cpuN lines.
func ParseStat(b []byte) (busy, total float64, cores int, err error) {
	found := false
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch {
		case fields[0] == "cpu":
			var vals []float64
			for _, f := range fields[1:] {
				v, e := strconv.ParseFloat(f, 64)
				if e != nil {
					return 0, 0, 0, fmt.Errorf("parse /proc/stat value %q: %w", f, e)
				}
				vals = append(vals, v)
			}
			if len(vals) < 4 {
				return 0, 0, 0, errors.New("parse /proc/stat: cpu line too short")
			}
			// guest/guest_nice are already included in user/nice; adding them
			// would double-count CPU time.
			if len(vals) > 8 {
				vals = vals[:8]
			}
			for _, v := range vals {
				total += v
			}
			idle, iowait := vals[3], 0.0
			if len(vals) > 4 {
				iowait = vals[4]
			}
			busy = total - idle - iowait
			found = true
		default:
			if rest, ok := strings.CutPrefix(fields[0], "cpu"); ok && rest != "" {
				if _, e := strconv.Atoi(rest); e == nil {
					cores++
				}
			}
		}
	}
	if !found {
		return 0, 0, 0, errors.New("parse /proc/stat: no aggregate cpu line")
	}
	return busy, total, cores, nil
}

// ParseCPUInfo returns the CPU model and core count from /proc/cpuinfo.
// Falls back to the ARM-style "Processor" line when "model name" is missing.
func ParseCPUInfo(b []byte) (modelName string, cores int, err error) {
	legacyProcLines := 0
	for _, line := range strings.Split(string(b), "\n") {
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		switch strings.TrimSpace(key) {
		case "processor":
			cores++
		case "Processor":
			// Legacy ARM kernels repeat one "Processor" line per core.
			legacyProcLines++
			if modelName == "" {
				modelName = strings.TrimSpace(value)
			}
		case "model name":
			if modelName == "" {
				modelName = strings.TrimSpace(value)
			}
		}
	}
	if cores == 0 {
		cores = legacyProcLines
	}
	if cores == 0 {
		return modelName, 0, errors.New("parse /proc/cpuinfo: no processor entries")
	}
	return modelName, cores, nil
}

// ParseLoadavg parses the three load averages from /proc/loadavg.
func ParseLoadavg(b []byte) (l1, l5, l15 float64, err error) {
	fields := strings.Fields(string(b))
	if len(fields) < 3 {
		return 0, 0, 0, errors.New("parse /proc/loadavg: too few fields")
	}
	for i, v := range []*float64{&l1, &l5, &l15} {
		parsed, e := strconv.ParseFloat(fields[i], 64)
		if e != nil {
			return 0, 0, 0, fmt.Errorf("parse load value %q: %w", fields[i], e)
		}
		*v = parsed
	}
	return l1, l5, l15, nil
}

// ParseMemInfo extracts memory figures (in bytes) from /proc/meminfo.
// Falls back to MemFree when MemAvailable is missing (ancient kernels).
func ParseMemInfo(b []byte) (memTotal, memAvail, swapTotal, swapFree uint64, err error) {
	var memFree uint64
	haveAvail := false
	for _, line := range strings.Split(string(b), "\n") {
		key, rest, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		kb, e := strconv.ParseUint(fields[0], 10, 64)
		if e != nil {
			continue
		}
		switch strings.TrimSpace(key) {
		case "MemTotal":
			memTotal = kb * 1024
		case "MemAvailable":
			memAvail = kb * 1024
			haveAvail = true
		case "MemFree":
			memFree = kb * 1024
		case "SwapTotal":
			swapTotal = kb * 1024
		case "SwapFree":
			swapFree = kb * 1024
		}
	}
	if memTotal == 0 {
		return 0, 0, 0, 0, errors.New("parse /proc/meminfo: MemTotal missing")
	}
	if !haveAvail {
		memAvail = memFree
	}
	if memAvail > memTotal {
		memAvail = memTotal
	}
	if swapFree > swapTotal {
		swapFree = swapTotal
	}
	return memTotal, memAvail, swapTotal, swapFree, nil
}

// NetDev holds per-interface cumulative byte counters.
type NetDev struct {
	RxBytes uint64
	TxBytes uint64
}

// ParseNetDev parses /proc/net/dev into per-interface counters.
func ParseNetDev(b []byte) (map[string]NetDev, error) {
	lines := strings.Split(string(b), "\n")
	if len(lines) < 3 {
		return nil, errors.New("parse /proc/net/dev: too few lines")
	}
	out := make(map[string]NetDev, len(lines)-2)
	for _, line := range lines[2:] {
		name, rest, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) < 16 {
			continue
		}
		rx, e1 := strconv.ParseUint(fields[0], 10, 64)
		tx, e2 := strconv.ParseUint(fields[8], 10, 64)
		if e1 != nil || e2 != nil {
			continue
		}
		out[strings.TrimSpace(name)] = NetDev{RxBytes: rx, TxBytes: tx}
	}
	if len(out) == 0 {
		return nil, errors.New("parse /proc/net/dev: no interfaces")
	}
	return out, nil
}

// ParseTCPEstablished counts ESTABLISHED (state 01) entries from /proc/net/tcp
// or /proc/net/tcp6.
func ParseTCPEstablished(b []byte) int {
	n := 0
	lines := strings.Split(string(b), "\n")
	for _, line := range lines[min(1, len(lines)):] {
		fields := strings.Fields(line)
		if len(fields) >= 4 && fields[3] == "01" {
			n++
		}
	}
	return n
}

// ParseUDPCount counts socket entries from /proc/net/udp or /proc/net/udp6.
func ParseUDPCount(b []byte) int {
	n := 0
	lines := strings.Split(string(b), "\n")
	for _, line := range lines[min(1, len(lines)):] {
		if len(strings.Fields(line)) >= 4 {
			n++
		}
	}
	return n
}

// Mount is one real-filesystem mount point.
type Mount struct {
	Device     string
	MountPoint string
}

// realFilesystems is an allowlist: pseudo filesystems (proc, sysfs, tmpfs,
// squashfs, cgroup…) are excluded from disk usage reporting.
var realFilesystems = map[string]bool{
	"ext2": true, "ext3": true, "ext4": true, "xfs": true, "btrfs": true,
	"zfs": true, "f2fs": true, "jfs": true, "reiserfs": true,
	"vfat": true, "exfat": true, "ntfs": true, "ntfs3": true, "overlay": true,
}

// ParseMounts extracts real filesystem mount points from /proc/mounts,
// deduplicated by mount point (first entry wins).
func ParseMounts(b []byte) []Mount {
	var out []Mount
	seen := map[string]bool{}
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		if !realFilesystems[fields[2]] {
			continue
		}
		mountPoint := strings.ReplaceAll(fields[1], `\040`, " ")
		if seen[mountPoint] {
			continue
		}
		seen[mountPoint] = true
		out = append(out, Mount{Device: fields[0], MountPoint: mountPoint})
	}
	return out
}

// ParseUptime returns the system uptime in seconds from /proc/uptime.
func ParseUptime(b []byte) (float64, error) {
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return 0, errors.New("parse /proc/uptime: empty")
	}
	v, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, fmt.Errorf("parse uptime %q: %w", fields[0], err)
	}
	return v, nil
}
