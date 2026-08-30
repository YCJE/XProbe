package api

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/YCJE/XProbe/server/internal/pkg"
)

// NewRouter 组装路由与中间件(设计文档 5.1 v1.3 全量)。
func NewRouter(d Deps, webFS fs.FS) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery(), pkg.SecurityHeaders())

	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })

	api := r.Group("/api/v1")
	api.Use(RateLimit(d.GlobalLimiter))
	{
		// 认证
		api.GET("/auth/setup-state", d.HandleSetupState)
		api.POST("/auth/setup", RateLimit(d.LoginLimiter), d.HandleSetup)
		api.POST("/auth/login", RateLimit(d.LoginLimiter), d.HandleLogin)
		authed := api.Group("")
		authed.Use(d.JWTAuth())
		{
			authed.POST("/auth/logout", d.HandleLogout)
			authed.GET("/auth/sessions", d.HandleSessions)
			authed.DELETE("/auth/sessions/:id", d.HandleRevokeSession)
			authed.DELETE("/auth/sessions", d.HandleRevokeAllSessions)

			// Agent 通道(仍按 Agent 凭证认证, 面板侧管理)
			authed.GET("/agents/tokens", d.HandleListAgentTokens)
			authed.POST("/agents/register-codes", d.HandleCreateRegisterCode)
			authed.GET("/agents/register-codes", d.HandleListRegisterCodes)
			authed.DELETE("/agents/register-codes/:hash", d.HandleDeleteRegisterCode)
			authed.POST("/agents/:id/reset-token", d.HandleResetToken)

			// 服务器
			authed.GET("/servers", d.HandleListServers)
			authed.GET("/servers/:id", d.HandleServerDetail)
			authed.GET("/servers/:id/history", d.HandleHistory)
			authed.GET("/servers/:id/traffic", d.HandleServerTraffic)
			authed.PUT("/servers/:id/meta", d.HandleUpdateMeta)
			authed.DELETE("/servers/:id", d.HandleDeleteServer)
			authed.POST("/servers/:id/reset-fingerprint", d.HandleResetFingerprint)

			// 告警与通知(M5)
			authed.GET("/alerts", d.HandleListAlerts)
			authed.POST("/alerts", d.HandleCreateAlert)
			authed.DELETE("/alerts/:id", d.HandleDeleteAlert)
			authed.GET("/alerts/history", d.HandleAlertHistory)
			authed.GET("/notify/channels", d.HandleListChannels)
			authed.POST("/notify/channels", d.HandleCreateChannel)
			authed.PUT("/notify/channels/:id", d.HandleUpdateChannel)
			authed.DELETE("/notify/channels/:id", d.HandleDeleteChannel)
			authed.POST("/notify/channels/:id/test", d.HandleTestChannel)
			authed.GET("/config/share", d.HandleGetShare)
			authed.PUT("/config/share", d.HandleSaveShare)

			// 标签
			authed.GET("/tags", d.HandleListTags)
			authed.POST("/tags", d.HandleCreateTag)
			authed.PUT("/tags/:id", d.HandleUpdateTag)
			authed.DELETE("/tags/:id", d.HandleDeleteTag)

			// 探测目标(预置目标可停用不可删除)
			authed.GET("/config/ping-targets", d.HandleListPingTargets)
			authed.POST("/config/ping-targets", d.HandleCreatePingTarget)
			authed.PUT("/config/ping-targets/:id", d.HandleUpdatePingTarget)
			authed.DELETE("/config/ping-targets/:id", d.HandleDeletePingTarget)
		}

		// Agent 专用(非面板)
		api.POST("/agent/register", RateLimit(d.RegisterLimiter), d.HandleRegister)
		api.GET("/agent/config", AgentAuth(d.Agents), d.HandleAgentConfig)
		api.GET("/agent/report", AgentAuth(d.Agents), d.HandleAgentWS)
		api.GET("/server-cert", d.HandleServerCert)

		// 公开分享页(免登录, 白名单字段)
		api.GET("/public/:share_id", d.HandlePublicShare)
	}

	// Agent 二进制分发(一键安装自包含, 设计文档 8.3)
	r.GET("/download/agent/:os/:arch", d.HandleDownloadAgent)

	// 面板实时推送(JWT Cookie 认证)
	r.GET("/ws/dashboard", d.JWTAuth(), d.HandleDashboardWS)

	// 前端静态资源(SPA fallback, 设计文档 3.3 embed)
	if webFS != nil {
		r.NoRoute(func(c *gin.Context) {
			path := strings.TrimPrefix(c.Request.URL.Path, "/")
			if path == "" {
				path = "index.html"
			}
			f, err := webFS.Open(path)
			if err != nil {
				// SPA fallback: 非 API 路由一律回 index.html
				if strings.HasPrefix(c.Request.URL.Path, "/api/") || strings.HasPrefix(c.Request.URL.Path, "/ws/") {
					c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
					return
				}
				serveFile(c, webFS, "index.html")
				return
			}
			f.Close()
			serveFile(c, webFS, path)
		})
	}
	return r
}
