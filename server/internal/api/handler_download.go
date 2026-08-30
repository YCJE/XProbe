package api

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"

	"github.com/gin-gonic/gin"

	xprobe "github.com/YCJE/XProbe/server"
)

// HandleDownloadAgent GET /download/agent/:os/:arch[.sha256]: 分发内嵌 Agent 二进制。
// 一键安装脚本从这里下载(设计文档 8.3, v1.3: Server 内嵌自包含分发, 不依赖 GitHub)。
// v1 仅发布 linux 二进制。
func (d Deps) HandleDownloadAgent(c *gin.Context) {
	goos := c.Param("os")
	arch := c.Param("arch")

	if goos != "linux" {
		c.JSON(http.StatusNotFound, gin.H{"error": "only linux binaries are distributed in v1"})
		return
	}

	if len(arch) > 7 && arch[len(arch)-7:] == ".sha256" {
		base := arch[:len(arch)-7]
		if body, err := xprobe.AgentSHA256(goos, base); err == nil {
			c.Data(http.StatusOK, "text/plain; charset=utf-8", body)
			return
		}
		// 构建时未生成校验文件: 按二进制内容现算
		bin, _, err := xprobe.AgentBinary(goos, base)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		h := sha256.Sum256(bin)
		c.String(http.StatusOK, "%s  xprobe-agent\n", hex.EncodeToString(h[:]))
		return
	}

	bin, _, err := xprobe.AgentBinary(goos, arch)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/octet-stream", bin)
}
