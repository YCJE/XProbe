package collector

import (
	"fmt"
	"strings"

	"github.com/YCJE/XProbe/internal/model"
)

// Disk 采集器: /proc/mounts 过滤真实文件系统挂载点, statfs 取使用率(设计文档 4.1)。
type Disk struct {
	src Sources
}

func NewDisk(src Sources) *Disk { return &Disk{src: src} }

// realFSTypes 视为真实磁盘的文件系统白名单; 其余(overlay/tmpfs/proc 等)排除。
// tmpfs 通常承载构建缓存, 计入会干扰磁盘告警; 如有需要可在面板按挂载点粒度复核。
var realFSTypes = map[string]bool{
	"ext2": true, "ext3": true, "ext4": true, "xfs": true, "btrfs": true,
	"zfs": true, "f2fs": true, "jfs": true, "reiserfs": true, "bcachefs": true,
	"vfat": true, "exfat": true, "ntfs": true, "ntfs3": true, "fuseblk": true,
}

type Mount struct {
	Device     string
	Mountpoint string
	FSType     string
}

func (d *Disk) Collect() ([]model.DiskUsage, error) {
	b, err := d.src.ReadFile("/proc/mounts")
	if err != nil {
		return nil, fmt.Errorf("read /proc/mounts: %w", err)
	}
	mounts := parseMounts(b)
	if len(mounts) == 0 {
		return nil, fmt.Errorf("parseMounts: no real filesystem mounts found")
	}
	out := make([]model.DiskUsage, 0, len(mounts))
	for _, m := range mounts {
		st, err := d.src.Statfs(m.Mountpoint)
		if err != nil {
			continue // 单个挂载点失败(如挂载期间移除)不影响其余
		}
		if st.Bsize == 0 || st.Blocks == 0 {
			continue
		}
		out = append(out, model.DiskUsage{
			Device: m.Mountpoint,
			Total:  st.Blocks * st.Bsize,
			Used:   (st.Blocks - st.Bavail) * st.Bsize,
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("disk: no usable mounts (statfs failed for all)")
	}
	return out, nil
}

// parseMounts 解析 /proc/mounts, 按白名单过滤并按挂载点去重(保留首个)。
func parseMounts(b []byte) []Mount {
	seen := map[string]bool{}
	var out []Mount
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		dev, mp, fstype := fields[0], unescapeMountPath(fields[1]), fields[2]
		if !realFSTypes[fstype] {
			continue
		}
		if mp == "" || seen[mp] {
			continue
		}
		seen[mp] = true
		out = append(out, Mount{Device: dev, Mountpoint: mp, FSType: fstype})
	}
	return out
}

// unescapeMountPath 还原 /proc/mounts 中的八进制转义(空格 "\040" 等)。
func unescapeMountPath(p string) string {
	if !strings.Contains(p, "\\") {
		return p
	}
	var sb strings.Builder
	for i := 0; i < len(p); i++ {
		if p[i] == '\\' && i+3 < len(p) {
			if v, ok := parseOctal(p[i+1 : i+4]); ok {
				sb.WriteByte(v)
				i += 3
				continue
			}
		}
		sb.WriteByte(p[i])
	}
	return sb.String()
}

func parseOctal(s string) (byte, bool) {
	if len(s) != 3 {
		return 0, false
	}
	var v int
	for _, c := range s {
		if c < '0' || c > '7' {
			return 0, false
		}
		v = v*8 + int(c-'0')
	}
	return byte(v), true
}
