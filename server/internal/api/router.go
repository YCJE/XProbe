package api

import (
	"github.com/gin-gonic/gin"

	"github.com/YCJE/XProbe/server/internal/pkg"
)

// NewRouter 组装路由与中间件(设计文档 5.1)。
func NewRouter(d Deps) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery(), pkg.SecurityHeaders())

	r.GET("/healthz", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) })

	api := r.Group("/api/v1")
	api.Use(RateLimit(d.GlobalLimiter))
	{
		api.POST("/agent/register", RateLimit(d.RegisterLimiter), d.HandleRegister)
		api.GET("/agent/config", AgentAuth(d.Agents), d.HandleAgentConfig)
		api.GET("/agent/report", AgentAuth(d.Agents), d.HandleAgentWS)
		api.GET("/server-cert", d.HandleServerCert)
	}
	return r
}
