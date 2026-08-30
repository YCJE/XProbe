package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/YCJE/XProbe/internal/model"
	"github.com/YCJE/XProbe/server/internal/pkg"
	"github.com/YCJE/XProbe/server/internal/repository"
)

// HandleListAlerts GET /api/v1/alerts。
func (d Deps) HandleListAlerts(c *gin.Context) {
	rules, err := d.Alerts.ListRules(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"rules": rules})
}

// HandleCreateAlert POST /api/v1/alerts。
func (d Deps) HandleCreateAlert(c *gin.Context) {
	var rule model.AlertRule
	if err := c.ShouldBindJSON(&rule); err != nil || rule.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name and metric required"})
		return
	}
	if !validMetric(rule.Metric) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unknown metric"})
		return
	}
	if rule.Operator != ">" && rule.Operator != "<" && rule.Operator != "=" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "operator must be >, < or ="})
		return
	}
	id, err := d.Alerts.CreateRule(c.Request.Context(), &rule)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id})
}

// HandleDeleteAlert DELETE /api/v1/alerts/:id。
func (d Deps) HandleDeleteAlert(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := d.Alerts.DeleteRule(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// HandleAlertHistory GET /api/v1/alerts/history。
func (d Deps) HandleAlertHistory(c *gin.Context) {
	history, err := d.Alerts.ListHistory(c.Request.Context(), 100)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"history": history})
}

// HandleListChannels GET /api/v1/notify/channels(敏感字段脱敏)。
func (d Deps) HandleListChannels(c *gin.Context) {
	channels, err := d.NotifyChannels.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	for i := range channels {
		maskChannel(&channels[i])
	}
	c.JSON(http.StatusOK, gin.H{"channels": channels})
}

// HandleCreateChannel POST /api/v1/notify/channels。
func (d Deps) HandleCreateChannel(c *gin.Context) {
	var ch model.NotifyChannel
	if err := c.ShouldBindJSON(&ch); err != nil || ch.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name required"})
		return
	}
	if ch.Type != "webhook" && ch.Type != "telegram" && ch.Type != "smtp" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "type must be webhook/telegram/smtp"})
		return
	}
	if ch.Type == "webhook" {
		if u, _ := ch.Config["url"].(string); u != "" {
			if _, err := pkg.CheckURLSchemeAndHost(u); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
		}
	}
	id, err := d.NotifyChannels.Create(c.Request.Context(), &ch)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id})
}

// HandleUpdateChannel PUT /api/v1/notify/channels/:id。
func (d Deps) HandleUpdateChannel(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var ch model.NotifyChannel
	if err := c.ShouldBindJSON(&ch); err != nil || ch.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name required"})
		return
	}
	ch.ID = id
	if err := d.NotifyChannels.Update(c.Request.Context(), &ch); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// HandleDeleteChannel DELETE /api/v1/notify/channels/:id。
func (d Deps) HandleDeleteChannel(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := d.NotifyChannels.Delete(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// HandleTestChannel POST /api/v1/notify/channels/:id/test。
func (d Deps) HandleTestChannel(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	cctx, cancel := context.WithTimeout(c.Request.Context(), 12*time.Second)
	defer cancel()
	if err := d.Notifier.Send(cctx, id, "[XProbe] 测试通知", "这是一条测试消息"); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// HandleSaveShare PUT /api/v1/config/share(logo_url 仅 https, 设计文档 7.7)。
func (d Deps) HandleSaveShare(c *gin.Context) {
	var cfg model.SharePageConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	if cfg.LogoURL != "" && !strings.HasPrefix(cfg.LogoURL, "https://") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "logo_url must use https:// scheme"})
		return
	}
	existing, err := d.Share.Get(c.Request.Context())
	if errors.Is(err, repository.ErrNotFound) {
		cfg.ShareID = newShareID()
	} else if err == nil {
		cfg.ShareID = existing.ShareID
	}
	if err := d.Share.Save(c.Request.Context(), &cfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"share_id": cfg.ShareID})
}

// HandleGetShare GET /api/v1/config/share(管理端)。
func (d Deps) HandleGetShare(c *gin.Context) {
	cfg, err := d.Share.Get(c.Request.Context())
	if errors.Is(err, repository.ErrNotFound) {
		c.JSON(http.StatusOK, gin.H{"share": nil})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"share": cfg})
}

// HandlePublicShare GET /api/v1/public/:share_id(免登录, 白名单字段, T11)。
func (d Deps) HandlePublicShare(c *gin.Context) {
	shareID := c.Param("share_id")
	cfg, err := d.Share.Get(c.Request.Context())
	if err != nil || cfg.ShareID != shareID {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	agents, err := d.Agents.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	allow := map[int64]bool{}
	for _, id := range cfg.AgentIDs {
		allow[id] = true
	}
	servers := []model.PublicShareServer{}
	for i := range agents {
		a := &agents[i]
		if !allow[a.ID] {
			continue
		}
		ps := model.PublicShareServer{
			DisplayName: a.DisplayName, Hostname: a.Hostname,
			Online: d.Hub.IsOnline(a.ID), CountryCode: a.CountryCode,
			Region: a.Region, ISP: a.ISP,
			Disk: []model.DiskUsage{}, Ping: map[string]float64{}, PingLoss: map[string]float64{},
		}
		if r, ok := d.Hub.LatestReport(a.ID); ok {
			ps.CPU = r.CPU.Usage
			ps.MemUsed, ps.MemTotal = r.Memory.Used, r.Memory.Total
			ps.Disk = r.Disk
			ps.Uptime = r.Uptime
		}
		if p, ok := d.Hub.LatestPing(a.ID); ok {
			for _, t := range p {
				name := t.Name
				if name == "" {
					name = t.Target
				}
				ps.Ping[name] = t.AvgLatency
				ps.PingLoss[name] = t.Loss
			}
		}
		servers = append(servers, ps)
	}
	c.JSON(http.StatusOK, model.PublicSharePayload{
		ShareID: cfg.ShareID, Title: cfg.Title, LogoURL: cfg.LogoURL,
		FooterText: cfg.FooterText, Servers: servers,
	})
}

func validMetric(m string) bool {
	switch m {
	case model.MetricCPU, model.MetricMem, model.MetricDisk,
		model.MetricOffline, model.MetricTrafficQuot, model.MetricExpireDays:
		return true
	}
	return false
}

func maskChannel(ch *model.NotifyChannel) {
	for _, k := range []string{"bot_token", "password"} {
		if _, ok := ch.Config[k]; ok {
			ch.Config[k] = "***"
		}
	}
	if u, ok := ch.Config["url"].(string); ok && len(u) > 24 {
		ch.Config["url"] = u[:24] + "…"
	}
}

func newShareID() string {
	tok, err := pkg.RandomToken()
	if err != nil {
		return ""
	}
	return tok
}
