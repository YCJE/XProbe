package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/YCJE/XProbe/internal/model"
	"github.com/YCJE/XProbe/server/internal/service"
)

// HandleListServices GET /api/v1/services: 配置 + 实时状态 + 45 天在线率。
func (d Deps) HandleListServices(c *gin.Context) {
	services, err := d.Services.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	statuses := map[int64]model.ServiceStatus{}
	for _, st := range d.Checker.Snapshot(c.Request.Context()) {
		statuses[st.ID] = st
	}
	type svcRow struct {
		model.Service
		Status *model.ServiceStatus `json:"status"`
	}
	rows := make([]svcRow, 0, len(services))
	for _, s := range services {
		row := svcRow{Service: s}
		if st, ok := statuses[s.ID]; ok {
			row.Status = &st
		}
		rows = append(rows, row)
	}
	c.JSON(http.StatusOK, gin.H{"services": rows})
}

// HandleCreateService POST /api/v1/services。
func (d Deps) HandleCreateService(c *gin.Context) {
	var v model.Service
	if err := c.ShouldBindJSON(&v); err != nil || v.Name == "" || v.Target == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name and target required"})
		return
	}
	if err := service.ValidateService(&v); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	id, err := d.Services.Create(c.Request.Context(), &v)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id})
}

// HandleUpdateService PUT /api/v1/services/:id。
func (d Deps) HandleUpdateService(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var v model.Service
	if err := c.ShouldBindJSON(&v); err != nil || v.Name == "" || v.Target == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name and target required"})
		return
	}
	v.ID = id
	if err := service.ValidateService(&v); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := d.Services.Update(c.Request.Context(), &v); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// HandleDeleteService DELETE /api/v1/services/:id。
func (d Deps) HandleDeleteService(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := d.Services.Delete(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	d.Checker.ForgetService(id)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// HandleTestService POST /api/v1/services/:id/test: 立即探活一次并返回结果。
func (d Deps) HandleTestService(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	services, err := d.Services.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	for _, svc := range services {
		if svc.ID != id {
			continue
		}
		start := time.Now()
		ok, perr := d.Checker.ProbeOncePublic(c.Request.Context(), &svc)
		returned := struct {
			OK      bool    `json:"ok"`
			Error   string  `json:"error,omitempty"`
			Latency float64 `json:"latency_ms"`
		}{OK: ok, Latency: float64(time.Since(start).Microseconds()) / 1000.0}
		if perr != nil {
			returned.Error = perr.Error()
		}
		c.JSON(http.StatusOK, returned)
		return
	}
	c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
}

// trafficReportRows 供报表端点与分享页复用。
var _ = time.Now
