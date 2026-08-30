// Package collector 实现 Agent 的系统指标采集(设计文档 4.1 采集项清单)。
//
// 所有采集器通过 Sources 抽象系统访问点: 生产环境由 default_linux.go 提供 Linux
// 实现; 测试注入假数据源, 使 /proc 解析与统计逻辑可跨平台验证(开发机为 Windows)。
// 采集零 exec: 不调用任何外部命令(S4)。
package collector

import (
	"errors"
	"os"
)

// Sources 抽象 Agent 采集所需的全部系统访问点。
type Sources struct {
	ReadFile func(path string) ([]byte, error)
	ReadDir  func(path string) ([]DirEntry, error)
	Statfs   func(path string) (FsStat, error)
	Uname    func() (UnameInfo, error)
	Hostname func() (string, error)
}

type DirEntry struct {
	Name string
}

// FsStat 是 statfs 结果中采集用到的字段。
type FsStat struct {
	Blocks uint64 // 文件系统总块数
	Bfree  uint64 // 空闲块(含 root 保留)
	Bavail uint64 // 非特权用户可用块
	Bsize  uint64 // 块大小(字节)
}

type UnameInfo struct {
	SysName string
	Release string // 内核版本
	Machine string // 架构
}

// ErrUnsupported 表示该采集项在当前平台不可用(v1 仅支持 Linux, 设计文档 4.1)。
var ErrUnsupported = errors.New("collector: unsupported on this platform (v1 is Linux-only)")

// standard 供各平台 DefaultSources 复用的可移植部分。
func standard(s *Sources) {
	if s.ReadFile == nil {
		s.ReadFile = os.ReadFile
	}
	if s.ReadDir == nil {
		s.ReadDir = func(path string) ([]DirEntry, error) {
			ents, err := os.ReadDir(path)
			if err != nil {
				return nil, err
			}
			out := make([]DirEntry, 0, len(ents))
			for _, e := range ents {
				out = append(out, DirEntry{Name: e.Name()})
			}
			return out, nil
		}
	}
	if s.Hostname == nil {
		s.Hostname = os.Hostname
	}
}
