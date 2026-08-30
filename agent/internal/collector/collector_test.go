package collector

import (
	"fmt"
)

// newFakeSources 构造测试用数据源: files 模拟 /proc 文件, dirs 模拟目录项。
func newFakeSources(files map[string][]byte, dirs map[string][]string) Sources {
	return Sources{
		ReadFile: func(p string) ([]byte, error) {
			if b, ok := files[p]; ok {
				return b, nil
			}
			return nil, fmt.Errorf("no such file: %s", p)
		},
		ReadDir: func(p string) ([]DirEntry, error) {
			names, ok := dirs[p]
			if !ok {
				return nil, fmt.Errorf("no such dir: %s", p)
			}
			out := make([]DirEntry, 0, len(names))
			for _, n := range names {
				out = append(out, DirEntry{Name: n})
			}
			return out, nil
		},
		Statfs: func(p string) (FsStat, error) { return FsStat{}, ErrUnsupported },
		Uname: func() (UnameInfo, error) {
			return UnameInfo{SysName: "Linux", Release: "5.15.0-generic", Machine: "x86_64"}, nil
		},
		Hostname: func() (string, error) { return "test-host", nil },
	}
}
