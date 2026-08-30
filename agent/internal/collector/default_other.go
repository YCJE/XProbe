//go:build !linux

package collector

// DefaultSources 返回非 Linux 平台的兜底实现。
// v1 仅支持 Linux(设计文档 4.1); Statfs/Uname 不可用, 采集器对应字段报 ErrUnsupported,
// 便于开发机上以 --once 模式验证其余采集与 JSON 输出。
func DefaultSources() Sources {
	s := Sources{
		Statfs: func(string) (FsStat, error) { return FsStat{}, ErrUnsupported },
		Uname:  func() (UnameInfo, error) { return UnameInfo{}, ErrUnsupported },
	}
	standard(&s)
	return s
}
