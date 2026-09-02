// Package server 提供前端静态资源 embed(设计文档 3.3: web/ 为唯一 embed 源)。
// 前端构建: cd frontend && npm run build → 输出到 server/web/。
package server

import (
	"embed"
	"io/fs"
)

//go:embed all:web
var webFS embed.FS

// Web 返回去掉 web/ 前缀的静态资源文件系统。
func Web() (fs.FS, error) {
	return fs.Sub(webFS, "web")
}

// PlaceholderHTML 源码 checkout 未构建前端时的占位页(make build-frontend 生成真实面板)。
const PlaceholderHTML = `<!doctype html><meta charset="utf-8"><title>XProbe</title>
<body style="font-family:system-ui;padding:2rem">
<h2>XProbe</h2><p>前端资源未构建: 运行 <code>make build-frontend</code> 后重新编译 Server, 或使用官方发布二进制。</p>
</body>`
