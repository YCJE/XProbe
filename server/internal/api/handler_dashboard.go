package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/YCJE/XProbe/server/internal/pkg"
	"github.com/YCJE/XProbe/server/internal/service"
)

// HandleDashboardWS GET /ws/dashboard: 浏览器实时推送(JWT 经 Cookie, 设计文档 6.8)。
// 每 3 秒推送全部服务器合并视图; 写失败即断开(浏览器负责重连)。
func (d Deps) HandleDashboardWS(c *gin.Context) {
	upgrader := websocket.Upgrader{
		ReadBufferSize:    4096,
		WriteBufferSize:   4096,
		EnableCompression: d.WSCompression,
		CheckOrigin:       func(*http.Request) bool { return true },
	}
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	push := func() error {
		// 会话吊销/过期后即时断开(握手后长连接不再信任, 审查 LOW #8)
		if token := tokenFromRequest(c); token == "" {
			return errors.New("session gone")
		} else if active, aerr := d.Sessions.IsActive(c.Request.Context(),
			pkg.SHA256Hex(token), time.Now().Unix()); aerr != nil || !active {
			return errors.New("session revoked")
		}
		agents, err := d.Agents.List(c.Request.Context())
		if err != nil {
			return err
		}
		payload := gin.H{
			"type":    "dashboard_update",
			"servers": service.BuildDashboardServers(agents, d.Hub),
			"ts":      time.Now().Unix(),
		}
		_ = conn.SetWriteDeadline(time.Now().Add(service.WriteTimeout))
		return conn.WriteJSON(payload)
	}

	if err := push(); err != nil { // 连接建立立即推一帧
		return
	}
	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-ticker.C:
			if err := push(); err != nil {
				return
			}
		}
	}
}
