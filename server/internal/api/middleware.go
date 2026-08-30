// Package api 实现 Server HTTP/WS API 层(Gin)。
package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/YCJE/XProbe/server/internal/pkg"
	"github.com/YCJE/XProbe/server/internal/repository"
)

// AgentAuth Agent Token 认证中间件。
// Token 经 Authorization: Bearer 头携带(禁止入 URL, 避免反代日志泄露, 设计文档 4.7);
// 服务端以 SHA256 哈希比对(S9)。
func AgentAuth(agents *repository.AgentRepo) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.GetHeader("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(h, prefix) {
			abort401(c)
			return
		}
		agent, err := agents.GetByTokenHash(c.Request.Context(), pkg.SHA256Hex(strings.TrimPrefix(h, prefix)))
		if err != nil {
			abort401(c)
			return
		}
		c.Set("agent", agent)
		c.Next()
	}
}

// RateLimit 按客户端 IP 限速中间件。
func RateLimit(l *pkg.Limiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		if l == nil {
			c.Next()
			return
		}
		if !l.Allow(c.ClientIP()) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests,
				gin.H{"error": "rate limit exceeded"})
			return
		}
		c.Next()
	}
}

func abort401(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
}
