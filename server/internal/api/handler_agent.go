package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/YCJE/XProbe/internal/model"
	"github.com/YCJE/XProbe/server/internal/pkg"
	"github.com/YCJE/XProbe/server/internal/service"
)

// HandleRegister POST /api/v1/agent/register(设计文档 4.2)。
func (d Deps) HandleRegister(c *gin.Context) {
	var req model.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	resp, err := d.Registry.Register(c.Request.Context(), &req, c.ClientIP())
	switch {
	case err == nil:
		c.JSON(http.StatusOK, resp)
	case errors.Is(err, service.ErrInvalidRegisterReq):
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid register request"})
	case errors.Is(err, service.ErrCodeInvalid):
		c.JSON(http.StatusUnauthorized, gin.H{"error": "register code invalid"})
	case errors.Is(err, service.ErrCodeExpired):
		c.JSON(http.StatusUnauthorized, gin.H{"error": "register code expired"})
	case errors.Is(err, service.ErrCodeUsed):
		c.JSON(http.StatusUnauthorized, gin.H{"error": "register code already used"})
	case errors.Is(err, service.ErrFingerprintConflict):
		c.JSON(http.StatusConflict, gin.H{"error": "host fingerprint conflict: resolve in panel (reset old fingerprint or delete old record)"})
	case errors.Is(err, service.ErrTooManyCodes):
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many active register codes"})
	default:
		log.Printf("[register] internal error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
	}
}

// HandleServerCert GET /api/v1/server-cert: 公开证书指纹, 供安装脚本写入 Agent Pinning 配置。
// 有 CertReloader 时每次现读(续期后指纹实时), 否则用启动时快照。
func (d Deps) HandleServerCert(c *gin.Context) {
	fp := d.CertFingerprint
	if d.CertReloader != nil {
		if live, err := d.CertReloader.Fingerprint(); err == nil {
			fp = live
		}
	}
	c.JSON(http.StatusOK, gin.H{"algorithm": "sha256", "fingerprint": fp})
}

// HandleAgentConfig GET /api/v1/agent/config: 只读探测目标配置(Agent 定时拉取, 设计文档 4.7)。
func (d Deps) HandleAgentConfig(c *gin.Context) {
	targets, err := d.PingTargets.ListEnabled(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	if targets == nil {
		targets = []model.PingTarget{}
	}
	c.JSON(http.StatusOK, model.AgentConfigPayload{PingTargets: targets})
}

// HandleAgentWS GET /api/v1/agent/report: Agent WebSocket 接入(设计文档 5.2)。
// 握手时校验 Bearer Token(AgentAuth 中间件)与 X-Host-Fingerprint(设计文档 7.5)。
func (d Deps) HandleAgentWS(c *gin.Context) {
	agent := c.MustGet("agent").(*model.Agent)

	fp := c.GetHeader("X-Host-Fingerprint")
	if !pkg.ConstantTimeHexEqual(fp, agent.HostFingerprint) {
		log.Printf("[monitor][security] fingerprint mismatch agent=%d ip=%s", agent.ID, c.ClientIP())
		c.AbortWithStatus(http.StatusForbidden)
		return
	}

	upgrader := websocket.Upgrader{
		ReadBufferSize:    4096,
		WriteBufferSize:   4096,
		EnableCompression: d.WSCompression,
		CheckOrigin:       func(*http.Request) bool { return true }, // Agent 非浏览器, 同源策略不适用
	}
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return // Upgrade 已写入错误响应
	}

	if err := d.Hub.Attach(agent.ID, conn); err != nil {
		log.Printf("[monitor] attach agent=%d: %v", agent.ID, err)
		_ = conn.Close()
		return
	}
	d.readLoop(agent.ID, conn)
}

// readLoop 单连接读循环: 消息分发 + 传输限制(设计文档 5.2 v1.3)。
func (d Deps) readLoop(agentID int64, conn service.WSConn) {
	defer d.Hub.Detach(agentID, conn)
	conn.SetReadLimit(service.ReadLimit) // 单帧 >64KB → ReadMessage 直接报错断开

	deadline := d.Hub.HeartbeatTimeout()
	for {
		_ = conn.SetReadDeadline(time.Now().Add(deadline))
		_, payload, err := conn.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) &&
				!errors.Is(err, websocket.ErrCloseSent) {
				// 非常规断开(含 64KB 超限/超时)记安全日志
				log.Printf("[monitor][security] agent=%d conn error: %v", agentID, err)
			}
			return
		}

		var head struct {
			Type model.FrameType `json:"type"`
		}
		if err := json.Unmarshal(payload, &head); err != nil {
			log.Printf("[monitor][security] agent=%d malformed frame, closing", agentID)
			return
		}

		switch head.Type {
		case model.FrameHeartbeat:
			if err := d.Hub.HandleHeartbeat(agentID, conn); err != nil {
				log.Printf("[monitor] heartbeat ack agent=%d: %v", agentID, err)
				return
			}
		case model.FrameReport:
			var r model.Report
			if json.Unmarshal(payload, &r) != nil {
				log.Printf("[monitor][security] agent=%d malformed report", agentID)
				return
			}
			if err := d.Hub.HandleReport(agentID, &r); err != nil {
				if errors.Is(err, service.ErrReportTooFast) {
					log.Printf("[monitor][security] agent=%d report too fast, dropped", agentID)
				} else {
					log.Printf("[monitor][security] agent=%d invalid report: %v", agentID, err)
				}
				// 丢弃帧但不断开——避免高频上报成为 DoS 放大器
			}
		case model.FramePingResult:
			var p model.PingReport
			if json.Unmarshal(payload, &p) != nil {
				log.Printf("[monitor][security] agent=%d malformed ping_result", agentID)
				return
			}
			if err := d.Hub.HandlePing(agentID, p.Data); err != nil {
				log.Printf("[monitor][security] agent=%d invalid ping_result: %v", agentID, err)
			}
		default:
			// 协议违规: S1 要求除 report/ping_result/heartbeat 外不存在任何帧
			log.Printf("[monitor][security] agent=%d unknown frame type %q, closing", agentID, head.Type)
			return
		}
	}
}
