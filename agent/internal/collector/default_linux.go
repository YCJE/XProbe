//go:build linux

package collector

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// DefaultSources 返回 Linux 生产实现(采集零 exec, S4)。
func DefaultSources() Sources {
	s := Sources{
		Statfs: statfsLinux,
		Uname:  unameLinux,
	}
	standard(&s)
	return s
}

func statfsLinux(path string) (FsStat, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return FsStat{}, err
	}
	return FsStat{
		Blocks: st.Blocks,
		Bfree:  st.Bfree,
		Bavail: st.Bavail,
		Bsize:  uint64(st.Bsize),
	}, nil
}

func unameLinux() (UnameInfo, error) {
	var u unix.Utsname
	if err := unix.Uname(&u); err != nil {
		return UnameInfo{}, err
	}
	return UnameInfo{
		SysName: utsCStr(u.Sysname),
		Release: utsCStr(u.Release),
		Machine: utsCStr(u.Machine),
	}, nil
}

func utsCStr(ca [65]byte) string {
	b := make([]byte, 0, len(ca))
	for _, c := range ca {
		if c == 0 {
			break
		}
		b = append(b, byte(c))
	}
	return string(b)
}
