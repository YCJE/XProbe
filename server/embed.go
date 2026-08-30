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
