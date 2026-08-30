package api

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/YCJE/XProbe/internal/model"
	"github.com/YCJE/XProbe/server/internal/service"
)

// HandleListPingTargets GET /api/v1/config/ping-targets。
func (d Deps) HandleListPingTargets(c *gin.Context) {
	targets, err := d.PingTargets.ListEnabled(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ping_targets": targets})
}

// HandleCreatePingTarget POST /api/v1/config/ping-targets。
func (d Deps) HandleCreatePingTarget(c *gin.Context) {
	var t model.PingTarget
	if err := c.ShouldBindJSON(&t); err != nil || t.Target == "" || t.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "target and name required"})
		return
	}
	if t.IPVersion != 4 && t.IPVersion != 6 {
		t.IPVersion = 4
	}
	if t.Protocol == "" {
		t.Protocol = "icmp"
	}
	id, err := d.PingTargets.Create(c.Request.Context(), t)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id})
}

// HandleUpdatePingTarget PUT /api/v1/config/ping-targets/:id。
func (d Deps) HandleUpdatePingTarget(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var t model.PingTarget
	if err := c.ShouldBindJSON(&t); err != nil || t.Target == "" || t.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "target and name required"})
		return
	}
	if err := d.PingTargets.Update(c.Request.Context(), id, t); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// HandleDeletePingTarget DELETE /api/v1/config/ping-targets/:id(预置目标不可删)。
func (d Deps) HandleDeletePingTarget(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := d.PingTargets.Delete(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// HandleServerTraffic GET /api/v1/servers/:id/traffic(月度流量归档)。
func (d Deps) HandleServerTraffic(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	traffic, err := d.Agents.ListTraffic(c.Request.Context(), id, 12)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"traffic_monthly": traffic})
}

// serveFile 以 fs.FS 输出静态文件, index.html 不缓存, 带 hash 的资源长缓存。
func serveFile(c *gin.Context, fsys fs.FS, name string) {
	b, err := fs.ReadFile(fsys, name)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	switch {
	case strings.HasSuffix(name, ".html"):
		c.Header("Cache-Control", "no-cache")
		c.Data(http.StatusOK, "text/html; charset=utf-8", b)
	case strings.HasSuffix(name, ".js") || strings.HasSuffix(name, ".css"):
		c.Header("Cache-Control", "public, max-age=31536000, immutable")
		if strings.HasSuffix(name, ".js") {
			c.Data(http.StatusOK, "text/javascript; charset=utf-8", b)
		} else {
			c.Data(http.StatusOK, "text/css; charset=utf-8", b)
		}
	case strings.HasSuffix(name, ".svg"):
		c.Data(http.StatusOK, "image/svg+xml", b)
	default:
		c.Data(http.StatusOK, "application/octet-stream", b)
	}
}

var _ = service.ErrTooManyCodes

// HandleListTags GET /api/v1/tags。
func (d Deps) HandleListTags(c *gin.Context) {
	tags, err := d.Tags.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"tags": tags})
}

// HandleCreateTag POST /api/v1/tags。
func (d Deps) HandleCreateTag(c *gin.Context) {
	var t model.Tag
	if err := c.ShouldBindJSON(&t); err != nil || t.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name required"})
		return
	}
	id, err := d.Tags.Create(c.Request.Context(), t)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id})
}

// HandleUpdateTag PUT /api/v1/tags/:id。
func (d Deps) HandleUpdateTag(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var t model.Tag
	if err := c.ShouldBindJSON(&t); err != nil || t.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name required"})
		return
	}
	t.ID = id
	if err := d.Tags.Update(c.Request.Context(), t); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// HandleDeleteTag DELETE /api/v1/tags/:id。
func (d Deps) HandleDeleteTag(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := d.Tags.Delete(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// HandleListRegisterCodes GET /api/v1/agents/register-codes。
func (d Deps) HandleListRegisterCodes(c *gin.Context) {
	codes, err := d.Registry.ListCodes(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"codes": codes})
}

// HandleDeleteRegisterCode DELETE /api/v1/agents/register-codes/:hash。
func (d Deps) HandleDeleteRegisterCode(c *gin.Context) {
	hash := c.Param("hash")
	if len(hash) < 8 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid hash"})
		return
	}
	if err := d.Registry.DeleteCode(c.Request.Context(), hash); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
