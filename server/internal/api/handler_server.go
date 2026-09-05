package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/YCJE/XProbe/internal/model"
	"github.com/YCJE/XProbe/server/internal/pkg"
	"github.com/YCJE/XProbe/server/internal/repository"
	"github.com/YCJE/XProbe/server/internal/service"
)

// HandleCreateServerNode POST /api/v1/servers: 预创建节点(Komari 模式)。
// 创建后返回绑定该节点的一次性注册码, 用于拼装一键安装命令。
func (d Deps) HandleCreateServerNode(c *gin.Context) {
	var req model.CreateNodeRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name required"})
		return
	}
	if len(req.Name) > 128 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name too long"})
		return
	}
	if !service.SafeLabel(req.Notes, 500) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid notes"})
		return
	}
	now := time.Now().Unix()
	id, err := d.Agents.Create(c.Request.Context(), &model.Agent{
		DisplayName: req.Name,
		Notes:       req.Notes,
		Hostname:    req.Name, // 注册后会被 Agent 真实主机名覆盖
		CreatedAt:   now,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	code, expires, err := d.Registry.IssueCodeForNode(c.Request.Context(), id)
	if err != nil {
		_ = d.Agents.DeleteCascade(c.Request.Context(), id)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id, "code": code, "expires_at": expires.Unix()})
}

// HandleNodeInstallCode POST /api/v1/servers/:id/install-code: 为节点重新生成绑定注册码。
func (d Deps) HandleNodeInstallCode(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if _, err := d.Agents.Get(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	code, expires, err := d.Registry.IssueCodeForNode(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": code, "expires_at": expires.Unix()})
}

// HandleListServers GET /api/v1/servers: 元数据 + 实时数据合并(设计文档 6.8)。
func (d Deps) HandleListServers(c *gin.Context) {
	agents, err := d.Agents.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"servers": service.BuildDashboardServers(agents, d.Hub)})
}

// HandleServerDetail GET /api/v1/servers/:id。
func (d Deps) HandleServerDetail(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	agent, err := d.Agents.Get(c.Request.Context(), id)
	if errors.Is(err, repository.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	servers := service.BuildDashboardServers([]model.Agent{*agent}, d.Hub)
	traffic, _ := d.Agents.ListTraffic(c.Request.Context(), id, 12)
	c.JSON(http.StatusOK, gin.H{"server": servers[0], "traffic_monthly": traffic})
}

// HandleHistory GET /api/v1/servers/:id/history?range=1h|6h|12h|1d|2d|7d|30d(设计文档 6.5)。
func (d Deps) HandleHistory(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	now := time.Now()
	switch c.Query("range") {
	case "1h", "6h":
		points := d.Hub.ReportSnapshot(id)
		cut := now.Add(-time.Hour)
		if c.Query("range") == "6h" {
			cut = now.Add(-6 * time.Hour)
		}
		trimmed := points[:0]
		for _, p := range points {
			if time.Unix(p.Timestamp, 0).After(cut) {
				trimmed = append(trimmed, p)
			}
		}
		// 6h = 环形缓冲(≤3h) + 5min 聚合(3-6h)拼接(设计文档 6.5, 审查 MEDIUM #6)
		var agg []model.MetricPoint
		if c.Query("range") == "6h" {
			var qerr error
			if agg, qerr = d.Records.Query5m(c.Request.Context(), id,
				now.Add(-6*time.Hour).Unix(), now.Add(-3*time.Hour).Unix()); qerr != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
				return
			}
		}
		c.JSON(http.StatusOK, model.HistoryResponse{
			Range: c.Query("range"), Granularity: "3s", Realtime: trimmed, Points5m: agg})
	case "12h", "1d", "2d":
		dur := map[string]time.Duration{"12h": 12 * time.Hour, "1d": 24 * time.Hour, "2d": 48 * time.Hour}[c.Query("range")]
		points, err := d.Records.Query5m(c.Request.Context(), id, now.Add(-dur).Unix(), now.Unix())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			return
		}
		c.JSON(http.StatusOK, model.HistoryResponse{
			Range: c.Query("range"), Granularity: "5m", Points5m: points})
	case "7d", "30d":
		days := 7
		if c.Query("range") == "30d" {
			days = 30
		}
		points, err := d.Records.QueryDaily(c.Request.Context(), id,
			now.AddDate(0, 0, -days).UTC().Format("2006-01-02"), now.UTC().Format("2006-01-02"))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			return
		}
		c.JSON(http.StatusOK, model.HistoryResponse{
			Range: c.Query("range"), Granularity: "1d", PointsDaily: points})
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "range must be 1h|6h|12h|1d|2d|7d|30d"})
	}
}

// HandlePingHistory GET /api/v1/servers/:id/ping-history: 环形缓冲最近 60 分钟探测行(旧→新)。
func (d Deps) HandlePingHistory(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	rows := d.Hub.PingRows(id)
	if rows == nil {
		rows = [][]model.PingResult{}
	}
	c.JSON(http.StatusOK, gin.H{"rows": rows, "interval_sec": 60})
}

// HandleUpdateMeta PUT /api/v1/servers/:id/meta(设计文档 5.8)。
func (d Deps) HandleUpdateMeta(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req model.UpdateMetaRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	if len(req.CountryCode) > 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "country_code must be ISO 3166-1 alpha-2"})
		return
	}
	if err := d.Agents.UpdateMeta(c.Request.Context(), id, req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// HandleDeleteServer DELETE /api/v1/servers/:id(级联清理, 设计文档 5.1 v1.3)。
func (d Deps) HandleDeleteServer(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := d.Agents.DeleteCascade(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	d.Hub.Drop(id) // 在线则断开
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// HandleResetFingerprint POST /api/v1/servers/:id/reset-fingerprint(设计文档 7.5)。
func (d Deps) HandleResetFingerprint(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := d.Agents.ResetFingerprint(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "note": "agent must re-register with a new code to rebind"})
}

// HandleResetToken POST /api/v1/agents/:id/reset-token: 新 Token 仅响应中完整出现一次(S9)。
func (d Deps) HandleResetToken(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	token, err := pkg.RandomToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	if err := d.Agents.RotateToken(c.Request.Context(), id, pkg.SHA256Hex(token)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	d.Hub.Drop(id) // 旧 Token 会话立即失效
	c.JSON(http.StatusOK, model.ResetTokenResponse{Token: token})
}

// HandleListAgentTokens GET /api/v1/agents/tokens(仅掩码展示)。
func (d Deps) HandleListAgentTokens(c *gin.Context) {
	agents, err := d.Agents.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	type tokenRow struct {
		ID        int64  `json:"id"`
		Hostname  string `json:"hostname"`
		Online    bool   `json:"online"`
		Version   string `json:"agent_version"`
		TokenMask string `json:"token_mask"`
	}
	rows := make([]tokenRow, 0, len(agents))
	for _, a := range agents {
		rows = append(rows, tokenRow{
			ID: a.ID, Hostname: a.Hostname, Online: d.Hub.IsOnline(a.ID),
			Version: a.AgentVersion, TokenMask: repository.MaskToken(a.TokenHash),
		})
	}
	c.JSON(http.StatusOK, gin.H{"agents": rows})
}

// HandleCreateRegisterCode POST /api/v1/agents/register-codes。
func (d Deps) HandleCreateRegisterCode(c *gin.Context) {
	code, expires, err := d.Registry.IssueCode(c.Request.Context())
	if errors.Is(err, service.ErrTooManyCodes) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "max 5 active register codes"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": code, "expires_at": expires.Unix()})
}

func pathID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return 0, false
	}
	return id, true
}
