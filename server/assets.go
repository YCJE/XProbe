package server

import (
	"embed"
	"fmt"
	"io/fs"
)

// Agent 二进制分发(设计文档 8.3/8.4): 构建时将各架构 Agent 放入 assets/agents/。
// Docker 构建自动完成; 本地开发目录仅含 .gitkeep, 分发端点返回 404。
//
//go:embed all:assets
var assetFS embed.FS

// AgentBinary 返回指定 os/arch 的 Agent 二进制内容。
func AgentBinary(goos, arch string) ([]byte, fs.FileInfo, error) {
	path := fmt.Sprintf("assets/agents/%s-%s/xprobe-agent", goos, arch)
	b, err := assetFS.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("agent binary %s not embedded (place it at %s at build time)", path, path)
	}
	info, _ := fs.Stat(assetFS, path)
	return b, info, nil
}

// AgentSHA256 返回对应 .sha256 文件内容(发布时生成)。
func AgentSHA256(goos, arch string) ([]byte, error) {
	return assetFS.ReadFile(fmt.Sprintf("assets/agents/%s-%s/xprobe-agent.sha256", goos, arch))
}
